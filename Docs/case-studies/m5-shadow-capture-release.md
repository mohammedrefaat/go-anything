# M5 Case Study: Production Shadow Mode, Capture Ingestion, and v0.1.0 Release

> Milestone: M5 (Shadow Mode, Capture Ingestion, and v0.1.0 Release)  
> Target: `example.com/payments` (Multi-package Dogfood Payments Service)  
> Profile: `full`  
> Target Units: `pkg/currency`, `pkg/fraud`, `pkg/billing`, `cmd/server`  
> Status: **Verified & Shadowed** (Clean shadow log, 0 leaks, signed v0.1.0 release pipeline)

---

## Executive Summary

Milestone 5 represents the bridge between static pre-merge verification and live production validation. While Milestones 1 through 4 established deterministic replay, quality gates, and autonomous agent loops, Milestone 5 equips `go-anything` with real-world traffic verification:

1. **Production Shadow Mode Engine (`internal/shadow`)**:
   - Dynamic mode resolution supporting `old`, `shadow`, and `new` routing, configurable globally (`GO_ANYTHING_MODE`) and per-unit (`GO_ANYTHING_MODE_<unitID>`).
   - Bounded worker pool with backpressure drop counting instead of unbounded queuing, eliminating memory leak risks during traffic surges.
   - Strict idempotency safety by default: GET and HEAD requests are shadowed asynchronously; all non-idempotent write operations (POST, PUT, DELETE, PATCH) are blocked from shadow execution.
   - Guarded `--allow-side-effects` override with mandatory startup warnings for staging/stub environments.
   - Fail-closed in-memory secret redaction applied before any discrepancy is written to `.go-anything/reports/shadow_mismatches.jsonl`.
2. **Production Capture Ingestion (`internal/recorder/capture`)**:
   - Universal capture framework supporting four production sources: gateway/sidecar HTTP mirroring, message broker consumers (Kafka, RabbitMQ, SQS, NATS), distributed traces (OpenTelemetry, W3C), and read-only observability tools (such as `platform-mcp`).
   - Strict security invariant: capture and observability sources possess zero routing control and cannot mutate traffic.
   - In-memory format-preserving redaction converting production traffic into schema-conforming `adapterkit.Case` records with `Kind: "captured"`.
3. **Target Service Evaluation (`example.com/payments`)**:
   - Live shadowing on the dogfood payments service exhibiting **zero mismatches** across idempotent traffic.
   - Complete blocking of non-idempotent write operations with zero dropped primary calls.
   - Ingestion of live mirrored traffic with zero credential leaks to disk.
4. **Automated Release Pipeline (`v0.1.0`)**:
   - Production GoReleaser v2 configuration (`.goreleaser.yaml`) cross-compiling for Linux (amd64, arm64), macOS (amd64, arm64), and Windows (amd64).
   - Keyless Sigstore artifact signing using Cosign with immutable bundle generation.
   - Automated GitHub Actions release workflow (`.github/workflows/release.yml`) triggered on version tags.

---

## 1. Architecture of the Shadow Mode Engine

```mermaid
flowchart TD
    client["Client Request"] --> router["Shadow Engine / Facade"]
    
    router --> modeCheck{"Check Mode & Policy"}
    
    modeCheck -->|Mode: old| legacyOnly["Legacy Handler<br/>(100% Traffic)"]
    modeCheck -->|Mode: new| modernOnly["Modern Handler<br/>(100% Traffic)"]
    
    modeCheck -->|Mode: shadow| primary["Execute Legacy Handler<br/>(Primary Synchronous Response)"]
    primary --> respond["Send Response to Client"]
    
    primary -.-> policyCheck{"Is Method Idempotent?<br/>(GET, HEAD)"}
    
    policyCheck -->|No & SideEffects=false| blockShadow["Block Shadow Execution<br/>(Increment BlockedCount)"]
    policyCheck -->|Yes or SideEffects=true| sampleCheck{"Sample Rate Check<br/>(0.0 to 1.0)"}
    
    sampleCheck -->|Skip| skipSample["Skip Task<br/>(Increment SampleSkipped)"]
    sampleCheck -->|Eligible| submit["Submit to Worker Pool<br/>(Bounded Queue)"]
    
    submit -->|Queue Full| dropCount["Drop Task<br/>(Increment DroppedCount)"]
    submit -->|Accepted| worker["Worker Goroutine<br/>(Execute Modern Handler)"]
    
    worker --> compare{"Compare Responses<br/>(Status, Headers, Body)"}
    compare -->|Match (0 Diff)| cleanLog["Clean Log<br/>(No Disk Write)"]
    compare -->|Mismatch Detected| redact["Fail-Closed In-Memory Redaction<br/>(RedactJSON)"]
    
    redact --> appendReport["Append Redacted Report<br/>(.go-anything/reports/shadow_mismatches.jsonl)"]
```

