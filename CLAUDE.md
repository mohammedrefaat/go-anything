# CLAUDE.md — Claude Code Instructions for go-anything

> Instructions for Claude Code (claude-code, claude-dev) when working on the `go-anything` repository.

---

## Project Overview

`go-anything` migrates applications to Go one unit at a time and proves each unit matches the original's recorded behavior before it ships. It is **not** a code translator — the coding agent writes the Go; `go-anything` orders the work, records evidence, replays it, enforces gates, and keeps migration state outside any chat session.

**Module:** `github.com/mohammedrefat/go-anything`
**License:** Apache-2.0
**Go version:** 1.26+ (see `go.mod`)
**Status:** Early development — CLI provides `help` and `version`; migration commands are not yet wired.

---

## Repository Layout

```text
cmd/go-anything/        CLI entry point (main.go)
pkg/adapterkit/         Public adapter contract and schema types (NEVER imports internal/)
internal/               Core packages:
  analyzer/             Source analysis
  bench/                Benchmark capture
  comparator/           Multi-mode case comparison
  gate/                 Quality gate runner
  mcp/                  MCP server and payload sanitizer
  planner/              Dependency wave planner
  recorder/             Case recording with dual-run volatility detection
  redactor/             Fail-closed in-memory redaction
  replay/               Concurrent deterministic replay engine
  sandbox/              Restricted execution sandbox
  shadow/               Shadow mode engine
  state/                Unit lifecycle state machine, atomic I/O, leases
adapters/               Language and application adapter stubs
schemas/                JSON Schemas (v1alpha1) and schema validation
  v1alpha1/             manifest, units, rules, cases, reports, evidence schemas
skills/                 Agent skill definitions
templates/              Agent instruction templates
Docs/                   Architecture plan, task plan, ADRs
  decisions/            Architecture Decision Records
.github/workflows/      CI (go test -race, govulncheck)
```

---

## Critical Rules — ALWAYS Follow

### 1. State and Locking
- Durable unit state lives in `.go-anything/units/`, one file per unit.
- Write atomically: temp file → `fsync` → rename, under an OS file lock.
- Leases and locks live only in the gitignored `.go-anything/local/`.
- **Never commit lease or lock files.**

### 2. Determinism
- Sort reports, scorecards, and diffs by `case_id`, **never** by completion time.
- Respect `--jobs N`, default `runtime.NumCPU()`.

### 3. Fail-Closed Security
- In-memory redaction and no-network sandboxing are **on by default**.
- Zero telemetry, strictly local operation.
- External adapters may **never** disable core redaction or shadow policy.

### 4. Human-Only Controls
- `approve`, `promote`, `lift`, and routing changes are **CLI-only**.
- **Never** expose these as MCP tools. Never implement or suggest bypasses.

### 5. Public Contract Boundary
- `pkg/adapterkit` must **never** import `internal/`.
- `Redactor` and `ShadowPolicy` are **never** exported from `adapterkit`.

### 6. Test Style
- Table-driven tests with `github.com/stretchr/testify`.
- Mocks only through explicit Go interfaces.
- **No real network calls** in unit tests.
- Always run with the race detector: `go test -race ./...`.

### 7. Verification Before Completion
Before marking any task complete, all of these must pass with zero failures:
```bash
gofmt -s -w .
go vet ./...
go test -race ./...
```

### 8. Decision Escalation
When you encounter an unsettled architectural question, **stop and append a short ADR** with a recommended answer to `DECISIONS.md` and `Docs/decisions/` instead of guessing. Follow the format in existing ADRs (see `adr-001-sandbox-backend.md`).

### 9. Stay in Scope
Change only the files named in a task's Scope section. Anything else needs an ADR.

### 10. Evidence Profiles
- **`replay`** profile: zero mismatches on recorded cases (used until M4).
- **`full`** profile: every quality gate check required (default from M4 on).
- A tagged release counts a unit as Completed only under `full`.

---

## MCP Server Tools (What You Can Use)

When working through the `go-anything mcp` server, only these eight tools are available:

