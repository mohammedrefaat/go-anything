# V2 Open Questions & Unsettled Architectural Decisions

**Date:** November 22, 2026  
**Origin:** Task T0 Discovery Findings (`Docs/V2_DISCOVERY.md`)  
**Context:** Escalation of unsettled architectural questions in accordance with **Execution Rule 6** ("When a task reaches an unsettled architectural question, stop and append a short ADR with a recommended answer to `DECISIONS.md` instead of guessing").

---

## Decision 1: CGO and Race Detector Enforcement Across Operating Systems

### Status
**Escalated / Pending Resolution** (Draft ADR-003)

### Context & Problem Statement
The Go data race detector (`go test -race`) relies internally on ThreadSanitizer (TSan), which mandates CGO (`CGO_ENABLED=1`) and an active C compiler toolchain (GCC or Clang on Unix, MinGW on Windows).

In the current repository:
1. `.goreleaser.yaml` explicitly enforces `CGO_ENABLED=0` to generate hermetic, statically linked binaries across Linux, Darwin, and Windows.
2. Developer and AI agent workstations on Windows or lightweight container environments frequently run without a C compiler (`CGO_ENABLED=0`).
3. Commit `11f628b` tightened `requiredChecksPassed` in `internal/mcp/tools.go` and `validateEvidenceForUnit` in `internal/state/machine.go` to require that check `race` must have status `pass` for the `full` evidence profile.
4. On non-CGO hosts, `defaultRaceExecutor` returns `ErrSkipped`, which results in `race` having status `skipped`. Consequently, MCP `run_gate` and `mark_verified` fail closed with `"Required checks were skipped; no passing evidence saved"`, causing 3 test failures in `cmd/go-anything` and `internal/mcp`.

This creates a conflict between:
- **Security & Integrity (Rule 3 & Rule 5):** Ported Go code must not ship with unobserved data races.
- **Cross-Platform Developer Ergonomics (Rule 1 & Rule 2):** Agents and developers running on standard Windows/macOS workstations must be able to complete migration tasks without local C toolchain prerequisites.

### Alternatives Considered

| Alternative | Description | Pros | Cons |
| :--- | :--- | :--- | :--- |
| **Option A: Explicit `allow_missing_tools` Exemption for Race on Non-CGO Hosts** | When `allow_missing_tools: true` is passed (standard for agent runs) and the host lacks CGO, allow `race` to record `StatusSkipped` without failing `requiredChecksPassed`. State machine records `"race: skipped (cgo unavailable on host)"`. | Immediate cross-platform compatibility; agent loop functions smoothly on all operating systems. | Releases might theoretically occur without local race validation if not checked in CI. |
| **Option B: Dual-Tier Evidence Profiles (`full-local` vs `full-ci`)** | Introduce a `local` or `development` verification profile for agent development that permits skipped optional/CGO checks, while reserving `full` strictly for CI environments. | Explicit semantic separation between developer confidence and release-grade proof. | Increases configuration complexity; units must transition profiles between agent and release. |
| **Option C: Mandatory Linux Container / Docker Sandbox for Quality Gate** | When running on Windows/macOS without CGO, `run_gate` delegates execution to a pre-built Docker/Podman container with GCC and CGO installed. | Guarantees strict race detection on all platforms without lowering the gate. | Heavy external dependency; fails in nested virtualization or environments without Docker daemon access. |

### Recommended Resolution (Proposed ADR-003)
**Adopt Option A with strict CI enforcement:**
1. In `internal/mcp/tools.go` (`requiredChecksPassed`) and `internal/state/machine.go` (`validateEvidenceForUnit`), allow `race` to carry `StatusSkipped` **only** when `allow_missing_tools: true` is explicitly passed and host diagnostics confirm CGO is absent (`strings.Contains(out, "requires cgo")`).
2. GitHub Actions CI retains strict `CGO_ENABLED=1` on Linux runners and rejects any pull request whose commit evidence was generated with skipped race checks.
3. Units may reach `Verified` locally under `allow_missing_tools`, but the `approve` and `promote` CLI commands warn the operator if any check was skipped during verification.

---

## Decision 2: Hot-Path Unit State & Fallback Counter Design

### Status
**Escalated / Pending Resolution** (Draft ADR-004)

### Context & Problem Statement
The strangler pattern front door (`adapters/application/http/frontdoor` and `adapters/application/cli/frontdoor`) intercepts live production traffic. For each incoming request, the front door must:
1. Look up the unit's routing state (`Approved`, `Cutover`, `Completed`).
2. Verify the kill switch (`GO_ANYTHING_UNIT_<id>=original`).
3. If an error occurs or the kill switch is triggered, route to legacy and atomically increment `unit.FallbackCount`.
4. Execution Rule 1 mandates that durable unit state lives in `.go-anything/units/<id>.json` written atomically via temp file, `fsync`, and rename under OS file locks (`LockFileEx` / `flock`).