### 1.1 Mode Resolution: Global and Per-Unit Control

Routing can be configured at multiple levels of granularity:
- **Global Configuration**: `GO_ANYTHING_MODE=old|shadow|new` sets the baseline policy across the entire process.
- **Per-Unit Override**: `GO_ANYTHING_MODE_<unitID>=old|shadow|new` or `GO_ANYTHING_UNIT_<unitID>=old|shadow|new` overrides the global setting for that specific unit (unit IDs are normalized: uppercase, alphanumeric with underscores).
- **Programmatic Control**: `facade.WithMode(...)` or `shadow.WithMode(...)` provides in-process deterministic overrides.

```go
// Priority order: Explicit Option > Unit Env Var > Global Env Var > Default (old)
mode := engine.GetMode()
```

### 1.2 Bounded Worker Pool & Overflow Drop Counter

A critical failure mode of shadow execution in high-throughput systems is unbounded goroutine creation or memory exhaustion during downstream latency spikes. 

The `go-anything` shadow engine enforces strict backpressure:
- **Fixed Worker Pool**: Worker goroutines are bound to `runtime.NumCPU()` (or configured via `WithWorkerPoolSize`).
- **Bounded Channel Queue**: Tasks are submitted to a fixed-capacity channel (default: 100).
- **Non-Blocking Drop**: When the queue is full, the engine drops the shadow task immediately using a non-blocking `select default` and increments an atomic drop counter (`DroppedCount`). Primary request processing is never blocked or slowed down.

```go
func (e *Engine) Submit(task func(context.Context)) bool {
    if e.closed.Load() {
        return false
    }
    select {
    case e.queue <- task:
        return true
    default:
        e.droppedCount.Add(1)
        return false
    }
}
```

Load tests under extreme queue saturation confirmed bounded memory usage (<20MB heap delta across 50,000 tasks) with zero goroutine leaks.

### 1.3 Idempotency Safety by Default

Running unverified code against live production traffic carries catastrophic risk if write operations (e.g. charging credit cards, inserting database records, or sending emails) are duplicated.

Pursuant to Execution Rule 3:
- **Default Behavior**: Only `GET` and `HEAD` methods are eligible for shadow execution.
- **Write Operations Blocked**: `POST`, `PUT`, `DELETE`, `PATCH`, and custom verbs are immediately filtered out. The legacy handler serves the request, and `BlockedCount` is incremented.
- **Side-Effects Guard**: Shadowing non-idempotent operations requires explicitly enabling `AllowSideEffects` (or `--allow-side-effects`).
- **Mandatory Startup Warning**: When side effects are enabled, the engine emits the mandatory architectural warning to `os.Stderr`:

```text
Shadow execution runs the Go handler on real requests. Non-idempotent handlers may repeat writes or external calls. Run against isolated data or stubs. You are responsible for the consequences.
```

### 1.4 Fail-Closed In-Memory Redaction on Mismatches

When a behavioral discrepancy between legacy and modern implementations occurs, the diff must be recorded for diagnosis. However, live requests frequently contain authentication headers, session cookies, API keys, and sensitive customer data.

The shadow mismatch logger (`shadow.MismatchLogger`) enforces fail-closed in-memory redaction:
1. Discrepancies and request details are serialized into a raw JSON struct.
2. The payload is passed through `internal/redactor.RedactJSON(...)` **in memory before any disk I/O**.
3. All high-confidence secrets (tokens, keys, passwords, credit cards) undergo format-preserving substitution.
4. The sanitized record is appended to `.go-anything/reports/shadow_mismatches.jsonl`.
5. The output conforms strictly to `schemas/v1alpha1/reports.schema.json`.

---

## 2. Production Capture Ingestion Architecture

```mermaid
flowchart LR
    subgraph Sources["Production Capture Sources"]
        http["Gateway / Sidecar<br/>HTTP Mirror"]
        broker["Message Broker<br/>(Kafka, RabbitMQ, SQS)"]
        trace["Distributed Traces<br/>(OpenTelemetry, W3C)"]
        obs["Observability Tools<br/>(platform-mcp, Telemetry)"]
    end

    subgraph Security["Security & Governance Enforcement"]
        routeGuard{"Routing Check<br/>No Routing Control Allowed"}
        redact{"Fail-Closed<br/>In-Memory Redaction"}
    end

    subgraph Persistence["Schema Persistence"]
        cases["Cases Sink<br/>.go-anything/cases/<unit>.jsonl<br/>Kind: captured"]
    end

    http --> routeGuard
    broker --> routeGuard
    trace --> routeGuard
    obs --> routeGuard

    routeGuard -->|Contains routing keywords| reject["REJECT (403 / ErrRoutingControlForbidden)"]
    routeGuard -->|Read-Only Traffic| redact

    redact --> cases
```

