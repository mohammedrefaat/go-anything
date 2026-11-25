# V2 Adjusted Task Plan (Subsequent Tasks)

**Date:** November 22, 2026  
**Origin:** Derived from Task T0 Discovery (`Docs/V2_DISCOVERY.md`)  
**Scope:** Actionable adjustments to `Docs/go-anything AI Agent Task Plan (v3.1).md` across Milestones M0 through M7.

---

## 1. Executive Context & Rationale

Task T0 verified that the foundational architecture, 40 packages, schemas, and MCP tools are in place. However, real-world execution on Windows and non-CGO developer environments identified specific operational friction points that require formal adjustments in subsequent task descriptions and exit gates:

1. **Host-Toolchain Incompatibility with Strict Gate Profile:** Commit `11f628b` tightened the `full` evidence profile to require `race: pass` without exception. On platforms lacking CGO (`CGO_ENABLED=0`), `go test -race` cannot execute, breaking MCP `run_gate` and acceptance tests.
2. **Stubbed CLI Commands:** Five subcommands (`analyze`, `diff`, `gate`, `shadow`, `cutover`) in `cmd/go-anything/main.go` remain stubbed and must be explicitly scheduled for CLI wiring.
3. **Advanced Schema Presence:** Schemas for `v1` and `internal/migrate` are already implemented ahead of Milestone M7, allowing M7 to pivot towards freeze verification and migration robustness.
4. **Distribution Pipeline Gaps:** GoReleaser builds multi-arch archives and signs with Cosign, but lacks Scoop and Homebrew bucket automation.

---

## 2. Priority Adjustments by Milestone

### Milestone M0: Foundation & Environment Compatibility

#### [ADJUSTED] Task T0.8: Cross-Platform Toolchain & CGO Accommodation in Quality Gate
- **Problem Discovered:** Commit `11f628b` enforces that `race` must have status `pass` under the `full` evidence profile. On Windows and Alpine hosts lacking a C compiler, `defaultRaceExecutor` returns `ErrSkipped`, causing `requiredChecksPassed` in `internal/mcp/tools.go` and `validateEvidenceForUnit` in `internal/state/machine.go` to reject evidence generation.
- **Adjustment:**
  - Introduce an environment-aware resolution in `internal/gate`: when `allow_missing_tools: true` is passed, or when the host environment detects `CGO_ENABLED=0` / absence of a C compiler, permit `race` to record `StatusSkipped` with an explicit reason (`"race detector requires CGO; skipped in non-CGO environment"`), OR define a `local` profile distinct from CI `full`.
  - Update `internal/mcp/tools.go` (`requiredChecksPassed`) to honor `allow_missing_tools` when skipping optional toolchain checks on local agent runs.
  - Retain strict `race: pass` in GitHub Actions CI where CGO is guaranteed on Linux runners.
- **Exit Gate:** `go test ./...` passes with 100% green tests (620/620) on Windows without CGO, and `TestMCP_All8Tools_EndToEnd` passes.

---

### Milestone M1: Sandboxed Record, Replay & Comparator (CLI Wiring)

#### [ADJUSTED] Task T1.6: Wire `go-anything diff` CLI Subcommand
- **Problem Discovered:** `cmd/go-anything/main.go` currently stubs `diff` with a placeholder message.
- **Adjustment:**
  - Connect `cmd/go-anything/diff.go` to `internal/comparator` and `internal/replay`.
  - Flags: `--unit <id>`, `--target <bin/url>`, `--mode <exact|structural_json|numeric_tolerance>`, `--jobs <N>`.
  - Output: Format structural differences with field-level paths, expected vs actual values, and masked secrets.
- **Exit Gate:** `go-anything diff --unit <id> --target <target>` runs replay, compares outputs, and displays colored diffs sorted by `case_id`.

---

### Milestone M2: Front Door, Kill Switch & Strangler Lift

#### [ADJUSTED] Task T2.6: Wire `go-anything cutover` CLI Subcommand
- **Problem Discovered:** `cmd/go-anything/main.go` currently stubs `cutover` with a placeholder message.
- **Adjustment:**
  - Connect `cmd/go-anything/cutover.go` to `internal/frontdoor` and `internal/state`.
  - Behavior: Verifies unit is in `Approved` state; switches primary routing in the front door configuration to the Go implementation; tests the kill switch (`GO_ANYTHING_UNIT_<id>=original`); advances unit state to `Cutover`.
- **Exit Gate:** Invoking `go-anything cutover <unit>` verifies front door switch, tests kill switch fallback, and transitions unit state from `Approved` to `Cutover`.

---

### Milestone M3: Go Source Modernization & Analysis

#### [ADJUSTED] Task T3.8: Wire `go-anything analyze` CLI Subcommand
- **Problem Discovered:** `cmd/go-anything/main.go` currently stubs `analyze` with a placeholder message.
- **Adjustment:**
  - Connect `cmd/go-anything/analyze.go` to `internal/analyzer` and `internal/planner`.
  - Behavior: Scans source code (`go/packages`, `go/types`), computes import graphs, detects cycles, and prints topological dependency waves in table and JSON formats.
  - Flags: `--source <go|python|typescript>`, `--json`, `--output <file>`.
- **Exit Gate:** `go-anything analyze ./...` prints verified dependency waves matching `internal/planner` unit waves.

---

### Milestone M4: Quality Gate, Scorecard & MCP Full Automation

