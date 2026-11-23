# Architectural Decision Records (ADRs)

This document tracks architectural decisions for `go-anything`. In accordance with **Execution Rule 6**, when an agent or contributor encounters an unsettled architectural question, they stop and record an ADR with a recommended decision rather than guessing.

---

## Foundational Pre-M0 Decisions

Prior to Milestone 0, key foundational design decisions were settled in the Architecture Plan (v3):

1. **License (Resolved):** Apache-2.0, chosen for its explicit patent grant, commercial compatibility, and permissive open-source terms.
2. **State Store (Resolved):** JSON and JSONL files with atomic writes (`temp file -> fsync -> rename`) under OS file locks, ensuring durable unit state is reviewable in pull requests.
3. **First Non-Go Adapter (Resolved):** Python, implementing subprocess and HTTP transports first.
4. **Go Modernization (Resolved):** Supported as a mode of the Go adapter (`go-anything init --source=go`) with `go-anything modernize` as an alias.

---

## ADR Index

| ADR | Title | Status | Date | Decision Summary | Document |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **ADR-001** | Restricted Execution Sandbox Backend | **Resolved** | 2026-11-19 | Use native Linux namespaces with `bubblewrap` fallback; best-effort isolation with explicit warning and env stripping on macOS/Windows. No mandatory container daemon. | [adr-001-sandbox-backend.md](Docs/decisions/adr-001-sandbox-backend.md) |
| **ADR-002** | Cross-Machine Unit Claims and Coordination | **Resolved** | 2026-11-19 | Pull-request and branch ownership is sufficient through v1.0; local leases in `.go-anything/local/` coordinate agents on a single machine; no shared claim service before v1.0. | [adr-002-cross-machine-claims.md](Docs/decisions/adr-002-cross-machine-claims.md) |
| **ADR-005** | Hot-Path Sidecar Protocol & IPC Contract | **Resolved** | 2026-11-23 | Use out-of-process sidecar protocol over in-process FFI/CGO (avoids GIL deadlocks, signal collisions, fork hazards). Tier 1: length-prefixed stdio JSON-RPC 2.0; Tier 2: Unix socket/named pipe; Tier 3: gRPC. `bridge.yaml` manifest, precise JSON/Go type mapping, -32000..-32099 error codes, and strangler fallback via `GO_ANYTHING_BRIDGE=off`. | [adr-005-hotpath-sidecar-protocol.md](Docs/decisions/adr-005-hotpath-sidecar-protocol.md) |

---

## Resolved ADR Summaries

### [ADR-001: Restricted Execution Sandbox Backend](Docs/decisions/adr-001-sandbox-backend.md)
* **Status:** Resolved
* **Scope:** `internal/sandbox/` (Task T0.5)
* **Summary:**
  - **Context:** `go-anything` executes original code during recording and differential fuzzing. Execution Rule 3 requires fail-closed isolation (no-network by default, environment allowlist, temp working copy, resource limits).
  - **Decision:** Tiered sandbox approach:
    1. *Linux:* Native Linux kernel namespaces (`unshare` via `syscall.SysProcAttr`) with `bubblewrap` (`bwrap`) fallback where user namespaces are restricted.
    2. *macOS & Windows:* Best-effort process runner using an environment allowlist, temporary directory isolation, timeout/process-group termination, and an explicit console warning (`[WARN] Sandbox running in best-effort mode...`). Proxy environment variables are directed to invalid sinks when `--allow-network` is not set.
    3. *Container Daemons:* Docker/Podman are not required for standard workflows, avoiding heavyweight dependencies and startup latency.

### [ADR-002: Cross-Machine Unit Claims and Coordination](Docs/decisions/adr-002-cross-machine-claims.md)
* **Status:** Resolved
* **Scope:** `internal/state/` (Task T0.3), MCP `claim_unit` (Task T3.2)
* **Summary:**
  - **Context:** Migration units are tracked in `.go-anything/units/<unit>.json`. Previous designs storing leases in git caused merge conflicts and stale locks.
  - **Decision:**
    1. *Single Machine:* Agents running on the same host coordinate via time-bounded leases (`owner`, `expires_at`) in `.go-anything/local/` under OS file locks. Local leases are gitignored and automatically expire if an agent crashes.
    2. *Cross-Machine:* Standard Git workflow (branches and Pull Requests) is sufficient through v1.0. Branch naming (`unit/<unit-id>`) and PR ownership prevent conflicting claims without needing an external distributed lock service.
    3. *No External Service:* No centralized coordination server (Redis/etcd/API) will be built or required prior to v1.0, preserving offline-first and zero-infrastructure operation.

### [ADR-005: Hot-Path Sidecar Protocol & IPC Contract](Docs/decisions/adr-005-hotpath-sidecar-protocol.md)
* **Status:** Resolved
* **Scope:** Task B1, Polyglot Strangulation (Python, Node.js/TypeScript, Ruby hosts calling ported Go units)
* **Summary:**
  - **Context:** Polyglot strangler migrations require invoking ported Go units from legacy runtimes. In-process FFI (C-Shared / CGO) causes GIL deadlocks, runtime signal collisions (`SIGURG`), unsafe multi-threaded `fork()` crashes, and C-compiler dependency requirements that break hermetic static binaries (`CGO_ENABLED=0`).
  - **Decision:**
    1. *Out-of-Process Sidecar First:* Adopt an isolated sidecar process architecture providing memory isolation, fault containment, and cross-platform zero-CGO static distribution.
    2. *Transport Tiers:*
       - **Tier 1:** Standard I/O with length-prefixed JSON-RPC 2.0 (LSP framing: `Content-Length: N\r\n\r\n{...}`). Universal, zero network ports, default.
       - **Tier 2:** Local IPC via Unix Domain Sockets (Linux/macOS) and Named Pipes (Windows: `\\.\pipe\...`) for high throughput (>80k req/s, sub-150µs latency).
       - **Tier 3:** gRPC / Protobuf over HTTP/2 for enterprise microservices and distributed sidecars.
    3. *Function Manifest:* Declared in `.go-anything/bridge.yaml` with typed parameters, return schemas, error codes, timeouts, and idempotency flags.
    4. *Type Mapping:* String serialization for integers $> 2^{53}$ to prevent float64 truncation, Base64 for byte slices, and RFC 3339 for timestamps.
    5. *Error Handling:* Application domain errors map to `-32000`..`-32099` with structured payload `{kind, message, stack}`; host client wrappers catch RPC errors and re-raise native exceptions.
    6. *Resilience & Lifecycle:* Lazy process spawning / pre-warm, `hello` protocol handshake, `ping` health checks, exponential backoff crash recovery, and clean process tree termination.
    7. *Strangler Fallback:* Reversible kill-switch via `GO_ANYTHING_BRIDGE=off` or `GO_ANYTHING_UNIT_<id>=original` falling back to `_original_<name>` and tracking fallback counters for safe lift verification.