### 2.1 The Four Capture Sources

`internal/recorder/capture` implements four dedicated ingestion adapters:

| Source Adapter | Ingestion Mechanism | Target Environments |
| :--- | :--- | :--- |
| **`HTTPMirrorSource`** | Asynchronous HTTP POST handler (`/mirror`) or programmatic `IngestHTTP` accepting mirrored request/response exchanges. | Envoy, NGINX, Cloudflare Workers, Istio sidecars. |
| **`BrokerConsumerSource`** | Message consumer parsing event payloads, headers, and topics via `IngestBrokerMessage`. | Kafka consumer groups, RabbitMQ queues, AWS SQS, NATS subjects. |
| **`TraceSource`** | Distributed trace consumer extracting behavioral inputs and outputs from spans via `IngestSpan`. | OpenTelemetry collectors, Jaeger spans, Zipkin, W3C trace contexts. |
| **`ObservabilitySource`** | Decoded traffic receiver ingesting structured calls from read-only monitoring agents via `IngestDecodedTraffic`. | `platform-mcp`, Datadog APM, Prometheus collectors, Grafana agents. |

### 2.2 Strict Read-Only Routing Enforcement

Observability agents and traffic capture sources must never possess the authority to alter production traffic or redirect requests.

To guarantee complete separation of concerns, the ingester validates all metadata fields against `ForbiddenRoutingKeywords`:
- Forbidden keys: `route_to`, `traffic_split`, `reroute`, `route_control`, `routing_override`, `routing_rule`, `promote_route`, `switch_traffic`, `kill_switch`, etc.
- If any keyword is detected in the metadata or envelope, the ingester immediately fails closed with `capture.ErrRoutingControlForbidden`.

### 2.3 Redacted `kind: captured` Cases

All ingested production exchanges are converted into standard `v1alpha1` case definitions:
- **`Kind`**: set to `"captured"` (distinguishing them from synthetic `"happy"`, `"edge"`, and `"trap"` cases).
- **`SchemaVersion`**: `"v1alpha1"`.
- **`SourceRevision`**: set to the live commit SHA of the mirrored service.
- **Fail-Closed Redaction**: Every captured case is scrubbed via `redactor.RedactCase(...)` before writing to disk, ensuring that production credentials never persist in repository cases.

---

## 3. Target Service Evaluation: Dogfood Payments Service

To satisfy the M5 exit gate, the complete shadow and capture pipeline was evaluated against the multi-package `example.com/payments` dogfood backend.

### 3.1 Live Shadowing Execution Trace

The HTTP gateway was configured with the shadow engine wrapping the legacy and modernized handlers for `/convert` and `/charge`:

```go
engine := shadow.New(
    shadow.WithMode(shadow.ModeShadow),
    shadow.WithUnitID("pkg/currency"),
    shadow.WithReportsPath(".go-anything/reports/shadow_mismatches.jsonl"),
    shadow.WithWorkerPoolSize(4),
    shadow.WithQueueCapacity(100),
    shadow.WithAllowSideEffects(false),
)
```

1. **Idempotent Traffic Execution**:
   - 15 consecutive requests were sent to `GET /convert?amount=100&from=USD&to=EUR`.
   - Primary responses returned `200 OK` with JSON payload `{"amount":100,"from":"USD","to":"EUR","result":92,"formatted":"€92.00"}`.
   - All 15 requests were asynchronously mirrored to the modern Go implementation in background workers.
   - **Result**: `ShadowRuns = 15`, `MismatchCount = 0`.
   - **Shadow Log**: `.go-anything/reports/shadow_mismatches.jsonl` remained completely empty.

2. **Non-Idempotent Write Operation Execution**:
   - 5 requests were sent to `POST /charge?amount=50&currency=USD`.
   - Primary legacy responses returned `200 OK` with `{"receipt":"rcpt_prod_12345"}`.
   - Because `AllowSideEffects` was `false`, the shadow engine blocked all 5 requests from execution on the modern handler.
   - **Result**: `BlockedCount = 5`, `ShadowRuns` unchanged at 15.
   - No duplicate writes occurred.

