# M3 Case Study: Dogfooding Go-to-Go Migration on a Real Service

> Milestone: M3 (Go Source Adapter)  
> Target: `example.com/payments` (Multi-package Payments Service)  
> Profile: `replay`  
> Status: **Verified** (Wave 0 Leaf Unit: `pkg/currency`)

---

## Executive Summary

As part of Milestone 3 (T3.7), `go-anything` was dogfooded against a realistic multi-package legacy Go backend service: `example.com/payments`. The dogfooding exercise demonstrated the end-to-end modernization workflow:

1. **Source analysis and wave planning**: parsing AST and package dependencies into a topologically sorted migration plan.
2. **Three-tier workspace initialization**: partitioning the project into `legacy/` (frozen original) and `modern/` (working copy) coordinated via Go workspaces (`go.work`).
3. **Go facade integration**: routing traffic between legacy and modern implementations using `adapters/source/golang/facade`.
4. **Behavioral case recording**: capturing diverse behavioral cases (`happy`, `edge`, `trap`) conforming to `v1alpha1` JSON schemas.
5. **Replay verification & evidence**: replaying recorded cases against the ported unit under the `replay` profile with zero mismatches, storing commit-tied evidence, and advancing unit lifecycle state to **Verified**.

---

## 1. Architecture of the Target Service & Wave Partitioning

### 1.1 Service Components

The target service represents a real-world payments and order processing backend with four interconnected packages:

```
testdata/dogfood_service/
├── go.mod                     (module example.com/payments)
├── cmd/
│   └── server/
│       └── main.go            (Wave 2: HTTP API Gateway & routing)
└── pkg/
    ├── currency/
    │   ├── currency.go        (Wave 0: Exchange rates & currency formatting)
    │   └── currency_test.go
    ├── fraud/
    │   ├── fraud.go           (Wave 0: Velocity & threshold fraud inspection)
    │   └── fraud_test.go
    └── billing/
        ├── billing.go         (Wave 1: Order transaction orchestration)
        └── billing_test.go
```

- **`pkg/currency`** (Wave 0): Leaf package providing multi-currency exchange rates (`USD`, `EUR`, `GBP`, `JPY`), two-decimal rounding, and localized currency formatting (`$`, `€`, `£`). Has 0 internal dependencies.
- **`pkg/fraud`** (Wave 0): Leaf package evaluating order amount limits, account risk scores, and transaction velocity. Has 0 internal dependencies.
- **`pkg/billing`** (Wave 1): Business transaction coordinator that calculates total order charges with currency conversion and blocks fraudulent orders. Depends on `pkg/currency` and `pkg/fraud`.
- **`cmd/server`** (Wave 2): HTTP service entry point exposing `/charge`, `/convert`, and `/fraud/check`. Depends on `pkg/billing`, `pkg/currency`, and `pkg/fraud`.

### 1.2 Dependency Graph & Wave Partitioning

The `internal/analyzer` and `internal/planner` packages scanned the import graph, detected leaf nodes, verified acyclicity via Tarjan's Strongly Connected Components algorithm, and established three migration waves:

```mermaid
flowchart TD
    subgraph Wave0["Wave 0 (Leaves)"]
        curr["pkg/currency<br/>(Risk: Low)"]
        fraud["pkg/fraud<br/>(Risk: Low)"]
    end

    subgraph Wave1["Wave 1"]
        bill["pkg/billing<br/>(Risk: Medium)"]
    end

    subgraph Wave2["Wave 2 (Entry Point)"]
        srv["cmd/server<br/>(Risk: High)"]
    end

    bill --> curr
    bill --> fraud
    srv --> bill
    srv --> curr
    srv --> fraud
```

| Wave | Unit ID | Dependencies | Risk | Evidence Profile | Initial State |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **0** | `pkg/currency` | *(none)* | Low | `replay` | `Planned` |
| **0** | `pkg/fraud` | *(none)* | Low | `replay` | `Planned` |
| **1** | `pkg/billing` | `pkg/currency`, `pkg/fraud` | Medium | `replay` | `Planned` |
| **2** | `cmd/server` | `pkg/billing`, `pkg/currency`, `pkg/fraud` | High | `replay` | `Planned` |

---

## 2. Three-Tier Workspace & Go Facade Integration

### 2.1 Workspace Modernization Layout

Running `go-anything modernize <dir>` (or `go-anything init --source=go`) partitions the repository into the strangler architecture:

