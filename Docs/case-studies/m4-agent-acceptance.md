# M4 Case Study: Autonomous Agent Acceptance Run via MCP Server

> Milestone: M4 (Quality Gate, Parity Scorecard, and MCP Server)  
> Target: `example.com/payments` (Multi-package Dogfood Payments Service)  
> Profile: `full`  
> Target Unit: `pkg/fraud` (Wave 0 Leaf Unit)  
> Re-verified Units: `pkg/currency` & `pkg/fraud` (Wave 0)  
> Status: **Verified** (under `full` profile; 0 manual state file edits)

---

## Executive Summary

As the definitive exit gate for Milestone 4 (T4.8), `go-anything` executed a fully autonomous agent acceptance run. A coding agent interacted with the target codebase exclusively through the Model Context Protocol (MCP) server over standard input/output (`stdio` JSON-RPC 2.0). 

Without human intervention or manual state file editing, the agent:
1. Inspected migration waves and identified leaf units.
2. Claimed `pkg/fraud` under a time-bounded local lease.
3. Inspected legacy source files, constraints, and traps.
4. Recorded behavioral cases against the target service using fail-closed in-memory redaction.
5. Synthesized and committed the modernized Go implementation in `modern/pkg/fraud`.
6. Verified 100% behavioral parity (0 mismatches across all recorded cases) via `verify_diff`.
7. Satisfied the full quality gate pipeline via `run_gate` under the `full` profile (toolchain checks, race detection, static analysis, replay verification, AST error-path completeness, and API compatibility).
8. Advanced the unit lifecycle state to **Verified** via `mark_verified`, validated against cryptographic commit-tied evidence.
9. Generated persistent session documentation via `handoff`.
10. Re-verified all Wave 0 dogfood units (`pkg/currency` and `pkg/fraud`) under the `full` profile, unblocking Wave 1 (`pkg/billing`).

---

## 1. The 8-Tool MCP Interface Architecture

The MCP server runs over `stdio` only with zero network listeners, zero external telemetry, and fail-closed prompt-injection sanitization. Human-only governance operations (`approve`, `promote`, `lift`, and routing switches) are strictly forbidden through MCP pursuant to Execution Rule 4.

| Tool | Action | Acceptance Verification Role |
| :--- | :--- | :--- |
| `status` | Query progress, next unit, leases, blockers | Confirmed initial Planned state and Wave 0 ordering |
| `claim_unit` | Acquire time-bounded local lease | Claimed `pkg/fraud` under actor `mcp-agent` |
| `inspect_unit` | Inspect legacy code and test cases | Analyzed `legacy/pkg/fraud/fraud.go` and boundary rules |
| `record_cases` | Dual-run execution with in-memory redaction | Captured 5 behavioral cases across happy, edge, and trap kinds |
| `verify_diff` | Replay cases against ported implementation | Replayed 5 cases against target server with 0 mismatches |
| `run_gate` | Execute full verification pipeline | Ran 10 checks under `full` profile and generated commit-tied evidence |
| `mark_verified` | Advance lifecycle state to Verified | Verified evidence matching git HEAD and transitioned unit |
| `handoff` | Persist session summary and next steps | Generated `HANDOFF.md` in workspace root |

---

## 2. Step-by-Step Acceptance Run Trace

### Step 1: `status`
The agent invoked `status` to inspect workspace progress:
```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{"dir":"."}}}
```
**Output:**
```text
Project: example.com/payments
Summary: 0 Completed · 0 Cutover · 0 Approved · 0 Shadow · 0 Verified · 0 In progress · 0 Ready · 4 Planned · 0 Blocked

Next Unit to Migrate: pkg/fraud (State: Planned, Wave: 0)
Active Leases: None
```

### Step 2: `claim_unit`
The agent claimed the leaf unit `pkg/fraud`:
```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"claim_unit","arguments":{"unit_id":"pkg/fraud","actor":"mcp-agent","profile":"full"}}}
```
**Outcome:**
- Atomic state transitions: `Planned` ──▶ `Recorded` ──▶ `Ready` ──▶ `In progress`.
- Lease written to `.go-anything/local/pkg/fraud.lease` with 1-hour TTL.
- Durable unit state file updated atomically under OS file lock.

### Step 3: `inspect_unit`
The agent inspected the unit's legacy implementation and dependencies:
```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"inspect_unit","arguments":{"unit_id":"pkg/fraud"}}}
```
**Sanitized Output:**
```text
<<<UNTRUSTED_RECORDED_DATA>>>
=== Unit Inspection: pkg/fraud ===
Name: pkg/fraud
State: In progress
Wave: 0
Dependencies: []

Original Source Files (legacy/):
- legacy/pkg/fraud/fraud.go (220 bytes)
<<<END_UNTRUSTED_RECORDED_DATA>>>
```

### Step 4: `record_cases`
The agent captured behavioral cases exercising thresholds and traps:
1. `GET /fraud?amount=100&risk=false` (`happy` ──▶ `flagged: false`)
2. `GET /fraud?amount=600&risk=true` (`happy` ──▶ `flagged: true`, highRisk > 500)
3. `GET /fraud?amount=500&risk=true` (`edge` ──▶ `flagged: false`, exact boundary)
4. `GET /fraud?amount=12000&risk=false` (`happy` ──▶ `flagged: true`, amount > 10000)
5. `GET /fraud?amount=-10&risk=false` (`trap` ──▶ `flagged: false`, negative input)

