# go-anything: Comprehensive User Guide

Welcome to the definitive user and operator guide for **go-anything**. This document provides end-to-end instructions for engineers and AI agents migrating legacy software to modern Go using incremental, verified behavioral strangler patterns.

---

## Table of Contents

1. [Introduction & Core Philosophy](#1-introduction--core-philosophy)
   - [What is go-anything?](#what-is-go-anything)
   - [The Strangler Fig Architecture](#the-strangler-fig-architecture)
   - [Why Not Pure AST Translation?](#why-not-pure-ast-translation)
   - [The Parity Principle & Disclaimer](#the-parity-principle--disclaimer)
2. [Installation & Workspace Setup](#2-installation--workspace-setup)
   - [System Prerequisites](#system-prerequisites)
   - [Building and Installing the CLI](#building-and-installing-the-cli)
   - [Verifying Your Environment (`doctor`)](#verifying-your-environment-doctor)
3. [Core Concepts & Mental Model](#3-core-concepts--mental-model)
   - [The 10-State Unit Lifecycle](#the-10-state-unit-lifecycle)
   - [Durable State vs. Local Leases](#durable-state-vs-local-leases)
   - [Dependency Waves & Topological Planning](#dependency-waves--topological-planning)
   - [Evidence Profiles (`replay` vs. `full`)](#evidence-profiles-replay-vs-full)
   - [The Human-Only Control Boundary](#the-human-only-control-boundary)
4. [CLI Command Reference](#4-cli-command-reference)
   - [`init` & `modernize`](#go-anything-init--modernize)
   - [`status`](#go-anything-status)
   - [`doctor`](#go-anything-doctor)
   - [`record`](#go-anything-record)
   - [`replay`](#go-anything-replay)
   - [`verify`](#go-anything-verify)
   - [`approve`](#go-anything-approve)
   - [`promote`](#go-anything-promote)
   - [`lift`](#go-anything-lift)
   - [`migrate`](#go-anything-migrate)
   - [`template` & `setup`](#go-anything-template--setup)
   - [`mcp`](#go-anything-mcp)
5. [End-to-End Migration Tutorials](#5-end-to-end-migration-tutorials)
   - [Tutorial A: Go-to-Go Modernization](#tutorial-a-go-to-go-modernization)
   - [Tutorial B: Python Service Migration](#tutorial-b-python-service-migration)
   - [Tutorial C: TypeScript / Node.js CLI Migration](#tutorial-c-typescript--nodejs-cli-migration)
6. [Working with Autonomous AI Agents & MCP](#6-working-with-autonomous-ai-agents--mcp)
   - [Configuring the MCP Server](#configuring-the-mcp-server)
   - [The 8 Standard MCP Tools](#the-8-standard-mcp-tools)
   - [Agent Operating Loop](#agent-operating-loop)
   - [Prompt Injection Defenses & Fail-Safe Controls](#prompt-injection-defenses--fail-safe-controls)
7. [Safety, Redaction & Sandboxing](#7-safety-redaction--sandboxing)
   - [Fail-Closed In-Memory Redaction](#fail-closed-in-memory-redaction)
   - [Dual-Run Volatility Detection & Normalization](#dual-run-volatility-detection--normalization)
   - [Execution Sandboxing & Isolation Tiers](#execution-sandboxing--isolation-tiers)
8. [Traffic Shadowing & Emergency Kill Switch](#8-traffic-shadowing--emergency-kill-switch)
   - [Running in Shadow Mode](#running-in-shadow-mode)
   - [Testing the Emergency Kill Switch](#testing-the-emergency-kill-switch)
   - [Final Strangler Lift](#final-strangler-lift)
9. [Key Go Semantic Traps & Edge Cases](#9-key-go-semantic-traps--edge-cases)
10. [Troubleshooting & FAQ](#10-troubleshooting--faq)

---

## 1. Introduction & Core Philosophy

### What is go-anything?

`go-anything` is an incremental migration framework and verification harness that safely rewrites software into idiomatic Go **one bounded unit at a time**.

Instead of risking an all-or-nothing "big-bang" rewrite that takes months and often fails in production, `go-anything` applies the **Strangler Fig pattern**. It decomposes an existing application into topological dependency waves, captures behavioral test vectors from the original system in an isolated sandbox, and systematically proves that the ported Go implementation matches recorded behavior before production traffic is switched over.

### The Strangler Fig Architecture

In a `go-anything` migration, the original application and the modern Go implementation coexist behind an intelligent front door:

```mermaid
flowchart TD
    Client["Client / User Traffic"] --> FD["Go Front Door Router / Proxy"]
    FD -->|Unit in Cutover State| GoUnit["Modern Go Unit"]
    FD -->|Unit Unmigrated or Blocked| OriginalApp["Legacy Application"]
    FD -.->|Shadow Mode: Mirror Traffic| ShadowPool["Async Shadow Comparator"]
    ShadowPool -.-> OriginalApp
    ShadowPool -.-> GoUnit
    GoUnit --> KS{"Emergency Kill Switch?<br/>(ENV / Signal)"}
    KS -->|Active (KILL_SWITCH=1)| OriginalApp
    KS -->|Normal| Output["Verified Response"]
```

1. **Incremental Replacement:** You port low-dependency leaf units first, followed by upstream callers.
2. **Instant Reversibility:** If any regression or unexpected divergence occurs under real-world conditions, an emergency kill-switch instantly reverts routing back to the original application without requiring code deployments.
3. **Safe Retirement (`lift`):** Once fallback traffic reaches zero and the entire service is migrated, `go-anything lift` removes the legacy shim and produces a clean, standalone Go binary.

### Why Not Pure AST Translation?

Automated source-to-source compilers (transpilers) produce unmaintainable, unidiomatic Go (frequently littered with pointer gymnastics, artificial classes, and alien design patterns).

`go-anything` decouples **code generation** from **orchestration and verification**:
- **You or your AI coding agent** write natural, idiomatic Go leveraging Go standard libraries and best practices.
- **`go-anything`** acts as the objective arbiter: analyzing call graphs, capturing behavioral evidence, replaying traffic, enforcing strict quality gates, and persisting state outside transient chat contexts.

### The Parity Principle & Disclaimer

> [!IMPORTANT]
> **Parity is claimed ONLY for recorded, observed, and fuzzed inputs.**
> Passing behavioral checks proves equivalence under the recorded test vectors and distributions. It is **not** a mathematical guarantee of semantic equivalence across untested execution paths. Teams must review test coverage and behavioral diffs before full traffic cutover.

---

## 2. Installation & Workspace Setup

### System Prerequisites

- **Go:** Version 1.26 or later installed (`go version`).
- **Git:** Version 2.30+ installed.
- **Operating System:**
  - **Linux (x86_64 / arm64):** Full Tier 1 sandboxing (unprivileged user namespaces, network isolation, bind mounts).
  - **macOS / Windows:** Tier 2 sandboxing (process isolation, temporary directories, environmental scrubbing).
- **Target Runtime (if migrating from external languages):**
  - Python 3.10+ (for Python migrations).
  - Node.js 18+ (for TypeScript/JavaScript migrations).

### Building and Installing the CLI

Clone the repository and install the binary:

```bash
# Clone the repository
git clone https://github.com/mohammedrefat/go-anything.git
cd go-anything

# Install into your $GOPATH/bin (ensure $GOPATH/bin is in your PATH)
go install ./cmd/go-anything

# Verify the CLI installation
go-anything version
# Output: go-anything v1.0.0 (profile: full, jobs: 8)
```

Alternatively, build a standalone binary locally:

```bash
go build -o go-anything ./cmd/go-anything
```

### Verifying Your Environment (`doctor`)

To check the operational readiness of your current machine and workspace:

```bash
go-anything doctor
```

`doctor` executes automated pre-flight checks:
- Verifies `.go-anything/` directory integrity.
- Confirms `.go-anything/local/` is ignored by `.gitignore` (protecting temporary agent leases from accidental commit).
- Scans `.go-anything/cases/` to ensure no raw secrets leaked past the in-memory redactor.
- Evaluates sandbox availability and OS isolation capabilities.

---

## 3. Core Concepts & Mental Model

### The 10-State Unit Lifecycle

Every migration unit tracks through a deterministic state machine managed server-side:

```
Planned ──► Recorded ──► Ready ──► In Progress ──► Verified ──► Shadow ──► Approved ──► Cutover ──► Completed
                                         │
                                         └──► Blocked (Human intervention required)
```

| Lifecycle State | Description & Requirements |
| :--- | :--- |
| **`Planned`** | Unit identified in the dependency graph, assigned to a wave, risks scored. |
| **`Recorded`** | Behavioral test cases recorded from original application with secrets redacted. |
| **`Ready`** | All upstream dependencies are either `Verified` or fronted by the legacy system. |
| **`In Progress`** | Actively claimed by a developer or AI agent holding a time-bounded lease. |
| **`Verified`** | Ported Go code passes compilation, test suite, and 100% case replay with evidence tied to a git commit. |
| **`Shadow`** | Ported unit running asynchronously alongside original on live traffic (read-only/idempotent). |
| **`Approved`** | Human engineer has reviewed scorecard and diffs, signing off via CLI. |
| **`Cutover`** | Production traffic routed to modern Go unit; fallback path active and monitored. |
| **`Completed`** | Legacy bridge lifted, fallback traffic confirmed zero, final clean build validated. |
| **`Blocked`** | Migration paused due to an architectural question, unresolvable mismatch, or dependency barrier. |

### Durable State vs. Local Leases

`go-anything` cleanly partitions repository-tracked assets from ephemeral local state:

- **Tracked in Git (`.go-anything/`):**
  - `manifest.json`: List of all units, source language, waves, and schema version.
  - `units/<unit-id>.json`: Durable state and history for each unit.
  - `cases/<unit-id>.jsonl`: Redacted, sanitized behavioral test fixtures.
  - `rules.json`: Auto-detected volatility rules (UUID, timestamp, random string normalizers).
  - `evidence/`: Cryptographically signed or commit-tied verification reports and scorecards.
- **Gitignored (`.go-anything/local/`):**
  - Ephemeral locks (`.lock`) preventing concurrent file mutations.
  - Active agent leases (`leases/<unit-id>.json`) granting temporary ownership to an actor.

### Dependency Waves & Topological Planning

Migration occurs in **topological waves** calculated from the call graph:
- **Wave 0 (Leaves):** Utility libraries, data structures, pure domain logic, health check endpoints.
- **Wave 1 (Intermediate):** Authentication handlers, database access layers, transformation pipelines.
- **Wave 2+ (Entrypoints):** Top-level HTTP routes, CLI command parsers, gRPC services.

Units within the same wave can be ported simultaneously in parallel lanes.

### Evidence Profiles (`replay` vs. `full`)

`go-anything` supports two verification profiles:
- **`replay` Profile:** Requires 100% pass rate on all recorded and fuzzed behavioral fixtures.
- **`full` Profile (Default):** Enforces behavioral replay **plus** the complete quality gate:
  1. `go build` (clean compilation without warnings)
  2. `go vet` (static code analysis)
  3. `go test -race` (unit tests under the Go race detector)
  4. `govulncheck` (known CVE vulnerability scanning)
  5. `error_paths` (malformed input and non-zero exit code parity)
  6. `api_compat` (exported symbol, type, and contract compliance)

### The Human-Only Control Boundary

To ensure safe operation when collaborating with autonomous AI agents:
- **AI Agents can execute:** `claim_unit`, `inspect_unit`, `record_cases`, `verify_diff`, `run_gate`, `mark_verified`, `handoff`.
- **Only Humans can execute:**
  - `go-anything approve`
  - `go-anything promote`
  - `go-anything lift`
  - Routing mutations and kill-switch overrides

This boundary is hard-coded into the MCP server architecture.

---

## 4. CLI Command Reference

### `go-anything init` & `modernize`

Initializes a migration workspace inside the target project directory.

```bash
# Syntax:
go-anything init [--source <lang>] [--name <project-name>] [directory]
go-anything modernize [directory]  # Shorthand for --source=go
```

**Flags:**
- `--source, -s <string>`: Source language (`go`, `python`, `typescript`). Default: `go`.
- `--name, -n <string>`: Name of the project (defaults to directory basename).
- `--profile, -p <string>`: Evidence profile (`full` or `replay`). Default: `full`.

**Example:**
```bash
go-anything init --source=python --name=payment-service ./services/payment
```

For Go-to-Go modernizations, `init` automatically establishes the two-tier layout:
- `legacy/`: Preserved original implementation.
- `modern/`: Target workspace for modernized Go code.

---

### `go-anything status`

Displays the current migration progress, dependency waves, unit states, active leases, and potential blockers.

```bash
go-anything status [--dir <path>]
```

**Example Output:**
```text
Workspace: payment-service (schemas: v1)
Overall Progress: 3/8 units completed (37%)

Wave 0:
  [Completed] unit-crypto        (dependencies: none, risk: low)
  [Verified]  unit-currency-conv (dependencies: none, risk: low)
  [Ready]     unit-card-token    (dependencies: none, risk: medium)

Wave 1:
  [Planned]   unit-charge-flow   (dependencies: [unit-card-token], risk: high)
```

---

### `go-anything doctor`

Runs diagnostic inspections against the workspace to guarantee safety, rule hygiene, and lease sanity.

```bash
go-anything doctor [--dir <path>]
```

**Checks Performed:**
1. Verifies schema compliance of `manifest.json`.
2. Validates that `.go-anything/local/` is properly ignored in `.gitignore`.
3. Scans all `.jsonl` fixtures for leaked secrets (e.g., unmasked JWTs, AWS credentials, private keys).
4. Tests OS sandbox isolation backend readiness.

---

### `go-anything record`

Executes the original application in a sandboxed environment, detects volatile fields via dual-run execution, applies fail-closed redaction, and saves sanitized JSONL fixtures.

```bash
go-anything record --unit <unit-id> [--cmd <command>] [--url <http-url>] [--curl <curl-cmd>] [--kind <category>]
```

**Flags:**
- `--unit, -u <string>`: Target unit identifier (*required*).
- `--cmd, -c <string>`: CLI command to execute and record.
- `--url <string>`: Target HTTP endpoint to query and record.
- `--curl <string>`: Full curl command string to ingest.
- `--kind, -k <string>`: Case kind: `happy`, `edge`, `trap`, `fuzz`, or `captured` (Default: `happy`).
- `--dir <string>`: Path to workspace root (Default: `.`).

**Example:**
```bash
# Record CLI behavior
go-anything record --unit unit-calc --cmd "./legacy-calc --sum 10 20" --kind happy

# Record HTTP behavior
go-anything record --unit unit-user-api --url "http://localhost:8080/api/v1/users/42" --kind happy
```

---

### `go-anything replay`

Performs concurrent, deterministic replay of all recorded test cases against the ported Go binary or HTTP service.

```bash
go-anything replay --unit <unit-id> --target <binary-or-url> [--jobs <N>] [--mode <comparator-mode>] [--case <case-id>]
```

**Flags:**
- `--unit, -u <string>`: Unit ID to replay (*required*).
- `--target, -t <string>`: Path to ported Go executable or base URL of running Go service (*required*).
- `--jobs, -j <int>`: Number of parallel worker goroutines (Default: `runtime.NumCPU()`).
- `--mode, -m <string>`: Comparator mode: `exact`, `structural_json`, `numeric_tolerance`, `http_contract`, `regex`.
- `--case <string>`: Replay and debug a single case ID.

**Example:**
```bash
go-anything replay --unit unit-calc --target ./bin/modern-calc --jobs 4
```

---

### `go-anything verify`

Executes complete behavioral replay, verifies quality gates, stores commit-tied evidence in `.go-anything/evidence/`, and advances the unit state to `Verified`.

```bash
go-anything verify <unit-id> --target <binary-or-url> [--profile <replay|full>] [--actor <name>]
```

**Flags:**
- `--target, -t <string>`: Ported Go executable or HTTP service URL (*required*).
- `--profile, -p <string>`: Evidence profile (`replay` or `full`). Default: `full`.
- `--actor <string>`: Identity of developer or agent performing verification (Default: `agent`).

**Example:**
```bash
go-anything verify unit-calc --target ./bin/modern-calc --profile full
```

---

### `go-anything approve`

Records human engineering sign-off. This command transitions a unit from `Verified` (or `Shadow`) to `Approved`.

```bash
go-anything approve <unit-id> [--skip-shadow] [--actor <engineer-name>]
```

> [!NOTE]
> This command is strictly CLI-only and cannot be invoked by AI agents via MCP.

---

### `go-anything promote`

Promotes a unit to `Cutover` status by updating front door routing rules, testing the emergency kill-switch, and routing live traffic to the modern Go implementation.

```bash
go-anything promote <unit-id> [--actor <engineer-name>]
```

---

### `go-anything lift`

The final step of the strangler migration: safely eliminates fallback wrappers, removes legacy bridges, confirms that zero traffic falls back to the original, and validates a clean standalone build.

```bash
go-anything lift [<unit-id>] [--skip-build] [--profile <replay|full>]
```

If `<unit-id>` is omitted, `lift` retires all units currently in `Cutover` state.

---

### `go-anything migrate`

Upgrades `.go-anything/` workspace metadata from legacy alpha schemas (`v1alpha1`) to stable `v1`.

```bash
go-anything migrate [--dir <path>]
```

---

### `go-anything template` & `setup`

Renders optimized AI coding agent instruction files tailored specifically to the project and `go-anything` workflow.

```bash
go-anything template [--agent <claude|cursor|codex|gemini|all>] [--dir <path>] [--skills]
```

**Supported Targets:**
- `claude`: Generates `CLAUDE.md`.
- `cursor`: Generates `.cursorrules`.
- `codex`: Generates `AGENTS.md`.
- `gemini`: Generates `GEMINI.md`.
- `--skills`: Writes `.go-anything/skills.md` containing operational guidance for AI assistants.

---

### `go-anything mcp`

Starts the built-in Model Context Protocol (MCP) server over `stdio`, allowing coding agents (Claude Desktop, Cursor, Gemini CLI, etc.) to securely drive the migration.

```bash
go-anything mcp
```

---

## 5. End-to-End Migration Tutorials

### Tutorial A: Go-to-Go Modernization

Modernizing an older Go codebase (e.g., upgrading from legacy Go 1.14 package layouts or global-variable monoliths to modular Go 1.26+ packages).

#### Step 1: Initialize Modernization
```bash
cd /workspace/legacy-go-service
go-anything modernize .
```
`go-anything` creates `legacy/` (holding the unmodified code) and sets up `modern/` for your new implementation.

#### Step 2: Check Waves & Status
```bash
go-anything status
```
Identify Wave 0 units (e.g., `config-parser`, `token-validator`).

#### Step 3: Record Baseline Behavior
```bash
go-anything record --unit token-validator --cmd "./legacy/bin/service --validate-token eyJhbGciOi..."
```

#### Step 4: Implement Modern Go Code
Write idiomatic, decoupled code in `modern/token/token.go`:
```go
package token

import "errors"

func Validate(tok string) (bool, error) {
    if len(tok) == 0 {
        return false, errors.New("empty token")
    }
    // Modernized validation logic...
    return true, nil
}
```

Build the modern binary:
```bash
go build -o ./bin/modern-token ./modern/token/cmd/main.go
```

#### Step 5: Replay & Verify
```bash
go-anything replay --unit token-validator --target ./bin/modern-token
go-anything verify token-validator --target ./bin/modern-token --profile full
```

#### Step 6: Human Approval & Cutover
```bash
go-anything approve token-validator --skip-shadow --notes "Unit tests and replay pass 100%"
go-anything promote token-validator
```

---

### Tutorial B: Python Service Migration

Migrating a Python microservice (e.g., Flask or FastAPI) to high-performance Go.

#### Step 1: Initialize Python Workspace
```bash
go-anything init --source=python --name=order-service /path/to/python-app
cd /path/to/python-app
```

#### Step 2: Record HTTP Traffic from Python Service
Ensure the Python service is running locally on port `5000`:
```bash
# Record standard order creation
go-anything record \
  --unit unit-create-order \
  --url "http://localhost:5000/orders" \
  --kind happy

# Record invalid payload (trap/edge case)
go-anything record \
  --unit unit-create-order \
  --curl "curl -X POST http://localhost:5000/orders -H 'Content-Type: application/json' -d '{\"items\": []}'" \
  --kind trap
```

#### Step 3: Implement in Modern Go
Create `main.go` using standard library `net/http`:
```go
package main

import (
    "encoding/json"
    "net/http"
)

type OrderRequest struct {
    Items []string `json:"items"`
}

func orderHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        w.WriteHeader(http.StatusMethodNotAllowed)
        return
    }
    var req OrderRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Items) == 0 {
        w.WriteHeader(http.StatusBadRequest)
        json.NewEncoder(w).Encode(map[string]string{"error": "invalid items"})
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(map[string]string{"status": "created"})
}

func main() {
    http.HandleFunc("/orders", orderHandler)
    http.ListenAndServe(":8080", nil)
}
```

#### Step 4: Verify Against Python Baseline
Start the Go service on port `8080` and run verification:
```bash
go-anything verify unit-create-order --target "http://localhost:8080" --profile replay
```

---

### Tutorial C: TypeScript / Node.js CLI Migration

Migrating a Node.js CLI tool to a single compiled Go binary.

#### Step 1: Initialize Workspace
```bash
go-anything init --source=typescript --name=csv-tool /path/to/node-cli
```

#### Step 2: Record Existing CLI Behavior
```bash
go-anything record --unit unit-transform --cmd "node dist/cli.js transform --input data.csv --delimiter comma"
```

#### Step 3: Implement Go Replacement
Develop the Go CLI using standard flags or subcommands, compile to `./bin/csv-tool`, and replay:
```bash
go-anything replay --unit unit-transform --target ./bin/csv-tool
go-anything verify unit-transform --target ./bin/csv-tool --profile full
```

---

## 6. Working with Autonomous AI Agents & MCP

### Configuring the MCP Server

`go-anything` exposes a standard [Model Context Protocol](https://modelcontextprotocol.io/) server over `stdio`. It can be added to any MCP-compliant client.

**Example: `claude_desktop_config.json` / Cursor MCP Settings:**
```json
{
  "mcpServers": {
    "go-anything": {
      "command": "go-anything",
      "args": ["mcp"],
      "env": {
        "PATH": "/usr/local/go/bin:/usr/bin:/bin"
      }
    }
  }
}
```

### The 8 Standard MCP Tools

The `go-anything mcp` server provides 8 tightly scoped tools:

| MCP Tool | Description | Inputs |
| :--- | :--- | :--- |
| `status` | Query migration state, dependency waves, and blockers. | `dir` (optional) |
| `claim_unit` | Acquire a time-bounded lease on a `Ready` unit. | `unit_id`, `actor`, `ttl_seconds` |
| `inspect_unit` | Read original code, sample cases, and semantic traps. | `unit_id` |
| `record_cases` | Run original code in the sandbox to generate cases. | `unit_id`, `command`, `url`, `kind` |
| `verify_diff` | Build Go target, replay cases, return diffs on mismatch. | `unit_id`, `target`, `mode` |
| `run_gate` | Run the 6-stage Quality Gate and create evidence scorecard. | `unit_id`, `target`, `profile` |
| `mark_verified` | Transition unit to `Verified` (tied to current Git commit). | `unit_id`, `commit_sha` |
| `handoff` | Write `HANDOFF.md` documenting session accomplishments. | `unit_id`, `summary`, `next_steps` |

### Agent Operating Loop

When an AI agent engages with `go-anything`, it follows this mandatory loop:

```mermaid
flowchart TD
    A["1. status"] --> B["2. claim_unit"]
    B --> C["3. inspect_unit"]
    C --> D["4. Write Go Code"]
    D --> E["5. verify_diff"]
    E -->|Diffs / Mismatch| D
    E -->|PASS (0 Diffs)| F["6. run_gate"]
    F -->|Gate Failure| D
    F -->|All Checks Pass| G["7. mark_verified"]
    G --> H["8. handoff"]
```

### Prompt Injection Defenses & Fail-Safe Controls

1. **Output Sanitization:** Strings returned through MCP are truncated and stripped of dangerous terminal control characters.
2. **Data Boundary Delimiters:** Code inspected by agents is wrapped in unambiguous XML boundaries (`<observed_source>`, `<untrusted_input>`).
3. **Hard Ceiling on Agent Actions:** AI agents can never mark a unit `Approved`, `Cutover`, or `Completed`. A human operator must execute those commands on the CLI.

---

## 7. Safety, Redaction & Sandboxing

### Fail-Closed In-Memory Redaction

`go-anything` enforces strict zero-leakage credential redaction:
- **Format-Preserving Substitution:** Credit card numbers, JWTs, UUIDs, and API tokens are replaced with synthetic, validly formatted test tokens **before** any case data touches disk.
- **Fail-Closed Guarantee:** If a payload contains an ambiguous secret token that cannot be safely redacted, recording is aborted immediately.

```json
// Example: Original captured header
"Authorization": "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.s3cr3t..."

// Redacted JSONL case stored on disk
"Authorization": "Bearer [REDACTED_JWT_TOKEN_0001]"
```

### Dual-Run Volatility Detection & Normalization

To eliminate false positive diffs caused by non-deterministic fields:
1. `record` runs the legacy application **twice** with identical inputs.
2. Any field that changes between runs (timestamps, ephemeral PIDs, random request IDs) is automatically marked `volatile`.
3. Auto-detected regex rules are stored in `.go-anything/rules.json` and masked during differential replay.

### Execution Sandboxing & Isolation Tiers

- **Tier 1 (Linux):** Namespaces (`CLONE_NEWNET`, `CLONE_NEWNS`, `CLONE_NEWPID`), `chroot` or pivot root, read-only file mounts, process memory caps. Network disabled by default.
- **Tier 2 (macOS / Windows):** Ephemeral isolated temporary workspaces, stripped environment variables (`env -i` allowlist), non-root execution, process tree timeouts.

---

## 8. Traffic Shadowing & Emergency Kill Switch

### Running in Shadow Mode

Before cutting over production traffic, `go-anything` can mirror live requests asynchronously:

```bash
# Start front door in shadow mode
go-anything shadow --unit unit-order-api --listen :80 --legacy http://localhost:5000 --modern http://localhost:8080
```

- **Idempotency Safeguard:** Shadow mode defaults to `GET` and `HEAD` requests only to prevent duplicate mutations.
- **Bounded Worker Pool:** Shadow replay occurs in a bounded worker pool. If the Go port is slow or stalls, shadow requests are dropped without impacting live client latency.

### Testing the Emergency Kill Switch

Before marking a unit `Cutover`, the promotion sequence validates the emergency kill switch:

```bash
# Emergency rollback via environment variable
export GO_ANYTHING_UNIT_unit_order_api=original

# Or global kill switch
export KILL_SWITCH=1
```

When active, 100% of traffic immediately bypasses the modern Go code and falls back to the legacy system.

### Final Strangler Lift

When all units are verified and production fallback metrics remain at zero:

```bash
go-anything lift
```

`go-anything lift`:
1. Strips all front-door proxies and fallback wrappers.
2. Promotes `modern/` to project root.
3. Archives `.go-anything/` migration metadata.
4. Executes a full clean build and test verification.

---

## 9. Key Go Semantic Traps & Edge Cases

When rewriting code into Go, watch out for these subtle language-level semantic differences:

| Category | The Trap | Mitigation / Best Practice |
| :--- | :--- | :--- |
| **Empty Slices** | `nil` slice serializes to `null` in JSON; empty initialized slice serializes to `[]`. | Explicitly initialize: `items := make([]string, 0)` when JSON array output is required. |
| **HTML Escaping** | Go's standard `encoding/json` escapes `<`, `>`, and `&` to unicode entities (`\u0026`). | Use `json.NewEncoder` with `SetEscapeHTML(false)` if raw characters are expected. |
| **Map Key Order** | Go deliberately randomizes map iteration order; original languages may preserve insertion order. | Sort keys deterministically before serialization if consumers rely on stable ordering. |
| **Numeric Types** | JSON unmarshaling to `interface{}` defaults numbers to `float64`, losing `int64` precision. | Use `json.NewDecoder(r).UseNumber()` for high-precision integer payloads. |
| **String Slicing** | String indexing in Go operates on **bytes**, not Unicode code points (runes). | Convert to `[]rune(s)` before slicing multi-byte UTF-8 strings. |
| **Error Handling** | Go functions return errors explicitly; unhandled errors cause panics or silent failures. | Check `if err != nil` on every call; verify non-zero exit codes against recorded edge cases. |

---

## 10. Troubleshooting & FAQ

### Q: What if `go-anything record` reports a secret detected in output?
**A:** The in-memory redactor will fail-closed to protect your repository. Add custom patterns or field designations to `.go-anything/rules.json` or update your redaction configuration before re-running the record command.

### Q: Why does `verify` fail with "mismatch" when JSON output looks identical?
**A:** Check for whitespace differences, map key order, or numeric formatting. Ensure `--mode structural_json` is specified so that semantic equivalence is compared rather than raw byte strings.

### Q: Can an AI agent run `promote` or `approve`?
**A:** No. `approve`, `promote`, and `lift` are strictly human-in-the-loop CLI commands and are intentionally omitted from the MCP tool registry.

### Q: How do I run verification on Windows without Linux namespaces?
**A:** `go-anything` automatically detects Windows and activates **Tier 2 Best-Effort Isolation**. For strict namespace and networking isolation on Windows, run `go-anything` inside WSL2 (Windows Subsystem for Linux).

---

*For further architectural details, refer to the [Architecture & Implementation Plan](go-anything%20Architecture%20%26%20Implementation%20Plan%20(v3)%20(1).md) and [SECURITY.md](../SECURITY.md).*
