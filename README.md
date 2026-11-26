<div align="center">

  <img src="assets/go-anything.svg" alt="A Gopher transforms multi-language code into clean, modular Go blocks" width="480" />

  <h1>go-anything</h1>
  <p><strong>Move from any language to modern Go—one verified unit at a time.</strong></p>
  <p>Replace high-risk big-bang rewrites with an incremental strangler migration,<br />backed by recorded behavioral evidence, automated quality gates, and human-in-the-loop control.</p>

  <p>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go" alt="Go 1.27+" /></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache--2.0-6C7CFF.svg" alt="Apache 2.0" /></a>
    <a href="https://github.com/mohammedrefaat/go-anything/releases"><img src="https://img.shields.io/badge/release-v2.0.1-34D399.svg" alt="Release v2.0.1" /></a>
  </p>

  <p>
    <a href="#installation--verification">Installation</a> ·
    <a href="#5-minute-vertical-slice-quickstart">Quickstart</a> ·
    <a href="Docs/USER_GUIDE.md">User Guide</a> ·
    <a href="STATUS.md">Command Matrix (32/32)</a> ·
    <a href="#hot-path-sidecars--differential-testing">Hot-Path &amp; Fuzz/Bench</a> ·
    <a href="#milestone-roadmap-m0m7--v20">Roadmap</a> ·
    <a href="CHANGELOG.md">Changelog</a> ·
    <a href="SECURITY.md">Security</a> ·
    <a href="Docs/go-anything%20Architecture%20%26%20Implementation%20Plan%20(v3)%20(1).md">Architecture</a>
  </p>

  <p>
    <strong>Languages:</strong><br />
    <a href="README.md">English</a> ·
    <a href="README.ar.md">العربية</a>
  </p>

</div>

---

## On this page

