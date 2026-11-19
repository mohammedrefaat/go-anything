# Git Hooks (`.githooks/`)

This directory contains version-controlled Git hooks for `go-anything`. They enforce the design principles and execution rules defined in [README.md](../README.md), [AGENTS.md](../AGENTS.md), and [CONTRIBUTING.md](../CONTRIBUTING.md).

The hooks are dual-compatible with both:
1. **[Githooks](https://github.com/rycus86/githooks)** (and the Go port [gabyx/Githooks](https://github.com/gabyx/Githooks))
2. **Native Git (>= 2.9)** via `core.hooksPath`

---

## Quick Setup

### Option A: Native Git (Recommended, Zero Dependencies)

Run this once in the repository:

```bash
git config core.hooksPath .githooks
```

### Option B: With `githooks` / `gabyx/Githooks`

If you or your team have `githooks` installed globally:
- Hooks in `.githooks/` run automatically on `clone` and `commit`.
- The repository includes `.githooks/trust-all`, which automatically marks the hooks as trusted by project maintainers so you don't get prompted on every commit.
- The `.githooks/.ignore` file ensures markdown and documentation files are not executed as hook scripts.

---

## Active Hooks & Checks

| Hook | Lifecycle Stage | Checks Performed | Core Rule Enforced |
| :--- | :--- | :--- | :--- |
| **`pre-commit`** | Before commit is saved | • Blocks any staged files under `.go-anything/local/`<br>• Enforces `gofmt -l`<br>• Runs `go vet` on changed packages<br>• Scans diffs for sensitive tokens & private keys<br>• Verifies `go mod verify` consistency | **Rule 1** (Leases never committed)<br>**Rule 3** (Fail-closed secret safety) |
| **`commit-msg`** | After writing commit message | • Rejects empty messages (only hard block)<br>• Recommends subject line under 72 chars (non-blocking tip)<br>• Suggests conventional commit prefix (`feat:`, `fix:`, `adr:`, etc.) | **Small, Reversible Steps**<br>(Clear change history) |
| **`pre-push`** | Before branch is pushed | • `go build ./...`<br>• `go vet ./...`<br>• `go test` (with `-race` if CGO is enabled)<br>• `govulncheck` (if installed locally) | **Rule 5** (Verification gate before completion) |

---

## Conventional Commit Prefixes

The `commit-msg` hook encourages standard conventional prefixes:

```text
feat:      New capability, adapter, or feature
fix:       Bug fix or parity correction
docs:      Documentation or architecture plan update
adr:       Architecture Decision Record (Docs/decisions/)
test:      New test cases, traps, or fuzzers
refactor:  Code restructuring without behavioral change
chore:     Dependencies, toolchain, or maintenance
ci:        GitHub Actions or CI workflows
build:     Build flags or packaging
perf:      Performance optimization or benchmark
```

Example: `feat(state): enforce atomic write with file lock`

---

## Bypassing Hooks

If you need to make an emergency commit or temporary local stash:

```bash
# Bypass single commit or push
git commit --no-verify -m "wip: temporary checkpoint"
git push --no-verify

# Or disable via environment variable (Githooks standard)
GITHOOKS_DISABLE=1 git commit -m "wip: temporary checkpoint"
```

> [!NOTE]
> CI on GitHub Actions runs `go test -race` and `govulncheck` on all pull requests regardless of local hook status.
