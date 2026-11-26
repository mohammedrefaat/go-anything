# AGENTS.md — AI Agent Guide for go-anything

> Universal instructions for any AI coding agent (Claude, Codex, Gemini, Cursor, Copilot, Windsurf, or others) working on the `go-anything` repository.

---

## What Is go-anything?

`go-anything` migrates applications to Go **one unit at a time** and proves each unit matches the original's recorded behavior before it ships. It follows the strangler pattern.

- **It is not a code translator.** The agent writes the Go implementation.
- **`go-anything` organizes and verifies:** it orders work by dependency waves, records behavioral evidence, replays cases, enforces quality gates, and persists migration state outside any chat session.
- **Parity is claimed only for recorded and fuzzed inputs**, never for unobserved behavior.

**Module:** `github.com/mohammedrefaat/go-anything`
**License:** Apache-2.0
**Go:** 1.27+ (see `go.mod`)
**Status:** Active (v2.0) — all 32 CLI commands are wired with E2E coverage; 14 MCP tools over stdio (see [STATUS.md](STATUS.md)).

---

## Repository Structure

```text
cmd/go-anything/        CLI entry point
pkg/adapterkit/         Public adapter contract (NEVER imports internal/)
internal/               Core engine:
  ├── analyzer/         Source code analysis (go/packages, go/types, call graph)
  ├── app/              Hot-path, fuzz, bench, profile use cases
  ├── bench/            Latency and allocation benchmarks
  ├── bridge/           Strangler sidecar bridge scaffolding
  ├── comparator/       Multi-mode case comparison (exact, structural_json, numeric_tolerance, etc.)
  ├── gate/             Quality gate runner (build, format, vet, staticcheck, golangci-lint, race, govulncheck, replay)
  ├── mcp/              MCP server (stdio only) and payload sanitizer
  ├── migrate/          Workspace schema migration engine
  ├── planner/          Dependency wave planner from import graph
  ├── recorder/         Dual-run case recording with volatility detection
  ├── redactor/         Fail-closed in-memory secret redaction and format-preserving substitution
  ├── replay/           Concurrent deterministic replay engine (errgroup + SetLimit)
  ├── sandbox/          Restricted execution (Linux namespaces / best-effort on macOS+Windows)
  ├── shadow/           Shadow mode engine (bounded pool, idempotent-only default)
  └── state/            Ten-state unit lifecycle, atomic I/O, file locks, local leases
adapters/               Language and application adapters (Go, Python, TS/JS, CLI, HTTP)
schemas/                JSON Schemas:
  ├── v1/               Frozen v1 schemas
  ├── v2/               v2 command output schemas
  └── v1alpha1/         Legacy schemas (upgraded via `go-anything migrate`)
skills/                 Agent workflow skill definitions
templates/              Agent instruction template loader
tools/genstatus/        Generates STATUS.md and MCP tool tables in docs/templates
Docs/                   Architecture and task plans
  ├── decisions/        ADR-001 (sandbox backend), ADR-002 (cross-machine claims)
  ├── go-anything Architecture & Implementation Plan (v3) (1).md
  └── go-anything AI Agent Task Plan (v3.1).md
.github/                CI workflows (go test -race, govulncheck) and Dependabot
```

---

## Execution Rules — MANDATORY

Every agent must obey these rules on every task. Violating any of them makes the task incomplete.

### Rule 1: State and Locking
- Durable unit state: `.go-anything/units/` (one JSON file per unit).
- Atomic writes: temp file → `fsync` → rename, under OS file lock (`flock` on Unix, `LockFileEx` on Windows).
- Leases and locks: `.go-anything/local/` only (gitignored). Never commit them.

### Rule 2: Determinism
- Sort all reports, scorecards, and diffs by `case_id`. **Never** by completion time.
- Respect `--jobs N` (default: `runtime.NumCPU()`).

### Rule 3: Fail-Closed Security
- In-memory redaction is on by default — secrets are substituted before any disk write.
- No-network sandboxing is on by default — `--allow-network` to opt in.
- Zero telemetry, local-only. No external adapter may disable core redaction or shadow policy.

### Rule 4: Human-Only Controls
These commands are **CLI-only** and **never** exposed as MCP tools:
- `approve` — human sign-off
- `promote` — switch routing
- `lift` — remove the bridge and the original
- Any routing change

### Rule 5: Verification Before Completion
Before marking **any** task complete, run and confirm zero failures:
```bash
gofmt -s -w .
go vet ./...
go test -race ./...
```

### Rule 6: Decision Escalation
If a task hits an unsettled architectural question: **stop**, write a short ADR to `DECISIONS.md` and `Docs/decisions/` with a recommended answer, and do not guess.

### Rule 7: Test Style
- Table-driven tests with `github.com/stretchr/testify`.
- Mocks via explicit Go interfaces only.
- No real network calls in unit tests.
- Race detector is mandatory: `go test -race ./...`.