| Tool | Purpose |
| :--- | :--- |
| `status` | Progress, next unit in dependency order, active leases, blockers |
| `claim_unit` | Take a unit with a time-bounded local lease |
| `inspect_unit` | Original source, sanitized sample cases, detected traps |
| `record_cases` | Run the original in sandbox, apply redaction, save JSONL |
| `verify_diff` | Build, replay all cases, return PASS or top mismatches |
| `run_gate` | Run quality gate, write scorecard, store evidence |
| `mark_verified` | Move to Verified (requires commit-tied evidence) |
| `handoff` | Write HANDOFF.md with state and next steps |

**You cannot and must not:** approve, promote, lift, change routing, or modify the shadow mode toggle. These require a human at the CLI.

---

## Agent Workflow Loop

Follow this loop for every migration unit:

```
claim → inspect → port → verify_diff → fix → run_gate → mark_verified → handoff
```

1. **Claim:** `claim_unit` to take a lease on the next available unit.
2. **Inspect:** `inspect_unit` to understand the original code, sample cases, and traps.
3. **Port:** Write the Go implementation. Keep changes within the task's scope.
4. **Verify:** `verify_diff` to build and replay all recorded cases against your port.
5. **Fix:** If mismatches exist, fix the Go code and re-run `verify_diff`.
6. **Gate:** `run_gate` to run the full quality gate (build, vet, staticcheck, tests, govulncheck, replay).
7. **Mark:** `mark_verified` to advance the unit's state (requires evidence tied to current commit).
8. **Handoff:** `handoff` to write `HANDOFF.md` for the next session or human reviewer.

If you encounter an **architectural question** not settled by the plan, write an ADR in `DECISIONS.md` and stop — do not guess.

---

## Go-to-Go Semantic Traps

When porting Go code, watch for these common behavioral divergences:

| Trap | Why It Bites |
| :--- | :--- |
| Empty collections | A nil slice marshals to `null`, an empty one to `[]` |
| HTML escaping | `encoding/json` escapes `<`, `>`, and `&` by default |
| Map key order | Go sorts keys on marshal; the original may keep insertion order |
| Numbers | Untyped JSON numbers decode to `float64`; int64 overflow, `-0`, NaN |
| Strings | UTF-16 vs UTF-8 offsets, rune vs byte slicing, `\r\n` vs `\n` |
| Time | Formats, zones, and sub-second precision differences |
| Error paths | Malformed input, missing fields, non-zero exit codes |
| `go.mod` directive | Raising `go` changes loop var semantics, `GODEBUG`, `ServeMux` patterns |

---

## Comparator Modes

The comparator (`internal/comparator/`) supports these modes — choose the right one for each case:

- `exact` — byte-for-byte match
- `structural_json` — JSON structure match (ignoring key order)
- `numeric_tolerance` — numeric values within a tolerance
- `error_equivalence` — error type or message match
- `http_contract` — HTTP status, headers, body contract
- `filesystem_snapshot` — directory tree and file content match

Distinctions the comparator checks: missing vs `null`, integer vs float, ordered vs unordered collections.

---

## Key Files to Know

| File | Purpose |
| :--- | :--- |
| [README.md](README.md) | Project overview and quickstart |
| [SECURITY.md](SECURITY.md) | Threat model and disclosure policy |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Development prerequisites and code standards |
| [DECISIONS.md](DECISIONS.md) | ADR index and resolved decisions |
| [Docs/go-anything Architecture & Implementation Plan (v3) (1).md](<Docs/go-anything Architecture & Implementation Plan (v3) (1).md>) | Full architecture and design |
| [Docs/go-anything AI Agent Task Plan (v3.1).md](<Docs/go-anything AI Agent Task Plan (v3.1).md>) | Ordered task backlog with dependencies and exit gates |

---

## What Not to Do

- ❌ Do not import `internal/` from `pkg/adapterkit/`.
- ❌ Do not expose `approve`, `promote`, `lift`, or routing as MCP tools.
- ❌ Do not add network calls to unit tests.
- ❌ Do not sort anything by completion time — use `case_id`.
- ❌ Do not commit `.go-anything/local/` files.
- ❌ Do not bypass fail-closed redaction in adapters.
- ❌ Do not guess at architectural questions — write an ADR.
- ❌ Do not claim parity beyond what recorded cases cover.