#### [ADJUSTED] Task T4.9: Wire `go-anything gate` CLI Subcommand
- **Problem Discovered:** `cmd/go-anything/main.go` currently stubs `gate` with a placeholder message.
- **Adjustment:**
  - Connect `cmd/go-anything/gate.go` to `internal/gate` and `internal/gate/scorecard`.
  - Flags: `--unit <id>`, `--profile <replay|full>`, `--allow-missing-tools`, `--scorecard`.
  - Output: Execute the configured quality gate pipeline, print status table per check, generate `.go-anything/reports/scorecard.md`, and write commit-tied evidence to `.go-anything/evidence/`.
- **Exit Gate:** `go-anything gate --unit <id> --profile full` executes all gate checks and writes commit-tied evidence identical to MCP `run_gate`.

#### [ADJUSTED] Task T4.10: Re-run M4 Agent Acceptance in Non-CGO & CI Environments
- **Adjustment:** Ensure `cmd/go-anything/m4_acceptance_test.go` (`TestM4_AgentAcceptanceRun`) tests both:
  1. Local agent flow with `allow_missing_tools: true` on Windows/macOS.
  2. Strict full profile with all tools passing on Linux CI.
- **Exit Gate:** `TestM4_AgentAcceptanceRun` passes on both local developer workstations and CI runners.

---

### Milestone M5: Shadow Mode & Production Capture

#### [ADJUSTED] Task T5.5: Wire `go-anything shadow` CLI Subcommand
- **Problem Discovered:** `cmd/go-anything/main.go` currently stubs `shadow` with a placeholder message.
- **Adjustment:**
  - Connect `cmd/go-anything/shadow.go` to `internal/shadow` and `adapters/application/http/frontdoor`.
  - Flags: `--unit <id>`, `--port <N>`, `--sample-rate <float>`, `--allow-side-effects`, `--output <file>`.
  - Behavior: Starts front door in shadow mode; clones incoming requests to the Go port; logs behavioral mismatches to `.go-anything/reports/shadow_mismatches.jsonl`.
- **Exit Gate:** Live traffic sent to shadow proxy records expected differences without impacting primary responses.

#### [ADJUSTED] Task T5.6: Package Distribution Automation (Homebrew & Scoop)
- **Problem Discovered:** `.goreleaser.yaml` currently builds standalone tar.gz and zip archives, but lacks Homebrew and Scoop integration.
- **Adjustment:**
  - Add Homebrew tap configuration block to `.goreleaser.yaml` targeting a dedicated repository (e.g., `mohammedrefaat/homebrew-tap`).
  - Add Scoop bucket configuration block to `.goreleaser.yaml` for Windows package management.
- **Exit Gate:** `goreleaser check` validates syntax and configuration for brew and scoop blocks.

---

### Milestone M6: Python Adapter & Worker Migration

#### [ADJUSTED] Task T6.5: Python Transport Specialization (Subprocess / HTTP Prioritized over CShared)
- **Problem Discovered:** `adapters/source/python/cshared` was found to have severe runtime constraints (GIL deadlocks, signal handler conflicts between Go and Python runtimes).
- **Adjustment:**
  - Keep `cshared` strictly as an experimental research package.
  - Direct all production Python migration units to use `TransportSubprocess` (CLI/scripts) and `TransportHTTP` (FastAPI/Flask services).
- **Exit Gate:** Python end-to-end dogfooding verifies 100% replay match across standard subprocess and HTTP transports without invoking `cshared`.

---

### Milestone M7: TypeScript Adapter, Schema Freeze & Safe Lift

#### [ADJUSTED] Task T7.5: Schema Freeze Validation & Migration Stress Testing
- **Problem Discovered:** `schemas/v1/` and `internal/migrate` are already present in the codebase.
- **Adjustment:**
  - Shift focus from initial schema drafting to comprehensive backwards-compatibility testing:
    1. Validate migration of workspaces initialized under `v1alpha1` containing legacy units, rules, and cases.
    2. Verify `go-anything migrate` idempotency (running migrate on an already upgraded `v1` workspace produces zero changes).
    3. Run schema validation across all existing test fixtures.
- **Exit Gate:** `TestMigrate_V1Alpha1_To_V1` and schema conformance tests pass with 100% assertions satisfied.

---

## 3. Parallel Execution Lanes (Revised)

Following the adjustments above, the updated execution lanes are:

| Milestone | Immediate Parallel Lanes | Must Wait For |
| :--- | :--- | :--- |
| **M0 (Fix)** | **T0.8** (Toolchain / CGO resolution) | T0 Discovery complete |
| **M1** | T1.1 (Dual-run recorder) with T1.3 (Comparator); **T1.6** (Diff CLI) | T0.8 (Clean test suite) |
| **M2** | T2.1 (HTTP front door) with T2.2 (CLI front door); **T2.6** (Cutover CLI) | M1 exit gate |
| **M3** | T3.4 (Modernize engine) with T3.5 (Parity fuzzing); **T3.8** (Analyze CLI) | M2 exit gate |
| **M4** | **T4.9** (Gate CLI) with T4.5 (Sanitizer); **T4.10** (Dual-mode acceptance) | M3 exit gate |
| **M5** | **T5.5** (Shadow CLI) with T5.2 (Traffic mirror); **T5.6** (Brew/Scoop) | M4 exit gate |
| **M6** | T6.2 (Py AST) with T6.4 (Worker sidecars); focus on Subprocess/HTTP | M5 exit gate |
| **M7** | T7.1 (TS Adapter) with **T7.5** (Schema Freeze & Stress Test) | M6 exit gate |