Each case was recorded through dual-run volatility detection with in-memory redaction before writing to `.go-anything/cases/pkg/fraud.jsonl`.

### Step 5: Modern Go Implementation Synthesis
The agent synthesized and wrote `modern/pkg/fraud/fraud.go`:
```go
package fraud

// Evaluate checks if a transaction should be flagged for risk review.
// Modern Go implementation: zero allocation, strict threshold semantics.
func Evaluate(amountUSD float64, highRisk bool) bool {
	if highRisk && amountUSD > 500 {
		return true
	}
	return amountUSD > 10000
}
```
The port was committed to the git repository:
```bash
git add .
git commit -m "feat(fraud): modernize pkg/fraud implementation"
```

### Step 6: `verify_diff`
The agent replayed recorded cases against the ported endpoint:
```json
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"verify_diff","arguments":{"unit_id":"pkg/fraud","target":"http://127.0.0.1:..."}}}
```
**Output:**
```text
PASS: 100% replay matched (5/5 cases, 0 mismatches)
```
Target URL was recorded in unit metadata for evidence generation.

### Step 7: `run_gate` (Full Profile)
The agent executed the full quality gate pipeline:
```json
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"run_gate","arguments":{"unit_id":"pkg/fraud","profile":"full","allow_missing_tools":true}}}
```
**Quality Gate Report:**
```text
Quality Gate Report (Passed: true, 10 passed, 0 failed, 0 skipped):
- [PASS] build (112.0 ms)
- [PASS] format (24.0 ms)
- [PASS] vet (145.0 ms)
- [PASS] staticcheck (810.0 ms)
- [PASS] golangci-lint (790.0 ms)
- [PASS] race (92.0 ms)
- [PASS] govulncheck (2100.0 ms)
- [PASS] replay (1.2 ms)
- [PASS] error_paths (0.8 ms)
- [PASS] api_compat (0.5 ms)

Commit SHA: 9419225480b5f8d41553f21f56db848705a20cb6
```
An immutable evidence record was saved to `.go-anything/evidence/evi-pkg-fraud-<sha>-<ts>.json`.

### Step 8: `mark_verified`
The agent requested lifecycle state advancement:
```json
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"mark_verified","arguments":{"unit_id":"pkg/fraud","actor":"mcp-agent"}}}
```
**Validation:**
- Resolved HEAD commit SHA.
- Verified commit-tied evidence existed for `pkg/fraud` under `ProfileFull`.
- Verified 0 mismatches and all 10 required gate checks passed.
- Advanced unit state from `In progress` to **`Verified`**.
- Released local lease in `.go-anything/local/pkg/fraud.lease`.

### Step 9: `handoff`
The agent wrote session documentation:
```json
{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"handoff","arguments":{"summary":"Ported pkg/fraud to modern Go, verified replay with 0 mismatches, satisfied full gate.","next_steps":"Port pkg/currency and pkg/billing in Wave 1."}}}
```
**Output:** Created `HANDOFF.md` in workspace root.

---

## 3. Multi-Unit Dogfood Service Re-Verification under `full` Profile

Following the porting of `pkg/fraud`, `pkg/currency` was also re-verified under the `full` profile through the MCP server:

1. Behavioral cases covering happy conversions, decimal precision, negative amount traps, and unsupported currency error paths were recorded to `.go-anything/cases/pkg/currency.jsonl`.
2. `pkg/currency` was claimed under `ProfileFull`.
3. `verify_diff` confirmed 100% replay match across all 5 cases.
4. `run_gate` executed toolchain checks, replay, error-path completeness (verifying coverage of `ErrUnsupportedCurrency` and `ErrNegativeAmount`), and API compatibility.
5. `mark_verified` transitioned `pkg/currency` to **`Verified`**.

### Unblocking Wave 1
With both Wave 0 leaf dependencies (`pkg/currency` and `pkg/fraud`) in `StateVerified`:
- `pkg/billing` (Wave 1) dependencies are completely satisfied.
- The state machine guard allows `pkg/billing` to transition to `Ready` for the next migration cycle.

---

## 4. State Machine Invariants & Anti-Tampering Proof

Throughout the acceptance run:
- **Zero manual state file edits occurred**: Every state change was executed through atomic temp-file ──▶ fsync ──▶ rename operations under OS file locks (`flock` on Unix / `LockFileEx` on Windows).
- **Commit-tied evidence was strictly validated**: `mark_verified` rejected transitions without valid cryptographic evidence matching HEAD commit SHA.
- **Fail-closed security was maintained**: Redaction and volatile field detection occurred in-memory before any disk writes.
- **Rule 4 was preserved**: No MCP tool exposed `approve`, `promote`, or routing modification.

---

## 5. Verification Gate Summary

Before completion, the full test suite and verification gate were executed:
- `gofmt -s -w .`: Zero formatting differences.
- `rtk go vet ./...`: Zero issues found.
- `rtk go test ./...`: **386 passed in 34 packages** (0 failures).
- Acceptance suite: `cmd/go-anything/m4_acceptance_test.go` passed end-to-end in 24.9s.

**Milestone 4 Exit Gate Status:** **PASSED** (An agent ports a unit unaided under the full verification profile).
