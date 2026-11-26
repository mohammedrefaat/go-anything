# Contributing to go-anything

Thank you for your interest in contributing to `go-anything`! We welcome contributions, bug reports, and improvements from human developers and AI agent contributors alike.

---

## Development Prerequisites

- **Go 1.27+**
- Git
- Standard Go toolchain tools (`gofmt`, `go vet`, `staticcheck`, `govulncheck`)

---

## Setup

After cloning, activate the git hooks:

```bash
git config core.hooksPath .githooks
```

This enables three hooks that enforce quality standards automatically:

| Hook | When | What It Checks |
| :--- | :--- | :--- |
| **pre-commit** | Before each commit | `gofmt`, `go vet`, blocks `.go-anything/local/` commits, scans for secrets |
| **commit-msg** | After writing message | Rejects empty messages, enforces 72-char subject, suggests conventional prefixes |
| **pre-push** | Before pushing | Full Rule 5 gate: `go build`, `go vet`, `go test -race`, `govulncheck` |

Bypass for one-off situations: `git commit --no-verify` / `git push --no-verify` (or `GITHOOKS_DISABLE=1`). See [.githooks/README.md](.githooks/README.md) for details.

---

## Architecture & Code Standards

Every contributor must adhere to the core architectural principles:

1. **State & Locking:**
   - Unit state lives in `.go-anything/units/` (one file per unit), written atomically (temp file, `fsync`, rename) under an OS file lock.
   - Leases, claims, and ephemeral locks live strictly in the gitignored `.go-anything/local/`. Never commit active leases.
2. **Determinism:**
   - Always sort test cases, reports, diffs, and scorecards deterministically by `case_id`, never by execution or completion order.
   - Respect `--jobs N` (defaulting to `runtime.NumCPU()`).
3. **Fail-Closed Security:**
   - In-memory redaction and no-network sandboxing are enabled by default.
   - Zero telemetry and strictly local operation.
   - External adapters may never disable core redaction or shadow policy.
4. **Human-Only Operations:**
   - `approve`, `promote`, `lift`, and routing changes are CLI-only. Never expose these as MCP tools.
5. **Clean Boundaries:**
   - `pkg/adapterkit` is the public adapter API and must **never** import `internal/`.
   - `Redactor` and `ShadowPolicy` are internal core concerns and must never be exported from `adapterkit`.
6. **Testing Standards:**
   - Table-driven tests with `github.com/stretchr/testify`.
   - Mocking only through explicit Go interfaces.
   - No real network calls in unit tests.
   - Mandatory race detector testing (`go test -race ./...`).

---

## Verification Before Submitting

Before submitting a pull request or marking a task complete, run the quality gate:

```bash
# 1. Format code
gofmt -s -w .

# 2. Run static analysis
go vet ./...
staticcheck ./...

# 3. Run unit tests with race detection
go test -race ./...

# 4. Check for known vulnerabilities
govulncheck ./...
```

All checks must pass with zero errors or warnings.

---

## Architectural Decisions

If your work raises an unsettled architectural or design question, please do not guess. Document the trade-offs and submit an Architecture Decision Record (ADR) under `Docs/decisions/` following the existing format.