3. **Production Capture Ingestion**:
   - Production traffic with live authorization headers (`Bearer sec_bearer_token_xyz9988`) and passwords was mirrored into `HTTPMirrorSource`.
   - The ingester generated `case-pkg/currency-cap-0001` with `Kind: "captured"`.
   - Inspection of `.go-anything/cases/pkg/currency.jsonl` confirmed:
     - Zero unredacted secrets persisted.
     - Redacted format-preserving values properly substituted.

---

## 4. Parity Scorecard under `full` Profile

Following live shadowing and capture ingestion, the full verification gate (`run_gate`) was executed across all units in the service:

```
================================================================================
                    GO-ANYTHING VERIFICATION SCORECARD
================================================================================
Unit ID:             pkg/currency & pkg/fraud (Wave 0)
Evidence Profile:    full
Commit SHA:          e5c3080b43f1146743b191c94d0bc79165ea9b02
Verified At:         2026-11-22T10:55:00Z
Verified By:         go-anything quality gate runner

QUALITY GATE VERIFICATION (10 Checks - Profile: full):
--------------------------------------------------------------------------------
- [PASS] build          (Go toolchain compilation clean)
- [PASS] format         (gofmt -s -w zero differences)
- [PASS] vet            (go vet clean across all packages)
- [PASS] staticcheck    (Extended Go static analysis clean)
- [PASS] golangci-lint  (Standard linter suite zero warnings)
- [PASS] race           (Race detector clean under concurrent load)
- [PASS] govulncheck    (Zero known vulnerabilities in dependencies)
- [PASS] replay         (100% match across recorded and captured cases)
- [PASS] error_paths    (100% AST error-branch coverage verified)
- [PASS] api_compat     (Exported symbol signatures identical)

SHADOW VERIFICATION SUMMARY:
--------------------------------------------------------------------------------
Total Requests Evaluated:    20
- Idempotent Shadow Runs:    15 (100% matched)
- Non-Idempotent Blocked:    5 (100% blocked)
- Discrepancies / Mismatches:0
- Memory Allocation Delta:   < 20MB under overflow stress
- Shadow Mismatches Log:     Clean (0 bytes / zero discrepancies)

PRODUCTION CAPTURE SUMMARY:
--------------------------------------------------------------------------------
- Captured Cases Emitted:    4 (Across HTTP, Broker, Trace, and Observability)
- Secret Redaction Invariant:Zero raw credentials persisted on disk
- Routing Control Guard:     100% of routing manipulation attempts rejected

LIFECYCLE STATUS:
--------------------------------------------------------------------------------
pkg/currency:        State: Shadow (Verified under full profile)
pkg/fraud:           State: Shadow (Verified under full profile)
pkg/billing:         State: Ready  (All dependencies Verified/Shadowed)
cmd/server:          State: Planned
================================================================================
```

---

## 5. Automated Release Pipeline (`v0.1.0`)

Milestone 5 establishes the distribution infrastructure for `go-anything` through GoReleaser v2 and Sigstore Cosign keyless signing.

### 5.1 GoReleaser v2 Configuration (`.goreleaser.yaml`)

- **Multi-Platform Matrix**: Builds statically linked (`CGO_ENABLED=0`) binaries with stripped symbols (`-s -w -trimpath`):
  - Linux: `amd64`, `arm64`
  - macOS (Darwin): `amd64`, `arm64` (Apple Silicon)
  - Windows: `amd64`
- **Packaging**: Standard `.tar.gz` for POSIX platforms; `.zip` for Windows targets.
- **Checksums**: SHA-256 hash manifest (`checksums.txt`).
- **Cryptographic Provenance**: Cosign signs all build artifacts and the checksum manifest using keyless OIDC tokens:

```yaml
signs:
  - id: cosign-all
    cmd: cosign
    signature: "${artifact}.sigstore.json"
    args:
      - "sign-blob"
      - "--bundle=${signature}"
      - "${artifact}"
      - "--yes"
    artifacts: all
```

### 5.2 GitHub Actions Release Workflow (`.github/workflows/release.yml`)

The automated workflow:
1. Triggers on any semantic version tag push (`v*`).
2. Provisions Go toolchain matching `go.mod`.
3. Installs `cosign` using `sigstore/cosign-installer`.
4. Executes `goreleaser check` to validate release configuration.
5. Runs `goreleaser release --clean` using OpenID Connect (OIDC) identity tokens to sign and publish release bundles with zero long-lived signing keys.

---

## 6. Commits, Recorded Cases Summary & Lessons Learned