### The Problem
On high-throughput HTTP workloads (e.g., 10,000 to 50,000 req/sec):
- Reading JSON files from disk and parsing them on every request introduces unacceptable I/O latency (1–5ms vs sub-millisecond proxy targets).
- Writing and fsyncing an updated JSON file to disk under an OS file lock on every fallback event creates severe lock contention, I/O bottlenecks, and thread starvation.

### Alternatives Considered

| Alternative | Description | Pros | Cons |
| :--- | :--- | :--- | :--- |
| **Option A: In-Memory Read Cache with Atomic Flush** | The front door maintains a cached in-memory routing table (`sync.RWMutex` or `atomic.Pointer[RouteTable]`). Fallback counts are updated via lock-free `atomic.Uint64` counters. A background flusher syncs counters to `.go-anything/units/<id>.json` periodically (e.g. every 1s) and upon graceful shutdown. | Microsecond proxy latency; zero disk I/O on the hot path; maintains JSON state compatibility. | Process crash could lose up to 1 second of fallback counter increments. |
| **Option B: Separation of Durable Lifecycle State from Ephemeral Metrics** | Keep `.go-anything/units/<id>.json` strictly for lifecycle state transitions (read-only during traffic proxying). Move fallback counts and runtime telemetry into a dedicated metrics store: memory-mapped file (`mmap`), local SQLite database in WAL mode, or Prometheus exporter. | Clean separation of concerns; high durability; zero lock contention on unit JSON files. | Adds an additional file format or dependency for metrics storage. |
| **Option C: Direct Synchronous Disk Persistence under File Lock** | Keep current implementation: every state check reads disk, and every fallback acquires `LockFileEx`/`flock`, updates JSON, and calls `fsync`. | Strict compliance with naive interpretation of Rule 1; maximum crash durability. | Unusable in production traffic; proxy throughput drops by 99% under load. |

### Recommended Resolution (Proposed ADR-004)
**Adopt Option A:**
1. Unit lifecycle state (`State`, `Dependencies`, `Profile`) remains durable in `.go-anything/units/<id>.json` and is loaded into memory at front-door startup.
2. Route switching (`cutover`, `promote`) updates the in-memory route table atomically via `atomic.Pointer`.
3. Fallback counters use in-memory `atomic.Uint64`. The front door flushes updated fallback counts to disk periodically and upon shutdown, guaranteeing near-zero latency on the hot path while persisting durable counts for `lift` verification.

---

## Decision 3: Python Adapter Transport Evolution (Subprocess vs HTTP vs CShared)

### Status
**Resolved in Research; Pending Formal Roadmap Reclassification**

### Context
`adapters/source/python/cshared` was implemented to explore embedding Go into CPython via shared C libraries (`.so` / `.dll`).
Extensive benchmarking and analysis documented in `adapters/source/python/cshared/DECISION.md` demonstrated critical operational hazards:
1. **GIL Deadlocks:** Go goroutines invoking Python C-API functions frequently deadlock against the Python Global Interpreter Lock (GIL).
2. **Signal Collision:** The Go runtime's preemptive scheduling relies on `SIGURG` / `SIGIO`, which conflicts with CPython's signal handlers.
3. **`fork()` Safety:** Go does not support multi-threaded `fork()` without immediate `exec()`, causing crashes in common Python WSGI servers (Gunicorn, uWSGI).

### Recommended Resolution
1. Designate `TransportSubprocess` (for CLI scripts and batch tools) and `TransportHTTP` (for web applications such as FastAPI and Flask) as the **only production-grade transports** for Milestone M6.
2. Formally reclassify `cshared` as an experimental research artifact not intended for production migration paths until WASM-based runtimes (such as `wazero`) provide memory-safe, thread-isolated in-process execution.

---

## Decision 4: Package Distribution Infrastructure

### Status
**Open / Actionable for Release Milestones (M5 & M7)**

### Context
`.goreleaser.yaml` currently builds standalone `.tar.gz` and `.zip` archives signed with Cosign. It does not publish to package managers commonly expected by Go developers and devops engineers:
- macOS: Homebrew (`brew install mohammedrefaat/tap/go-anything`)
- Windows: Scoop (`scoop install go-anything`)
- Linux: AUR / Debian / RPM packages

### Recommended Resolution
1. In Milestone M5, establish `mohammedrefaat/homebrew-tap` and configure GoReleaser `brews` block.
2. Configure GoReleaser `scoops` block to generate Windows manifests.
3. Maintain binary self-update (`go-anything update` or Go install `go install github.com/mohammedrefaat/go-anything/cmd/go-anything@latest`).
