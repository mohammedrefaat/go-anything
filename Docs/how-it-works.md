# How go-anything Works

`go-anything` migrates applications to Go **one unit at a time** and proves each unit matches the original's recorded behavior before it ships. It follows Martin Fowler's **Strangler Fig pattern**.

---

## Core Philosophy

- **`go-anything` is NOT an automated code translator.** Translators generate unidiomatic, unmaintainable code that mimics legacy language quirks. Instead, human developers or AI coding agents write clean, idiomatic Go.
- **`go-anything` organizes, records, and verifies.** It builds a topological dependency graph, records behavioral evidence from the running original application, deterministically replays those inputs against the Go port, runs a fail-closed quality gate, and manages shadow routing until cutover.
- **Parity is claimed only for observed and fuzzed inputs (Rule R-6).** We never claim equivalence for unobserved code paths or unspecified behaviors.

---

## Architecture Flow

```text
+-------------------+      +-------------------+      +-------------------+
|  1. Analysis &    | ---> | 2. Sandboxed      | ---> | 3. Implementation |
|  Dependency Waves |      |    Recording      |      |    & Scaffolding  |
+-------------------+      +-------------------+      +-------------------+
                                                        |
                                                        v
+-------------------+      +-------------------+      +-------------------+
|  6. Shadow Mode   | <--- | 5. Quality Gate   | <--- | 4. Deterministic  |
|  & Human Cutover  |      |    Scorecard      |      |    Diff Replay    |
+-------------------+      +-------------------+      +-------------------+
```

---

## The Strangler Fig Workflow

### 1. Analysis and Wave Planning
When initialized on a legacy repository (`go-anything init --source=<lang>`), the language adapter analyzes package manifests, ASTs, and import graphs. Units are planned topologically into sequential waves:
- **Wave 0:** Leaf dependencies (libraries, utilities, stateless functions).
- **Wave 1..N:** Dependent domain services, data handlers, and API routes.
- **Wave Final:** Application entry points and HTTP front-doors.

### 2. Dual-Run Sandboxed Case Recording
Behavioral cases are recorded either from existing unit/integration test suites (`go-anything record --from-tests`) or by executing CLI commands and HTTP requests against the sandbox:
- **Dual-run volatility detection:** Runs each input twice. Fields that change across runs (timestamps, session IDs, monotonic counters) are flagged as volatile and parameterized with wildcards.
- **In-memory redaction (Rule R-4):** All passwords, authorization headers, tokens, and secrets are substituted in memory *before* being written to disk (`.go-anything/cases/<unit>.jsonl`).

### 3. Agent Implementation & Leases
An agent or developer claims a Ready unit:
```bash
go-anything claim --unit-id pkg/crypto --actor agent-1
```
This acquires a time-bounded local file lease under OS locks. The agent writes idiomatic Go inside the unit's declared scope. Scaffolds return `ErrNotPorted` to ensure no incomplete code is silently used.

### 4. Deterministic Replay & Comparator
The replay engine feeds recorded inputs into the Go implementation and compares outputs:
- **Case sorting:** Diffs and scorecards are always sorted deterministically by `case_id` (Rule 2).
- **Comparison modes:** Exact binary match, structural JSON comparison, numeric float tolerance, or HTTP header/status matching.
- **Divergence:** Any unexpected output or panic blocks transition.

### 5. Quality Gate Pipeline
The gate runner verifies the port against 8 mandatory checks:
- `build`: Clean Go compilation.
- `format`: Code formatted with `gofmt -s`.
- `vet`: Clean `go vet` run.
- `race`: Zero race conditions via `go test -race`.
- `govulncheck`: Zero known CVE vulnerabilities.
- `replay`: 100% match on recorded cases (0 mismatches).
- `error_paths`: Correct handling of negative and malformed inputs.
- `api_compat`: Exported type and signature parity.

When all checks pass, commit-tied evidence is cryptographically recorded in `.go-anything/evidence/` tied to the HEAD commit SHA.

### 6. Shadow Mode & Human Routing Cutover (Rule R-1)
Before switching live traffic:
- **Shadow Mode:** The front door sends live production requests concurrently to the legacy system and the Go port. Side effects are guarded; differences are logged.
- **Human-Only Sign-Off:** Once shadow traffic runs clean with 0 divergences, a human operator issues the CLI cutover commands:
  ```bash
  go-anything approve --unit-id pkg/crypto
  go-anything promote --unit-id pkg/crypto
  ```
  These commands are strictly forbidden from MCP tools or automated scripts.
- **Lift:** When fallback traffic reaches zero, `go-anything lift` removes legacy bridge wrappers and finalizes pure Go modernization.