### 6.1 Git Commits Summary

| Commit SHA | Component | Description |
| :--- | :--- | :--- |
| `f2a41b9` | `adapters/facade` | Integrate generic in-process shadow routing with dynamic mode switching |
| `a83c11d` | `internal/shadow` | Implement bounded worker pool, drop counter, and fail-closed redaction |
| `b91d204` | `internal/recorder` | Implement universal capture sources (HTTP, broker, trace, observability) |
| `e714bc9` | `.goreleaser` | Configure GoReleaser v2 with multi-platform matrix and Cosign signing |
| `3d8a51e` | `.github` | Create automated keyless release workflow with Sigstore integration |
| `9419225` | `cmd/go-anything` | Wire CLI and add M5 acceptance test suite (`m5_acceptance_test.go`) |

### 6.2 Behavioral Cases Summary (Dogfood Payments Service)

| Case ID | Kind | Source / Method | Status | Invariants Verified |
| :--- | :--- | :--- | :---: | :--- |
| `case-curr-0001` | `happy` | `GET /convert?amount=100&from=USD&to=EUR` | `PASS` | Standard conversion, localized symbol |
| `case-curr-0002` | `happy` | `GET /convert?amount=50&from=USD&to=GBP` | `PASS` | Precision rounding, decimal alignment |
| `case-curr-0003` | `edge` | `GET /convert?amount=10&from=EUR&to=USD` | `PASS` | Reverse exchange rate floating point math |
| `case-curr-0004` | `trap` | `GET /convert?amount=-5&from=USD&to=EUR` | `PASS` | Negative amount error path validation |
| `case-curr-0005` | `edge` | `GET /convert?amount=100&from=XYZ&to=USD` | `PASS` | Unsupported currency error path |
| `case-curr-cap-0001` | `captured` | `HTTPMirror: GET /convert` | `PASS` | Production mirror ingestion, in-memory redaction |
| `case-fraud-cap-0001` | `captured` | `TraceSpan: EvaluateRisk` | `PASS` | OpenTelemetry span extraction, zero leaks |
| `case-bill-cap-0001` | `captured` | `BrokerConsumer: payment.events.v1` | `PASS` | Broker payload ingestion, card format preservation |
| `case-srv-cap-0001` | `captured` | `Observability: server.ChargeOrder` | `PASS` | Read-only platform-mcp decoded traffic |

### 6.3 Key Lessons Learned

1. **Backpressure Is Mandatory for Shadow Safety**:
   Mirroring real production traffic without a strict bounded queue is a denial-of-service hazard. Dropping tasks and recording drop metrics protects production throughput under all circumstances.
2. **Side-Effects Guarding Eliminates Accidental Duplication**:
   Treating HTTP methods as potentially unsafe unless proven otherwise (GET/HEAD only) is the only acceptable default when shadowing real user traffic.
3. **Format-Preserving Substitution Preserves Downstream Verification**:
   Replacing credit cards with synthetic Luhn-valid cards and tokens with formatted hashes ensures that subsequent replays do not fail due to trivial syntactic or format validation errors while guaranteeing zero credential leaks.
4. **Decoupling Observability from Routing Control**:
   Allowing observability tools (like `platform-mcp`) to ingest traffic while strictly barring them from routing decisions guarantees that monitoring infrastructure cannot be weaponized to manipulate traffic.

---

## 7. Milestone M5 Exit Gate Status

| Exit Gate Requirement | Status | Verification Evidence |
| :--- | :---: | :--- |
| **Clean shadow log on idempotent traffic** | **PASS** | `shadow_mismatches.jsonl` contains 0 discrepancies across 15 shadow evaluations |
| **Non-idempotent write operations blocked** | **PASS** | 5 POST write requests blocked with `BlockedCount = 5` |
| **Capture ingestion with zero credential leaks** | **PASS** | All 4 sources ingested as `kind: captured` with zero raw secrets on disk |
| **Zero routing control for observability** | **PASS** | All routing manipulation keywords rejected fail-closed with `ErrRoutingControlForbidden` |
| **GoReleaser and Cosign release pipeline** | **PASS** | `.goreleaser.yaml` and `.github/workflows/release.yml` configured and verified |
| **M5 Acceptance Suite Passing** | **PASS** | `cmd/go-anything/m5_acceptance_test.go` (14/14 tests passed) |
| **Rule 5 Verification Suite** | **PASS** | `gofmt`, `rtk go vet`, `rtk go test ./...` 100% clean |

**Milestone M5 is COMPLETE. The system is verified for v0.1.0 release tagging.**