### Rule 8: Public Contract Boundary
- `pkg/adapterkit` must **never** import from `internal/`.
- `Redactor` and `ShadowPolicy` are **never** exported from `adapterkit`.

### Rule 9: Evidence Profiles
Two profiles exist:
- **`replay`**: zero mismatches on recorded cases. Used until the full gate is built (M4).
- **`full`**: all quality gate checks pass. Default from M4 onward. Required for tagged releases.

### Rule 10: Stay in Scope
Only change files listed in a task's Scope. Anything outside requires an ADR.

---

## Unit Lifecycle (10 States)

```
Planned → Recorded → Ready → In Progress → Verified → Shadow → Approved → Cutover → Completed
                                   ↓
                               Blocked (needs human decision)
```

| State | Evidence Required |
| :--- | :--- |
| Planned | Unit in a dependency wave; dependencies and risk recorded |
| Recorded | Redacted cases with wildcards and traps saved |
| Ready | All dependencies Verified or fronted by the original |
| In Progress | Active claim with live local lease |
| Verified | Build, test, replay, API, and gate reports tied to a commit |
| Shadow | Shadow mode on for the unit |
| Approved | Human CLI sign-off recorded |
| Cutover | Route switched and kill switch tested |
| Completed | Fallback traffic at zero; gate re-run on final code |

The state machine is enforced server-side. A transition without evidence is rejected.

---

## MCP Server Interface

<!-- BEGIN GENERATED: mcp-tools -->
The MCP server (`go-anything mcp`, `stdio` only) exposes **14 tools** (agent-accessible):

| Tool | Access | Purpose |
| :--- | :--- | :--- |
| `status` | Agent | Show migration progress, next unit in dependency wave order, active leases, and blockers. |
| `claim_unit` | Agent | Take a Ready migration unit with a time-bounded local lease in .go-anything/local/. |
| `inspect_unit` | Agent | View original source code, sanitized sample cases, detected traps, and constraints for a unit. |
| `record_cases` | Agent | Run the original application, apply fail-closed redaction, and save sanitized cases into .go-anything/cases/. |
| `verify_diff` | Agent | Replay recorded behavioral cases against the ported Go implementation and return PASS or top sanitized mismatches. |
| `run_gate` | Agent | Run the full quality gate pipeline (toolchain checks, error-paths, and API compatibility) and generate commit-tied evidence. |
| `mark_verified` | Agent | Advance unit to Verified state. Strictly rejected unless valid commit-tied evidence exists for the target commit SHA. |
| `handoff` | Agent | Write HANDOFF.md with workspace state, progress, and next steps for the next session or reviewer. |
| `analyze` | Agent | Analyze codebase dependencies and view topological migration wave ordering (read-only). |
| `profile` | Agent | Profile legacy execution and rank candidate hot functions for migration (read-only). |
| `bridge` | Agent | Scaffold strangler sidecar bridge, manifest, and client wrappers (only writes generated scaffold files). |
| `hotpath` | Agent | Execute guided hot-path porting pipeline up to the human boundary; strictly stops before approval (Rule R-1). |
| `fuzz_bench` | Agent | Run differential fuzz testing or latency/throughput benchmarking against recorded cases and target. |
| `shadow_status` | Agent | Inspect front door shadow mode execution metrics, request counts, and mismatch logs (read-only). |

**Not exposed via MCP (CLI-only, Execution Rule 4):**

- `approve`: Human sign-off and state advance to Approved
- `promote`: Front-door routing table cutover
- `lift`: Retirement of original codebase and fallback bridges
- `routing`: Manual routing overrides
- `toggle_shadow`: Live traffic shadow activation
- `kill_switch`: Kill-switch override and emergency fallback engagement
<!-- END GENERATED: mcp-tools -->

---

## Standard Agent Loop

```
1. claim_unit        →  Take the next Ready unit
2. inspect_unit      →  Understand original code, cases, traps
3. Write Go code     →  Stay within the task's declared Scope
4. verify_diff       →  Build + replay → check for mismatches
5. Fix and repeat    →  Until verify_diff returns PASS
6. run_gate          →  Full quality checks + scorecard
7. mark_verified     →  Advance state (requires commit-tied evidence)
8. handoff           →  Write HANDOFF.md for the next session/reviewer
```

### Hot-Path & Sidecar Workflow

```
profile → bridge → go (hotpath) → verify_diff → run_gate → shadow → human approve/promote/lift
```

If you encounter a question not settled by the architecture plan or task plan, **write an ADR and stop**. Do not guess.

---

## Task Plan & Dependencies

The full task backlog is in [Docs/go-anything AI Agent Task Plan (v3.1).md](<Docs/go-anything AI Agent Task Plan (v3.1).md>).