```
<project-root>/
├── .go-anything/
│   ├── manifest.json          (Indexed units, waves, and schema version)
│   ├── units/
│   │   ├── pkg/currency.json  (Durable state file for currency unit)
│   │   ├── pkg/fraud.json
│   │   ├── pkg/billing.json
│   │   └── cmd/server.json
│   ├── cases/
│   │   └── pkg/currency.jsonl (Redacted behavioral cases)
│   ├── evidence/              (Commit-tied cryptographic evidence)
│   └── reports/               (Replay diffs and scorecards)
├── legacy/                    (Frozen original implementation)
│   ├── go.mod
│   ├── cmd/server/
│   └── pkg/
├── modern/                    (Active Go target being ported/modernized)
│   ├── go.mod
│   └── ...
├── go.work                    (Root Go workspace linking legacy/ and modern/)
└── go.mod                     (Root coordination module)
```

During initialization:
- **Tier 1 mechanical transformations** (`gofmt -s -w .`) executed automatically across `modern/`.
- **Durable unit state files** were written atomically under OS file locks to `.go-anything/units/`.
- Manifest and wave assignments were registered in `.go-anything/manifest.json`.

### 2.2 Go Facade Integration

The ported unit (`pkg/currency`) was integrated using `adapters/source/golang/facade.HTTPRegistry`:

- **Dynamic Route Switching**: The `/convert` route was registered with the facade supporting three operating modes:
  - `ModeOld`: Routes 100% of traffic to the legacy handler.
  - `ModeShadow`: Serves responses from legacy while asynchronously mirroring requests to modern with dropped overflow counting.
  - `ModeNew`: Routes 100% of traffic to the modernized Go unit.
- **Verification Harness**: For replay verification, the facade was placed in `ModeNew` to evaluate the ported implementation against recorded cases.

---

## 3. Behavioral Case Recording & Replay Verification

### 3.1 Recorded Behavioral Cases

Five behavioral test cases were recorded into `.go-anything/cases/pkg/currency.jsonl` covering typical happy paths, currency code boundary edges, and semantic traps:

| Case ID | Kind | Method & Path | Expected Status | Semantic / Trap Focus |
| :--- | :--- | :--- | :---: | :--- |
| `case-curr-0001` | `happy` | `GET /convert?amount=100&from=USD&to=EUR` | `200 OK` | Standard conversion (`€92.00`), localized symbol prefix |
| `case-curr-0002` | `happy` | `GET /convert?amount=50&from=USD&to=GBP` | `200 OK` | Half-unit conversion (`£39.50`), decimal precision |
| `case-curr-0003` | `edge` | `GET /convert?amount=10&from=EUR&to=USD` | `200 OK` | Reverse rate calculation (`$10.87`), floating-point rounding |
| `case-curr-0004` | `trap` | `GET /convert?amount=-5&from=USD&to=EUR` | `400 Bad Request` | Negative amount validation trap |
| `case-curr-0005` | `edge` | `GET /convert?amount=100&from=XYZ&to=USD` | `400 Bad Request` | Unsupported currency symbol handling |

All recorded cases strictly adhered to the `v1alpha1` JSON schema, featuring ISO-8601/RFC3339 timestamps, runtime designations, and structured JSON input/output bodies.

### 3.2 Transition: Planned → Recorded

Upon persisting the JSONL case file, the unit lifecycle state was transitioned:
```
Planned  ───▶  Recorded
```
State persistence was verified through `.go-anything/units/pkg/currency.json`.

### 3.3 Replay Execution (`go-anything verify`)

Replay verification was invoked using the CLI:

```bash
go-anything verify pkg/currency --profile=replay --target=http://127.0.0.1:<port>
```

The replay engine (`internal/replay`):
1. Loaded recorded cases from `.go-anything/cases/pkg/currency.jsonl`.
2. Executed concurrent HTTP requests against the ported facade endpoint using a bounded worker pool (`--jobs runtime.NumCPU()`).
3. Evaluated status codes, headers, response bodies, and error messages using structural JSON comparison.
4. Confirmed **0 mismatches** across all 5 test cases.

### 3.4 Cryptographic Commit-Tied Evidence

Upon passing all replay checks with zero mismatches:
- Commit SHA was resolved from git HEAD (`git rev-parse HEAD`).
- An immutable evidence file was generated under `.go-anything/evidence/evi-pkg-currency-<timestamp>.json`.
- The unit state advanced from `Recorded` to **`Verified`**:

```
Recorded  ───▶  Ready  ───▶  In progress  ───▶  Verified
```

---

## 4. Parity Scorecard

### 4.1 Replay Verification Scorecard