- [How it works](#how-it-works)
- [Why go-anything?](#why-go-anything)
- [Explicit Parity Disclaimer](#explicit-parity-disclaimer)
- [Installation & Verification](#installation--verification)
- [5-Minute Vertical-Slice Quickstart](#5-minute-vertical-slice-quickstart)
- [Hot-Path Sidecars & Differential Testing](#hot-path-sidecars--differential-testing)
- [Milestone Roadmap (M0–M7 & v2.0)](#milestone-roadmap-m0m7--v20)
- [Known Limitations](#known-limitations)
- [Autonomous Agent & MCP Workflow](#autonomous-agent--mcp-workflow)
- [Architecture & Design Principles](#architecture--design-principles)
- [Repository Map](#repository-map)
- [Governance & Contributing](#governance--contributing)
- [License](#license)

---

## How it works

`go-anything` migrates applications from **Go, Python, TypeScript, or JavaScript** to modern Go **one unit or hot path at a time** and proves each unit matches the original's recorded behavior before it ships.

- **Not an AST code translator:** You or your AI coding agent write idiomatic Go. `go-anything` provides the orchestrator, sandbox, behavioral capture, differential replay/fuzzing, sidecar bridges, and quality gates.
- **The Strangler Pattern:** The original application remains live and functional. Bounded units (or individual hot functions via stdio JSON-RPC sidecars) are replaced incrementally behind a reversible router or bridge.
- **Fail-Closed Verification:** Parity is verified by replaying recorded and fuzzed traffic through both implementations and comparing outputs and side-effects under deterministic rules.

```mermaid
flowchart LR
    A["1. Dependency Graph<br/>& Wave Planning"] --> B["2. Sandboxed Case<br/>Recording & Redaction"]
    B --> C["3. Implement Unit<br/>in Modern Go"]
    C --> D["4. Concurrent Replay<br/>& Quality Gate"]
    D -->|Mismatches / Failures| C
    D -->|Verified with Evidence| E["5. Shadow Mode<br/>(Live Traffic)"]
    E --> F["6. Human CLI Sign-off<br/>& Cutover"]
    F --> G["7. Strangler Lift<br/>& Retirement"]
```

---

## Why go-anything?

| Risky Big-Bang Rewrite | Verified Incremental Migration (`go-anything`) |
| :--- | :--- |
| Replace the entire codebase all at once | Move one isolated dependency wave or hot function at a time |
| Discover edge cases and regressions in production | Compare against recorded fixtures and fuzz inputs before traffic moves |
| Progress trapped inside ephemeral chat sessions | Durable state persisted in reviewable JSON/JSONL files |
| Blanket assumptions of parity ("tests passed") | Explicit parity boundaries tied to recorded test vectors & commit SHAs |
| Autonomous agents given unconstrained deployment access | Human-only controls for `approve`, `promote`, `lift`, and routing |

---

## Explicit Parity Disclaimer

> [!IMPORTANT]
> **Parity has limits.**
>
> Parity is claimed **only for recorded, observed, and fuzzed inputs**, never for unobserved behavior. Passing behavioral checks proves equivalence under the recorded test vectors and distributions—it is **not** a mathematical guarantee of semantic equivalence across arbitrary, untested execution paths.
>
> `go-anything` enforces dual-run recording to identify volatile fields (timestamps, random tokens, UUIDs) and normalizes them with auto-detected rules, but migration teams must review behavioral diffs and audit coverage before traffic cutover.

---

## Installation & Verification

`go-anything` is distributed as signed, zero-dependency standalone binaries, through major package managers, as well as via the Go toolchain. Full platform matrix, checksums, and keyless Sigstore Cosign verification instructions are documented in [Docs/install.md](Docs/install.md) and [Docs/packaging.md](Docs/packaging.md).

### Package Managers

Install `go-anything` using your preferred system package manager:

#### Homebrew (macOS & Linux)
```bash
brew tap mohammedrefaat/tap
brew install go-anything
```

#### Scoop (Windows)
```powershell
scoop bucket add go-anything https://github.com/mohammedrefaat/scoop-bucket.git
scoop install go-anything
```

#### npm (Node.js)
```bash
npm install -g @mohammedrefaat/go-anything
```

### Prebuilt Binaries

Download prebuilt binaries for your platform from [GitHub Releases](https://github.com/mohammedrefaat/go-anything/releases):

| Platform | Architecture | Binary / Archive |
| :--- | :--- | :--- |
| **Linux** | `amd64`, `arm64` | `go-anything_<version>_linux_<arch>.tar.gz` |
| **macOS** | `arm64` (Apple Silicon), `amd64` (Intel) | `go-anything_<version>_darwin_<arch>.tar.gz` |
| **Windows** | `amd64` | `go-anything_<version>_windows_amd64.zip` |

#### Linux & macOS (Bash / Zsh)

```bash
VERSION="2.0.1"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
RELEASE_URL="https://github.com/mohammedrefaat/go-anything/releases/download/v${VERSION}"
ARTIFACT="go-anything_${VERSION}_${OS}_${ARCH}.tar.gz"

# Download archive, checksums, and signature
curl -fsSL -O "${RELEASE_URL}/${ARTIFACT}"
curl -fsSL -O "${RELEASE_URL}/checksums.txt"
curl -fsSL -O "${RELEASE_URL}/checksums.txt.sigstore.json"

# Verify cryptographic signature via Cosign (Keyless Sigstore)
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "^https://github\.com/mohammedrefaat/go-anything/\.github/workflows/release\.yml@refs/tags/v.*$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt

# Verify SHA-256 checksum
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum --ignore-missing -c checksums.txt
else
  shasum -a 256 --ignore-missing -c checksums.txt
fi

# Extract and install
tar -xzf "${ARTIFACT}" go-anything
sudo install -m 0755 go-anything /usr/local/bin/go-anything
```

#### Windows (PowerShell)

```powershell
$Version = "2.0.1"
$ReleaseUrl = "https://github.com/mohammedrefaat/go-anything/releases/download/v$Version"
$Artifact = "go-anything_${Version}_windows_amd64.zip"

Invoke-WebRequest -Uri "$ReleaseUrl/$Artifact" -OutFile $Artifact
Invoke-WebRequest -Uri "$ReleaseUrl/checksums.txt" -OutFile "checksums.txt"
Invoke-WebRequest -Uri "$ReleaseUrl/checksums.txt.sigstore.json" -OutFile "checksums.txt.sigstore.json"

# Verify signature with Cosign (if installed)
if (Get-Command cosign -ErrorAction SilentlyContinue) {
    cosign verify-blob `
      --bundle checksums.txt.sigstore.json `
      --certificate-identity-regexp "^https://github\.com/mohammedrefaat/go-anything/\.github/workflows/release\.yml@refs/tags/v.*$" `
      --certificate-oidc-issuer "https://token.actions.githubusercontent.com" `
      checksums.txt
}

# Verify SHA-256 hash
$expected = (Get-Content checksums.txt | Select-String $Artifact).Line.Split(" ")[0].ToLower()
$actual = (Get-FileHash -Algorithm SHA256 $Artifact).Hash.ToLower()
if ($expected -ne $actual) { throw "Checksum mismatch!" }

# Extract and install
Expand-Archive -Path $Artifact -DestinationPath .\go-anything-bin -Force
$targetDir = "$HOME\bin"
if (!(Test-Path $targetDir)) { New-Item -ItemType Directory -Path $targetDir }
Move-Item .\go-anything-bin\go-anything.exe "$targetDir\go-anything.exe" -Force
```

### Install via Go Toolchain (`go install`)

If Go 1.27+ is installed:

```bash
go install github.com/mohammedrefaat/go-anything/cmd/go-anything@latest
```

### Verification & Health Check

Verify the CLI binary and check system readiness:

```bash
go-anything version
# Output: go-anything v2.0.1 (profile: full, jobs: 8)

go-anything doctor
# Output: [PASS] workspace clean, tools available, no stale leases
```

---

## 5-Minute Vertical-Slice Quickstart

Follow this self-contained walkthrough from a clean machine to initializing a project, recording behavior, implementing a Go unit, passing the quality gate, and replaying cases **without human assistance**.

### Prerequisites

- [Go 1.27+](https://go.dev/dl/) installed and available on your `PATH`.
- Git.

### Step 1: Install `go-anything`

Install the CLI using prebuilt binaries, `go install`, or from source (see [Installation & Verification](#installation--verification) or [Docs/install.md](Docs/install.md) for full instructions and cryptographic verification):

```bash
# Option A: Go toolchain
go install github.com/mohammedrefaat/go-anything/cmd/go-anything@latest

# Option B: Clone and install locally
git clone https://github.com/mohammedrefaat/go-anything.git
cd go-anything
go install ./cmd/go-anything

# Verify installation
go-anything version
# Output: go-anything v2.0.1 (profile: full, jobs: 8)
```

*(Alternatively, run commands directly via `go run ./cmd/go-anything <subcommand>`.)*

---

### Step 2: Initialize or Modernize a Workspace

Create a dedicated migration workspace (`--source` supports `go`, `python`, `typescript`, `javascript`) or modernize an existing Go project. For a legacy Go microservice, `modernize` (alias for `init --source=go`) creates the `legacy/` reference copy and `modern/` target scaffold:

```bash
# Initialize a workspace for an existing service
go-anything init --source=go --name=sample-service /path/to/my-service
cd /path/to/my-service

# Inspect workspace health and security baseline
go-anything doctor
```

`go-anything doctor` validates:
- Unit state store in `.go-anything/units/`
- `.gitignore` protection for `.go-anything/local/` (preventing accidental lease commits)
- Redaction rules in `.go-anything/rules.json`
- Sandbox isolation readiness

---

### Step 3: Inspect Units and Dependency Waves

View registered units, their dependency waves, and active leases:

```bash
go-anything status
```

Output:
```text
Workspace: sample-service (schemas: v1)
Dependency Waves:
  Wave 0:
    [Ready] unit-healthcheck (dependencies: none, risk: low)
    [Ready] unit-auth-token  (dependencies: none, risk: medium)
  Wave 1:
    [Planned] unit-user-api   (dependencies: [unit-auth-token], risk: high)
```

---

### Step 4: Record Behavioral Cases (Sandboxed & Redacted)

Capture behavioral test cases from the original application. `go-anything record` executes the command inside a restricted sandbox, performs in-memory fail-closed redaction of sensitive credentials, and saves sanitized JSONL fixtures:

```bash
# Record behavioral cases from the original CLI or service
go-anything record \
  --unit unit-healthcheck \
  --cmd "./legacy-service --health" \
  --dir .

# Or intercept cases from existing Go/Python/Jest tests and capture side-effects:
# go-anything record --unit unit-healthcheck --from-tests --effects --dir .
```

`go-anything` automatically:
- Executes the original binary twice to detect volatile outputs (timestamps, monotonic counters).
- Applies format-preserving substitution to secrets (API keys, AWS secrets, URL credentials, tokens, cookies).
- Stores sanitized fixtures in `.go-anything/cases/unit-healthcheck.jsonl`.
- Transitions unit state from `Planned` to `Recorded`.

---

### Step 5: Implement the Ported Unit in Modern Go

Write the ported unit implementation in `modern/` (or let an autonomous coding agent write it):

```go
// modern/health/main.go
package main

import (
    "encoding/json"
    "os"
)

type HealthResponse struct {
    Status string `json:"status"`
}

func main() {
    resp := HealthResponse{Status: "ok"}
    _ = json.NewEncoder(os.Stdout).Encode(resp)
}
```

Build the ported target:
```bash
go build -o ./bin/modern-health ./modern/health/main.go
```

---

### Step 6: Concurrent Deterministic Replay

Replay all recorded cases against the ported Go binary across worker pools:

```bash
go-anything replay \
  --unit unit-healthcheck \
  --target ./bin/modern-health \
  --dir .
```

Output:
```text
[REPLAY] Replaying 10 recorded cases for 'unit-healthcheck' across 8 workers...
  ✓ case-001: MATCH (0.8ms)
  ✓ case-002: MATCH (0.9ms)
  ...
  ✓ case-010: MATCH (0.7ms)
Replay complete: 10/10 passed (0 mismatches, 0 errors).
```

---

### Step 7: Verification & Quality Gate

Verify the unit against the `full` quality gate (`build`, `format`, `vet`, `staticcheck`, `race`, `govulncheck`, `replay`, `error_paths`, `api_compat`) or `--profile replay` for behavioral-only checks:

```bash
# Verify behavioral equivalence against recorded cases
go-anything verify unit-healthcheck \
  --target ./bin/modern-health \
  --profile replay \
  --dir .
```

When running the `full` profile (`go-anything gate` or MCP `run_gate`), v2 commit-tied evidence (`schemas/v2/evidence.schema.json`, `profile_version: "2.0.1"`) is recorded under `.go-anything/evidence/`, advancing the unit state to `Verified`.

---

### Step 8: Human-in-the-Loop Approval, Promotion & Lift

In this walkthrough, `go-anything approve unit-healthcheck --skip-shadow` skips shadow validation and moves the unit directly from Verified to Approved. The operator can then promote the route and retire the legacy original once fallback traffic reaches zero:

```bash
# 1. Human sign-off (CLI-only, cannot be executed by MCP tools)
go-anything approve unit-healthcheck --skip-shadow

# 2. Switch strangler routing to modern Go implementation
go-anything promote unit-healthcheck

# 3. Retire the legacy bridge once fallback traffic reaches zero
go-anything lift unit-healthcheck
```

---

## Hot-Path Sidecars & Differential Testing

Starting in `v2.0`, `go-anything` supports **hot-path strangler sidecars** alongside whole-unit migrations, allowing Python and TypeScript/Node applications to offload CPU- or latency-critical functions to a Go stdio JSON-RPC sidecar with automatic crash recovery and fallback ([ADR-005](Docs/decisions/adr-005-hotpath-sidecar-protocol.md), [Docs/hotpath.md](Docs/hotpath.md)).

```text
profile → bridge → go (hotpath) → fuzz / bench → gate → shadow → human approve / enable / lift
```

Key commands (all 32 CLI commands are wired with 100% E2E coverage in [STATUS.md](STATUS.md)):

- **Discovery & Differential Testing:** `profile` (rank candidate hot functions), `fuzz` (differential fuzzing across recorded vectors), `bench` (comparative latency & allocation benchmarks), `diff` (structural case diffs).
- **Sidecar Lifecycle:** `bridge` (generate Go sidecar + Python/TS wrappers), `go` (guided hot-path porting up to human approval), `enable` / `disable` (toggle bridge routing), `map` (function porting status), `refresh` (re-record & re-diff), `clean` (remove generated bridge artifacts), `report` (Markdown/JSON scorecard).

**Real-World Benchmarks** (reproducible via `make demo-node` and `make demo-python`; see [Docs/real-world.md](Docs/real-world.md)):

| Application | Workload | Replay Parity | p50 Speedup | Throughput Gain |
| :--- | :--- | :---: | :---: | :---: |
| **Node.js REST API** (`demo/real-world/node-rest-api`) | CRUD REST API (`typicode/json-server`) | 100% (9/9 cases) | **1.38x faster** | **+41.7%** |
| **Python Task Worker** (`demo/real-world/python-worker`) | Stats, crypto hashing & filtering | 100% (8/8 cases) | **5.69x faster** | **+472%** |

---

## Milestone Roadmap (M0–M7 & v2.0)

All core milestones (M0–M7) and v2.0 releases have been implemented, proven by automated test suites, and verified:

| Milestone | Release | Proving CLI Command(s) | Proving Test File(s) | Scope & Deliverables | Exit Gate Status |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **M0: Foundation** | `v0.0.x` | [`init`](cmd/go-anything/init.go), [`doctor`](cmd/go-anything/doctor.go), [`status`](cmd/go-anything/status.go), [`template`](cmd/go-anything/template.go) | [`cmd/go-anything/cli_test.go`](cmd/go-anything/cli_test.go) | State machine (10 states), atomic I/O, file locks, fail-closed redactor, sandbox, CLI skeleton, ADR-001/002. | ✅ **Passed:** `go-anything init` + `doctor` pass with clean validation. |
| **M1: Sandboxed Replay** | `v0.0.x` | [`record`](cmd/go-anything/record.go), [`replay`](cmd/go-anything/replay.go), [`diff`](cmd/go-anything/diff.go) | [`cmd/go-anything/e2e_m1_test.go`](cmd/go-anything/e2e_m1_test.go) | Dual-run volatility detection, multi-mode comparator (exact, json, numeric, regex), concurrent replay engine. | ✅ **Passed:** 100% replay across 3 identical consecutive runs. |
| **M2: Front Door & Lift** | `v0.0.x` | [`verify`](cmd/go-anything/verify.go), [`approve`](cmd/go-anything/approve.go), [`promote`](cmd/go-anything/promote.go), [`cutover`](cmd/go-anything/cutover.go), [`lift`](cmd/go-anything/lift.go) | [`cmd/go-anything/e2e_m2_test.go`](cmd/go-anything/e2e_m2_test.go), [`cmd/go-anything/verify_approve_promote_test.go`](cmd/go-anything/verify_approve_promote_test.go) | Reverse proxy front door, emergency kill-switch (`KILL_SWITCH=1`), fallback monitoring, and `lift` command. | ✅ **Passed:** Full strangler lifecycle on demo CLI & HTTP applications. |
| **M3: Go Source Adapter** | `v0.1.0` | [`modernize`](cmd/go-anything/init.go), [`analyze`](cmd/go-anything/analyze.go) | [`cmd/go-anything/modernize_test.go`](cmd/go-anything/modernize_test.go), [`cmd/go-anything/dogfood_test.go`](cmd/go-anything/dogfood_test.go) | Go AST analyzer, type resolution, call graph dependency wave planner, `go-anything modernize` workflow. | ✅ **Passed:** Real Go service ported with 0 mismatches. |
| **M4: Quality Gate & MCP** | `v0.1.0` | [`gate`](cmd/go-anything/gate.go), [`mcp`](cmd/go-anything/mcp.go) | [`cmd/go-anything/m4_acceptance_test.go`](cmd/go-anything/m4_acceptance_test.go), [`cmd/go-anything/mcp_test.go`](cmd/go-anything/mcp_test.go) | Multi-check toolchain quality gate runner (`AllChecks` / `FullProfileChecks`), scorecard engine, stdio MCP server (M4 shipped 8; now 14, see [STATUS.md](STATUS.md)) with payload sanitization. | ✅ **Passed:** Autonomous AI agent ports a complete unit unaided. |
| **M5: Shadow & Capture** | `v0.1.0` | [`shadow`](cmd/go-anything/shadow.go) | [`cmd/go-anything/m5_acceptance_test.go`](cmd/go-anything/m5_acceptance_test.go) | Real-time traffic shadowing, bounded pool with overflow counters, idempotent-only GET/HEAD default, capture ingestion. | ✅ **Passed:** Clean shadow log on live traffic + dogfood case study. |
| **M6: Python Adapter** | `v0.2.0` | [`init --source=python`](cmd/go-anything/init.go) | [`cmd/go-anything/m6_acceptance_test.go`](cmd/go-anything/m6_acceptance_test.go) | Python source adapter (AST import analysis, pytest discovery, Subprocess, HTTP, C-Shared transports, foreign library wrappers). | ✅ **Passed:** Python CLI and FastAPI HTTP service ported + conformance suite pass. |
| **M7: TypeScript & Freeze** | `v1.0.0` | [`migrate`](cmd/go-anything/migrate.go), [`init --source=typescript`](cmd/go-anything/init.go) | [`cmd/go-anything/m7_acceptance_test.go`](cmd/go-anything/m7_acceptance_test.go), [`cmd/go-anything/migrate_test.go`](cmd/go-anything/migrate_test.go) | TypeScript/Node adapter (ESM/CommonJS, Jest/Vitest), `schemas/v1/` freeze, `go-anything migrate`, `GOVERNANCE.md`. | ✅ **Passed:** Multi-unit open-source project fully lifted with zero fallback traffic. |
| **v2.0: Hot-Path & JS** | `v2.0.0` | [`fuzz`](cmd/go-anything/fuzz.go), [`bench`](cmd/go-anything/bench.go), [`profile`](cmd/go-anything/profile.go), [`bridge`](cmd/go-anything/bridge.go), [`go`](cmd/go-anything/go.go), [`setup`](cmd/go-anything/setup.go) | [`cmd/go-anything/go_test.go`](cmd/go-anything/go_test.go), [`cmd/go-anything/companion_test.go`](cmd/go-anything/companion_test.go), [`cmd/go-anything/setup_test.go`](cmd/go-anything/setup_test.go) | Stdio JSON-RPC sidecars (ADR-005), differential fuzz/bench/profile, `record --from-tests`/`--effects`, 14 MCP tools, multi-tool `setup`, JS adapter, `schemas/v2/`. | ✅ **Passed:** 32/32 commands wired with E2E coverage + real-world Node/Python ports. |
| **v2.0.1: Gate & Security Hardening** | `v2.0.1` | [`gate`](cmd/go-anything/gate.go), [`mcp`](cmd/go-anything/mcp.go) | [`cmd/go-anything/owner_test.go`](cmd/go-anything/owner_test.go), [`cmd/go-anything/mcp_test.go`](cmd/go-anything/mcp_test.go), [`cmd/go-anything/stubs_guard_test.go`](cmd/go-anything/stubs_guard_test.go) | Canonical owner identity (ADR-007), code-derived docs (`tools/genstatus`), `staticcheck` in `full` gate + v2 evidence (ADR-008), expanded redactor, sandboxed sidecar builds, multi-language `inspect_unit`. | ✅ **Passed:** Full CI (`staticcheck`, `govulncheck`, `-race`, dogfood gate) & drift guards pass. |

> [!NOTE]
> **Upcoming Security & Fix Plan ([Docs/GO_ANYTHING_FIX_AND_SECURITY_PLAN.md](Docs/GO_ANYTHING_FIX_AND_SECURITY_PLAN.md)):**
> - **`v2.0.1` (Shipped — Part A):** Code-derived docs, canonical identity, expanded redactor (`redactor.Detect`), `staticcheck` in `full` gate with v2 evidence, multi-language `inspect_unit`, sandboxed sidecar builds, and CI supply-chain hardening.
> - **`v2.1.0` (Planned — Security Advisory & Audit):** Shared `internal/audit/` engine with type-checked `go/packages` driver, first 8 Go security rules (`GA-SQLI`, `GA-CMDI`, `GA-WEAKRAND`, `GA-BODYLIMIT`, `GA-TLS`, `GA-SERVER`, `GA-SECRET`, `GA-WEAKHASH`), `security` gate check (advisory in `full`, opt-in `--security=enforce`), `go-anything audit` CLI (`console`/`json`/`sarif`), read-only MCP `audit_code`, and legacy-tree secret warnings (`go-anything audit secrets`).
> - **`v2.2.0` (Planned — Security Parity & Enforcement):** Differential security parity (`security_parity` negative-case replay for authn/authz/validation/limits/method/traversal), `security` promoted to **required** in `full` with an evidence schema bump, redacted legacy tree view (`go-anything view build`), heuristic `audit agent-config` lint, and release SBOM/`audit --deps`.

---

## Known Limitations

1. **CGO and Data Race Detector Host Requirements:**
   The `full` verification profile runs `go test -race`, which requires CGO and a host C compiler (GCC/Clang on Linux/macOS, MinGW-w64 on Windows). In CGO-free or cross-compiled environments (`CGO_ENABLED=0`), the race detector check is skipped (`StatusSkipped`). Environments without a C compiler should specify the `--profile=replay` evidence profile or provision a C toolchain.
2. **Behavioral Parity Claim Boundary (Rule R-6):**
   `go-anything` claims behavioral equivalence **strictly and only for recorded, observed, and fuzzed inputs** (happy paths, edge cases, semantic traps, and captured production fixtures). It does **not** provide a mathematical proof or blanket assertion of universal semantic parity across unobserved, non-tested execution paths. Teams must review diffs, test vectors, and coverage scorecards before approving cutover.
3. **Sandbox Isolation Tiers (ADR-001):**
   Full kernel-level namespace isolation (network, mount, pid, ipc namespaces via Linux `unshare`/cgroups) is available as **Tier 1** on Linux hosts. On macOS and Windows environments, **Tier 2** best-effort process isolation is applied: restrictive environment variable allowlisting, local temporary directory virtualization, non-zero timeout enforcement, and loopback binding guards. Bare-metal network hardware isolation is not guaranteed under Tier 2; use containerized or Linux CI runners for strict sandbox isolation.
4. **Python C-Shared Extension Status:**
   CPython C-Shared library in-process invocation (`adapters/source/python/cshared`) is experimental due to CPython Global Interpreter Lock (GIL) constraints, signal trapping, and thread-safety pitfalls across Go runtimes. For production Python migrations, the Subprocess and HTTP sidecar transports (`adapters/application/cli` and `adapters/application/http`) are the recommended, supported approaches.
5. **Strict Human-Only Control Boundaries (Execution Rule 4):**
   Altering live production routing tables, approving migration units, and permanently retiring legacy original code via `lift` are strictly human-only CLI operations (`approve`, `promote`, `lift`, `cutover`, `shadow`, and routing/kill-switch controls). These commands are intentionally excluded from the MCP agent server interface and cannot be invoked or forged by autonomous agents.
6. **Offline Sandboxed Sidecar Builds:**
   Hot-path sidecar `go build` and `go vet` commands execute inside `internal/sandbox` with networking disabled (`GOPROXY=off`, `GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly`). Workspaces using external Go dependencies must vendor them (`go mod vendor`) or warm the local `GOMODCACHE` beforehand (see [Docs/hotpath.md](Docs/hotpath.md)).

---

## Autonomous Agent & MCP Workflow

`go-anything` includes a built-in Model Context Protocol (MCP) server that runs over `stdio` without external network listeners, plus a `setup` command to register it non-destructively across AI coding tools (Claude Code, Codex, Cursor, Gemini, and OpenCode):

```bash
# Preview and install MCP registration & agent instructions (creates .bak backups)
go-anything setup --tool all --dry-run
go-anything setup --tool all --yes

# Or start the MCP server directly over stdio
go-anything mcp
```

### The Standard & Companion MCP Tools

The MCP server enforces the 10-state unit lifecycle server-side. Agents cannot forge gate results or skip steps (see [STATUS.md](STATUS.md) for the full 32-command traceability matrix and CLI exit codes):

#### Core Lifecycle Tools (8 of 14 total)
1. `status` — Show progress, next unit in dependency wave order, active leases, and blockers.
2. `claim_unit` — Take a Ready unit with a time-bounded local lease in `.go-anything/local/`.
3. `inspect_unit` — View original source code (Go, Python, TypeScript, JavaScript), sanitized sample cases, detected traps, and constraints.
4. `record_cases` — Run the original application in the sandbox, apply fail-closed redaction, and save JSONL.
5. `verify_diff` — Build the port, replay all cases, and return PASS or top sanitized mismatches.
6. `run_gate` — Run the full quality gate pipeline (`build`, `format`, `vet`, `staticcheck`, `race`, `govulncheck`, `replay`, `error_paths`, `api_compat`) and write v2 commit-tied evidence.
7. `mark_verified` — Advance unit to Verified (fail-closed: rejected unless valid v2 evidence exists for HEAD).
8. `handoff` — Write `HANDOFF.md` with progress and next steps for subsequent sessions.

#### Companion & Hot-Path Tools (v2.0)
- `analyze` — Analyze legacy codebase and output dependency wave plan.
- `profile` — Profile legacy execution hotspots and rank candidate functions for migration.
- `bridge` — Scaffold strangler sidecar bridge, manifest, and client wrappers.
- `hotpath` — Execute guided hot-path porting pipeline up to the human approval boundary.
- `fuzz_bench` — Run differential fuzz testing or performance benchmarks.
- `shadow_status` — Inspect front-door shadow mode execution metrics and mismatch logs.

> **Human-Only Boundary:** The MCP server **never** exposes `approve`, `promote`, `lift`, `routing`, `toggle_shadow`, or `kill_switch`. Any attempt to invoke them via MCP is rejected fail-closed with `FORBIDDEN` (Execution Rule 4).

---

## Architecture & Design Principles

1. **Small, Reversible Steps:** Replace software incrementally using the strangler pattern. The original remains immediately available as fallback until final lift.
2. **Fail-Closed In-Memory Redaction:** Sensitive credentials (API keys, AWS secrets, URL credentials, tokens, private keys, session cookies, cards) are replaced before any data hits disk. Unparseable sensitive payloads abort recording unless explicitly overridden.
3. **Restricted Sandboxing:** Application execution and sidecar builds occur within sandbox boundaries: Linux namespaces (Tier 1) and best-effort process isolation on macOS/Windows (Tier 2). Network access is disabled by default.
4. **Deterministic Evidence:** Test cases, diffs, and scorecards are deterministically sorted by `case_id`, never execution time.
5. **Local-First, Zero Telemetry:** All state, leases, and evidence remain inside `.go-anything/`. No telemetry is transmitted over the network.
6. **Decoupled Adapter Architecture:** Language adapters interact via the frozen `pkg/adapterkit` contract and must achieve 100% compliance on `pkg/adapterkit/conformance`.

---

## Repository Map

```text
cmd/go-anything/        CLI entry point (32 wired commands) and MCP stdio server
pkg/adapterkit/         Public, frozen adapter contracts (v1)
  └── conformance/      Official adapter certification test suite
internal/               Core migration engine:
  ├── adapterhost/      Adapter discovery, manifest loading & runtime host
  ├── analyzer/         Source code AST, type analysis & call graphs
  ├── app/              Application use cases (hot-path, fuzz, bench, profile, gate)
  ├── bench/            Latency and allocation benchmarks
  ├── bridge/           Hot-path strangler sidecar bridge scaffolding & wrappers
  ├── comparator/       Multi-mode diffing (exact, json, numeric, regex, effects)
  ├── frontdoor/        Reverse proxy front door & kill-switch routing
  ├── gate/             Quality gate runner (build, format, vet, staticcheck, race, vuln)
  ├── lift/             Legacy fallback retirement engine
  ├── mcp/              Stdio MCP server (14 tools) with payload sanitization
  ├── migrate/          Workspace schema migration engine
  ├── planner/          Topological dependency wave planner
  ├── recorder/         Sandboxed dual-run case recording & volatility detection
  ├── redactor/         Fail-closed in-memory credential detection & redaction
  ├── replay/           Concurrent deterministic replay engine
  ├── sandbox/          Linux namespaces & OS process isolation
  ├── setup/            Non-destructive multi-tool MCP & template setup
  ├── shadow/           Live shadow mode engine with bounded worker pools
  └── state/            10-state unit lifecycle, atomic I/O, file locks & v2 evidence
adapters/               Language and runtime adapters:
  ├── source/           Source adapters (Go, Python, TypeScript, JavaScript)
  └── application/      Runtime adapters (CLI, HTTP, Foreign Library, Worker)
skills/                 Agent workflow skill definitions
templates/              Agent instruction templates (CLAUDE.md, AGENTS.md, etc.)
tools/genstatus/        Code-derived STATUS.md and MCP tool table generator
packaging/              Package manager wrappers (npm, Homebrew, Scoop)
demo/                   Real-world migration benchmarks (Node REST API, Python Worker)
schemas/                Versioned JSON Schemas:
  ├── v1/               Stable frozen schemas (manifest, units, rules, cases, etc.)
  ├── v2/               v2 command & evidence schemas (evidence, fuzz, bench, profile, etc.)
  └── v1alpha1/         Legacy schemas (supported via go-anything migrate)
Docs/                   Architecture, guides, and documentation:
  ├── decisions/        Architecture Decision Records (ADR-001, 002, 005, 007, 008)
  ├── case-studies/     Public dogfooding case studies (M3, M4, M5, M6, M7)
  ├── USER_GUIDE.md     Comprehensive user guide and CLI reference
  ├── hotpath.md        Sandboxed sidecar build & offline prerequisites
  ├── how-it-works.md   Deep dive into strangler recording & verification
  ├── real-world.md     External Node.js & Python migration benchmarks
  ├── pitfalls.md       Semantic traps & migration pitfalls
  ├── faq.md            Frequently asked questions
  ├── privacy.md        Local-first privacy & zero-telemetry guarantees
  ├── whole-apps.md     Full-application migration guide
  └── install.md        Platform matrix, checksums, and Cosign verification
STATUS.md               Code-derived 32-command & 14-MCP-tool traceability matrix
CHANGELOG.md            Release history and upgrade notes
GOVERNANCE.md           Maintainer roles, consensus, RFCs, and adapter certification
SECURITY.md             Threat model, sandboxing, and vulnerability disclosure policy
CONTRIBUTING.md         Development prerequisites, coding standards, and verification
```

---

## Governance & Contributing

- **Governance Model:** Read [GOVERNANCE.md](GOVERNANCE.md) for details on maintainer tiers (Core Maintainers, Adapter Maintainers, Contributors), consensus models, the RFC process for schemas and `pkg/adapterkit`, and adapter certification.
- **Security Policy:** Read [SECURITY.md](SECURITY.md) for the complete threat model, sandboxing tiers, and private vulnerability reporting workflows.
- **Contributing:** Read [CONTRIBUTING.md](CONTRIBUTING.md) for local environment setup, pre-commit git hooks, and verification requirements.

To enable the automated git hooks:

```bash
git config core.hooksPath .githooks
```

---

## License

`go-anything` is distributed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) for attributions.