### How to Pick Work
1. Find a task whose dependencies are all complete.
2. Check the **parallel lanes table** (in the task plan) for what can run beside it.
3. Stop at any exit gate that fails.
4. A task is done **only** when its exit gate passes **and** Rule 5 verification passes.

### Milestones

| Milestone | Focus | Exit Gate |
| :--- | :--- | :--- |
| **M0** | Foundation (state, redactor, sandbox, CLI skeleton) | `go-anything init` + `go-anything doctor` pass |
| **M1** | Sandboxed record, replay, comparator (MVP) | 100% replay × 3 identical runs |
| **M2** | Front door, kill switch, lift | Full cycle on demo CLI and HTTP apps |
| **M3** | Go source adapter | Real Go unit ported with zero mismatches |
| **M4** | Quality gate, scorecard, MCP server | Agent ports a unit unaided |
| **M5** | Shadow mode, capture, v0.1.0 release | Clean shadow log + Go case study |
| **M6** | Python adapter, v0.2.0 | Python CLI and API ported |
| **M7** | TypeScript adapter, schema freeze, v1.0.0 | Multi-unit OSS project lifted |

---

## Key Semantic Traps (Go Target)

When porting to Go, generate or watch for cases covering:

| Category | Issue |
| :--- | :--- |
| Empty collections | nil slice → `null` vs empty slice → `[]` |
| HTML escaping | `encoding/json` escapes `<`, `>`, `&` |
| Map key order | Go sorts keys; originals may keep insertion order |
| Numbers | `float64` default for JSON; int64 overflow, `-0`, NaN |
| Strings | UTF-16 vs UTF-8 offsets; rune vs byte slicing; `\r\n` vs `\n` |
| Time | Timezone handling, format strings, sub-second precision |
| Error paths | Malformed input, missing fields, non-zero exit codes |
| `go.mod` directive | Raising `go` changes loop var semantics, `GODEBUG`, `ServeMux` |

---

## Security Constraints for Agents

> Read [SECURITY.md](SECURITY.md) for the full threat model.

- **Redaction is core-owned.** You cannot disable it. Adapters only declare field semantics (`secret`, `volatile`, `ordered`) — the core decides what to redact.
- **Sandbox is on by default.** No network, env allowlist, temp working copy, resource limits.
- **MCP payloads are sanitized.** Truncated strings, stripped control chars, data boundary delimiters. This reduces prompt-injection risk but does not eliminate it.
- **Never bypass** the state machine, skip evidence, or fake gate results.

---

## Workspace Layout (In the Migrated Project)

When `go-anything init` is run on a target project, it creates:

```text
.go-anything/
  manifest.json        Index of units, schema_version
  units/               One durable state file per unit (JSON)
  local/               Leases and locks (GITIGNORED)
  rules.json           Auto-detected wildcards and normalizers
  cases/               Redacted cases, one JSONL per unit
  reports/             Diffs, benchmarks, shadow mismatches, scorecard
  evidence/            Gate reports tied to a git commit
```

For Go-to-Go migrations (`go-anything init --source=go` / `go-anything modernize`):
```text
legacy/                Frozen original (module replace or separate import path)
modern/                New Go implementation
```

---

## Key Documents

| Document | What's In It |
| :--- | :--- |
| [README.md](README.md) | Project overview, quickstart, design principles |
| [SECURITY.md](SECURITY.md) | Threat model, trust-the-repo warning, vulnerability disclosure |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Dev prerequisites, code standards, verification steps |
| [DECISIONS.md](DECISIONS.md) | ADR index with resolved and open decisions |
| [Architecture Plan (v3)](<Docs/go-anything Architecture & Implementation Plan (v3) (1).md>) | Full system design, security model, extensibility, roadmap |
| [Task Plan (v3.1)](<Docs/go-anything AI Agent Task Plan (v3.1).md>) | Ordered task backlog, parallel lanes, exit gates |
| [ADR-001](Docs/decisions/adr-001-sandbox-backend.md) | Sandbox backend decision (Linux namespaces + best-effort fallback) |
| [ADR-002](Docs/decisions/adr-002-cross-machine-claims.md) | Cross-machine claims (PR ownership through v1.0) |

---

## Do NOT

- ❌ Import `internal/` from `pkg/adapterkit/`
- ❌ Expose `approve`, `promote`, `lift`, or routing as MCP tools
- ❌ Add real network calls to unit tests
- ❌ Sort outputs by completion time — always `case_id`
- ❌ Commit `.go-anything/local/` files
- ❌ Disable or bypass fail-closed redaction
- ❌ Guess at architectural decisions — write an ADR
- ❌ Claim full parity — only recorded and fuzzed behavior is verified
- ❌ Skip the verification gate (`gofmt`, `go vet`, `go test -race`)
- ❌ Modify files outside a task's declared Scope without an ADR