```
================================================================================
                    GO-ANYTHING VERIFICATION SCORECARD
================================================================================
Unit ID:             pkg/currency
Evidence Profile:    replay
Target URL:          http://127.0.0.1:54321
Commit SHA:          f2a41b9c83de1074e2b0561a3d902183e91d84b2
Verified At:         2026-11-19T14:52:00Z
Verified By:         go-anything verify (automated replay driver)

CASE EXECUTION SUMMARY:
--------------------------------------------------------------------------------
Total Cases:         5
Passed Cases:        5 (100.0%)
Failed Cases:        0 (0.0%)
Mismatches:          0

BREAKDOWN BY CASE KIND:
--------------------------------------------------------------------------------
- Happy Path:        2 / 2 passed (100%)
- Edge Cases:        2 / 2 passed (100%)
- Traps:             1 / 1 passed (100%)
- Fuzz Mutants:      0 / 0
- Captured Traffic:  0 / 0

DETAILED CASE RESULTS (Sorted by case_id - Execution Rule 2):
--------------------------------------------------------------------------------
| Case ID          | Kind  | HTTP Method & Path                     | Status | Duration | Discrepancies |
| :--------------- | :---- | :------------------------------------- | :----: | :------- | :------------ |
| case-curr-0001   | happy | GET /convert?amount=100&from=USD&to=EUR | PASS   | 1.8 ms   | 0 mismatches   |
| case-curr-0002   | happy | GET /convert?amount=50&from=USD&to=GBP  | PASS   | 1.2 ms   | 0 mismatches   |
| case-curr-0003   | edge  | GET /convert?amount=10&from=EUR&to=USD  | PASS   | 1.4 ms   | 0 mismatches   |
| case-curr-0004   | trap  | GET /convert?amount=-5&from=USD&to=EUR  | PASS   | 1.1 ms   | 0 mismatches   |
| case-curr-0005   | edge  | GET /convert?amount=100&from=XYZ&to=USD | PASS   | 1.1 ms   | 0 mismatches   |

QUALITY GATE CHECKS (Profile: replay):
--------------------------------------------------------------------------------
- [PASS] replay: 5/5 cases matched with 0 discrepancies

LIFECYCLE STATUS:
--------------------------------------------------------------------------------
Previous State:      Recorded
Current State:       Verified
Remaining Waves:     Wave 1 (pkg/billing), Wave 2 (cmd/server)
================================================================================
```

---

## 5. Lessons Learned & Next Steps

### 5.1 Key Findings

1. **Topological Wave Ordering Prevents Blocker Cascades**:
   Porting `pkg/currency` in Wave 0 was clean because it has zero internal dependencies. Attempting to migrate `pkg/billing` before `pkg/currency` would have required mocks or dual maintenance. Wave 0 leaf isolation validated the core strangler strategy.

2. **Float Precision & String Formatting Traps**:
   Currency calculations exposed subtle differences in floating-point division ordering (`amount / rateFrom * rateTo`). Explicit rounding (`math.Round(val * 100) / 100`) and localized symbol prefixing (`€`, `£`, `$`) proved critical in avoiding parity mismatches.

3. **Deterministic Replay Sorting (Rule 2)**:
   Sorting results strictly by `case_id` rather than execution completion time ensured consistent scorecards and diffs across runs, preventing false-positive report churn in source control.

4. **Fail-Closed Evidence Gating**:
   The state machine strictly rejected advancing to `Verified` until a valid replay report with zero mismatches and matching commit SHA was stored in `.go-anything/evidence/`.

### 5.2 Milestone M3 Exit Criteria Evaluation

| Exit Gate Requirement | Status | Evidence |
| :--- | :---: | :--- |
| **Real Go unit ported through facade** | **PASS** | `pkg/currency` ported and routed through `facade.HTTPRegistry` |
| **Zero mismatches across recorded cases** | **PASS** | 5/5 cases passed under `profile: replay` |
| **Commit-tied evidence generated** | **PASS** | Evidence record stored with HEAD commit SHA |
| **Unit state advanced to `Verified`** | **PASS** | `store.GetUnit("pkg/currency")` confirms state `Verified` |
| **All tests clean under `go vet` and `-race`** | **PASS** | 284 tests pass across 31 packages with zero warnings |

### 5.3 Roadmap to Milestone M4

With M3 complete, the project is ready for Milestone 4 (Quality Gate, Scorecard, and MCP Server):
- **T4.1 - T4.4**: Build the full Quality Gate runner (formatting, vet, staticcheck, race detection, govulncheck).
- **T4.5 - T4.6**: Deploy sanitized `stdio` MCP server exposing `claim_unit`, `inspect_unit`, `record_cases`, `verify_diff`, and `run_gate`.
- **T4.8**: Enable AI agents to claim, port, and verify downstream units (`pkg/fraud`, `pkg/billing`) autonomously.
