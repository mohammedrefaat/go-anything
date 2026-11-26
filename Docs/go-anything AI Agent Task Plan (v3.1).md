# go-anything: AI Agent Task Plan (v3.1)

Nov 19, 2026 · @mohammed refaat

## Purpose and use

This backlog turns the go-anything v3 plan into ordered tasks that coding agents can take one at a time. Every task names its dependencies, the files it may touch, what to build, and an exit gate that can be checked by running something. Milestones M0 to M7 match the roadmap in the v3 plan.

**Task format**

- **Depends on:** tasks that must be finished first
- **Scope:** the directories and files the task may change
- **Tasks:** the concrete deliverables
- **Exit gate:** a command or test that proves the task is done

**How to pick work:** take a task whose dependencies are all complete, check the parallel lanes table for what else can run beside it, and stop at any exit gate that fails. A task is done only when its exit gate and the verification rule in the execution rules both pass.

## Execution rules

Every agent obeys these rules on every task.

1. **State and locking:** durable unit state lives in `.go-anything/units/`, one file per unit, written atomically (temp file, `fsync`, rename) under an OS file lock. Leases and locks live only in the gitignored `.go-anything/local/`.
2. **Determinism:** sort reports, scorecards and diffs by `case_id`, never by completion time. Respect `--jobs N`, default `runtime.NumCPU()`.
3. **Fail-closed security:** in-memory redaction, no-network sandboxing and local-only, zero-telemetry operation are on by default. No external adapter may disable core redaction or shadow policy.
4. **Human-only controls:** `approve`, `promote`, `lift` and any routing change are CLI-only. Never expose them as MCP tools.
5. **Verification per task:** before marking a task complete, `gofmt -s -w .`, `go vet ./...` and `go test -race ./...` must pass with zero failures.
6. **Decision escalation:** when a task reaches an unsettled architectural question, stop and append a short ADR with a recommended answer to `DECISIONS.md` instead of guessing.
7. **Test style:** table-driven tests with testify; mocks only through interfaces; no real network in unit tests.
8. **Public contract:** `pkg/adapterkit` never imports `internal/`, and `Redactor` and `ShadowPolicy` are never exported from it.
9. **Evidence profiles:** a unit is verified under one of two profiles. `replay` requires zero mismatches on the recorded cases and is used until T4.3 builds the full gate. `full` requires every gate check and is the default from T4.3 on. A tagged release counts a unit as Completed only under `full`.
10. **Stay in scope:** change only the files named in the task's Scope; anything else needs an ADR.

## Parallel lanes

Parallelism follows the dependency graph, not the number of agents available. Tasks listed together can run side by side once each one's own dependencies are complete.

| Milestone | Can run in parallel | Must wait for |
| --- | --- | --- |
| M0 | T0.0 with T0.1; then T0.3, T0.4 and T0.5 | T0.2 (schemas) before T0.3 and T0.4; T0.0 before T0.3 and T0.5 |
| M0 | T0.6 with T0.7 | T0.3 and T0.4 |
| M1 | T1.1 with T1.3 | T1.2 waits for T1.1; T1.4 for T1.1, T1.3 and T0.5; T1.5 for T1.4 |
| M2 | T2.1 with T2.2 | T2.3 waits for both; T2.4 for T2.3; T2.5 for T2.4 |
| M3 | T3.4, T3.5 and T3.6 | T3.1, T3.2 and T3.3 run in order before them; T3.7 is last |
| M4 | T4.1 with T4.5; then T4.2 with T4.3 | T4.4 waits for T4.3; T4.6 for T4.3, T4.4 and T4.5; T4.7 and T4.8 follow |
| M5 | T5.1, T5.2 and T5.3 | T5.4 is last |
| M6 | T6.2 with T6.4 | T6.1 first; T6.3 is last |
| M7 | T7.1 with T7.3 | T7.2 waits for T6.3 and T7.1; T7.4 is last |

## Pre-flight and M0: foundation (v0.0.x)

### T0.0: Resolve open decisions

- **Depends on:** none. **Owner:** a human; the agent drafts the ADR templates.
- **Scope:** `DECISIONS.md`, `docs/decisions/`
- **Tasks:** ADR-001 for the sandbox backend (Linux namespaces, `bubblewrap` or a container runtime, plus the best-effort fallback on macOS and Windows). ADR-002 for cross-machine claims (is pull-request ownership enough through v1.0, or is a shared claim service needed).
- **Exit gate:** both ADRs are marked resolved.

### T0.1: Scaffold the repository and day-1 OSS files

- **Depends on:** none
- **Scope:** `go.mod`, `cmd/go-anything/`, `pkg/adapterkit/`, `internal/`, `adapters/`, `schemas/`, `skills/`, `templates/`, `docs/decisions/`, `LICENSE`, `NOTICE`, `README.md`, `SECURITY.md`, `CONTRIBUTING.md`, `.github/`
- **Tasks:** initialize the module and the full skeleton (`analyzer`, `planner`, `recorder`, `replay`, `comparator`, `gate`, `shadow`, `bench`, `mcp`, `state`, `redactor`, `sandbox` under `internal/`). Add the Apache-2.0 `LICENSE` and a `NOTICE` crediting `rust-anything` (MIT, Erkam Yaman). Write `README.md` with the parity disclaimer and a quickstart placeholder, `SECURITY.md` with the threat model, a trust-the-repo warning and a disclosure policy, and a short `CONTRIBUTING.md`. Add a GitHub Actions workflow (`go test -race ./...`, `govulncheck`) and Dependabot.
- **Exit gate:** `go build ./...` passes and every file above exists.

### T0.2: Schemas and the `adapterkit` contract

- **Depends on:** T0.1
- **Scope:** `schemas/v1alpha1/`, `pkg/adapterkit/`
- **Tasks:** write `v1alpha1` JSON Schemas for `manifest`, `units`, `rules`, `cases`, `reports` and `evidence`, with a mandatory `schema_version` on every record. Add matching Go types in `adapterkit`, documented as versioned and unstable until v1.0. Define the transport-agnostic `LanguageAdapter`, `Replay` and `Transport` interfaces (`InProcess`, `Subprocess`, `HTTP`, `CShared`), the capability declarations (`read_repo`, `run_original`, `network`) and the field semantics (`secret`, `volatile`, `ordered`). Do not add `Redactor` or `ShadowPolicy`.
- **Exit gate:** schema validation tests pass and `adapterkit` compiles with no import from `internal/`.

### T0.3: State machine, atomic I/O, local leases and evidence profiles

- **Depends on:** T0.0, T0.2
- **Scope:** `internal/state/`
- **Tasks:** create the workspace initializer for `.go-anything/` (`manifest.json`, `units/`, `local/`, `rules.json`, `cases/`, `reports/`, `evidence/`) and add `local/` to `.gitignore`. Implement atomic persistence under an OS file lock. Implement the ten-state lifecycle with transition guards that require linked evidence. Record the evidence profile (`replay` or `full`) in each unit's state file and make the guards check what that profile requires. Implement time-bounded leases (`owner`, `expires_at`) in `local/`, following ADR-002; an expired lease returns the unit to Ready.
- **Exit gate:** tests cover atomic writes, concurrent lock safety, lease expiry, rejection of transitions with missing evidence, and both profiles.

### T0.4: Fail-closed redactor and format-preserving substitution

- **Depends on:** T0.2
- **Scope:** `internal/redactor/`
- **Tasks:** build an in-memory pipeline that runs before any payload reaches disk. Add detectors for `Authorization`, `Cookie`, `Set-Cookie`, JWTs, API keys, PEM blocks, emails and card patterns. Substitute values with the same shape (token length and prefix, Luhn-valid cards on a test BIN, emails on a reserved domain). Mark signature and MAC fields `unreplayable` instead of faking them. On an unparseable payload or an unexpected high-confidence secret, drop the case and fail recording unless `--unsafe-allow-unredacted` is passed.
- **Exit gate:** tests show secrets never reach disk, substituted values keep their format and Luhn validity, `unreplayable` fields are flagged, and unparseable payloads fail closed.

### T0.5: Restricted execution sandbox

- **Depends on:** T0.0, T0.1
- **Scope:** `internal/sandbox/`
- **Tasks:** implement the runner chosen in ADR-001: no network by default (`--allow-network` opts in), an environment allowlist, a temporary working copy, and CPU, time and memory limits. Use Linux namespaces or the chosen backend where supported, and print a best-effort isolation warning elsewhere.
- **Exit gate:** integration tests show outbound calls fail by default, unlisted env vars are stripped, writes stay in the temporary copy, and a timeout kills a runaway process.

### T0.6: External adapter host and `doctor`

- **Depends on:** T0.2, T0.3, T0.4
- **Scope:** `internal/adapterhost/`, `cmd/go-anything/`
- **Tasks:** build the subprocess host speaking JSON over `stdio`. Require explicit installation and a pinned SHA-256 checksum, and reject any action the adapter did not declare. Implement `go-anything doctor` to check for committed secrets, `.gitignore` coverage of raw captures and `local/`, and unsafe flags.
- **Exit gate:** a tampered adapter fails its checksum, an undeclared capability is blocked, and `doctor` catches a missing gitignore rule and a leaked secret.

### T0.7: CLI skeleton with `init` and `status`

- **Depends on:** T0.3
- **Scope:** `cmd/go-anything/`
- **Tasks:** add the root command with the global flags `--jobs` and `--profile`. Implement `go-anything init` (creates the workspace with the T0.3 initializer; `--source` is registered now and adapters arrive later) and `go-anything status` (units, states, leases).
- **Exit gate:** `go-anything init` creates the full layout in an empty repository.

**M0 exit gate:** after T0.6 and T0.7, `go-anything init` followed by `go-anything doctor` passes on an empty repository.

## M1: sandboxed record, replay and comparator (MVP, v0.0.x)

### T1.1: Recorder with dual-run volatility detection

- **Depends on:** T0.3, T0.4, T0.5
- **Scope:** `internal/recorder/`
- **Tasks:** ingest cases from doc examples (`curl` lines, CLI commands), existing tests, user-supplied input files and captured traffic. For each case: redact and substitute the input, run the original twice in the sandbox, diff the two outputs to find volatile fields (timestamps, UUIDs, request ids), and store them as wildcards in `.go-anything/rules.json`. Save cases as versioned JSONL in `.go-anything/cases/<unit>.jsonl` with `source_revision`, `runtime`, `captured_at` and `kind` (`happy`, `edge`, `trap`, `fuzz`, `captured`). Record coverage of the original where the runtime supports it.
- **Exit gate:** recording a sample CLI and a sample HTTP server produces redacted JSONL cases, detected wildcard rules and no raw secret on disk.

### T1.2: Go semantic trap generator

- **Depends on:** T1.1
- **Scope:** `internal/recorder/traps/`
- **Tasks:** generate `kind: trap` cases for seven hazards: empty collections (nil slice as `null` vs `[]`), HTML escaping (`<`, `>`, `&`), map key order, numbers (`int64` overflow, `-0`, NaN, float formatting), strings (UTF-16 vs UTF-8, rune vs byte slicing, `\r\n` vs `\n`), time (zones, formats, sub-second precision) and error paths (malformed input, missing fields, non-zero exit codes).
- **Exit gate:** the generator emits valid cases for all seven categories and the recorder accepts them.

### T1.3: Multi-mode comparator

- **Depends on:** T0.2
- **Scope:** `internal/comparator/`
- **Tasks:** implement the modes `exact`, `structural_json`, `numeric_tolerance`, `error_equivalence`, `http_contract` and `filesystem_snapshot`. Apply the wildcard and normalizer rules from `rules.json`. Tell apart missing vs `null`, integer vs float, and ordered vs unordered collections. Save every mismatch as a standalone reproducible case.
- **Exit gate:** table-driven tests cover every mode and every distinction above.

### T1.4: Concurrent, deterministic replay engine

- **Depends on:** T0.5, T1.1, T1.3
- **Scope:** `internal/replay/`
- **Tasks:** replay in parallel with `errgroup` and `SetLimit(--jobs N)`. Give each case its own temp directory and ephemeral port. For stateful HTTP sequences, run the steps serially against a fresh server instance and run independent sequences in parallel. Sort every report and diff by `case_id`.
- **Exit gate:** under `-race` with `--jobs 4`, repeated runs produce byte-identical reports.

### T1.5: CLI and HTTP application adapters

- **Depends on:** T0.7, T1.4
- **Scope:** `adapters/application/cli/`, `adapters/application/http/`, `cmd/go-anything/`
- **Tasks:** wire `go-anything record` and `go-anything replay` for black-box CLI binaries and HTTP servers. Prove the whole M1 flow: record cases from an original app, replay them against a Go reimplementation, and read the diffs, with no language adapter involved.
- **Exit gate:** end-to-end tests pass for a sample CLI and a sample HTTP server.

**M1 exit gate:** replaying the original app against its own recorded cases passes 100% on three runs in a row, with byte-identical reports.

## M2: front door, kill switch and lift (v0.0.x)

### T2.1: CLI front door

- **Depends on:** T1.5
- **Scope:** `adapters/application/cli/frontdoor/`
- **Tasks:** build the Go entry point that parses `argv`, runs ported subcommands natively, and runs unported ones through `os/exec`, passing `argv`, `stdin`, env, `stdout`, `stderr` and the exit code through unchanged.
- **Exit gate:** integration tests show transparent passthrough for unported commands and native execution for ported ones.

### T2.2: HTTP front door

- **Depends on:** T1.5
- **Scope:** `adapters/application/http/frontdoor/`
- **Tasks:** build the front door on `httputil.ReverseProxy` with a per-unit route table. Proxy unported routes to the original on a local port, and make shared auth and CORS middleware wrap both ported and proxied paths.
- **Exit gate:** integration tests show a mix of ported and unported routes served behind one entry point.

### T2.3: Per-unit kill switch and fallback counter

- **Depends on:** T2.1, T2.2
- **Scope:** `internal/frontdoor/`
- **Tasks:** add the `GO_ANYTHING_UNIT_<id>=original` override that sends a unit back to the original without a redeploy. Count every fallback per unit with an atomic counter persisted under `.go-anything/local/`, and show the counts in `go-anything status`.
- **Exit gate:** tests show the override routes traffic to the original and increments the counter.

### T2.4: `verify`, `approve` and `promote` (human CLI)

- **Depends on:** T0.3, T2.3
- **Scope:** `cmd/go-anything/`
- **Tasks:** implement `go-anything verify <unit> --profile replay`, which replays the recorded cases, stores the evidence and moves the unit to Verified. Implement `go-anything approve <unit>`, which records a human sign-off; until T5.1 exists, skipping the Shadow state requires `--skip-shadow`, and the skip is written into the evidence. Implement `go-anything promote <unit>`, which switches routing and tests the kill switch.
- **Exit gate:** a unit moves from In progress to Cutover through the CLI, and each command refuses when its required evidence is missing.

### T2.5: `lift` on a copy

- **Depends on:** T2.4
- **Scope:** `cmd/go-anything/`, `internal/lift/`
- **Tasks:** implement `go-anything lift`. It works on a copy of the project, never in place. On the copy it checks that every fallback counter is zero and that every unit has evidence for the configured profile tied to the current commit, re-runs that profile on the final code, removes the front door bridge and the original, and rebuilds. Only if everything passes does it write the result back and mark the units Completed. When anything fails it changes nothing and lists the blockers.
- **Exit gate:** `lift` refuses with a nonzero fallback count or stale evidence and leaves the project untouched, and it succeeds on both the demo CLI and the demo HTTP server.

**M2 exit gate:** one route and one command move to Go through `record`, `replay`, front door, kill switch and `lift` on both demo apps, and replay stays at 100%.

## M3: Go source adapter (v0.1.0 line)

### T3.1: Go analyzer and dependency wave planner

- **Depends on:** T0.2, T0.3
- **Scope:** `internal/analyzer/`, `internal/planner/`, `adapters/source/golang/`
- **Tasks:** analyze Go repositories with `go/packages`, `go/types` and the `golang.org/x/tools` call graph. Model each package as a unit, compute migration waves from the import graph, and group import cycles so they move together. Analyze units of the same wave in parallel. Write `manifest.json` and a Planned state file per unit.
- **Exit gate:** a multi-package Go repository with a cycle is split into correct waves.

### T3.2: `init --source=go`, `modernize` and the three-tier workspace

- **Depends on:** T3.1, T0.7
- **Scope:** `adapters/source/golang/`, `cmd/go-anything/`
- **Tasks:** implement `go-anything init --source=go` with `go-anything modernize` as an alias. Create `legacy/` (the frozen original through a module `replace` or a separate import path) and `modern/`. Automate Tier 1 changes (`go fix`, modernize analyzers, `golangci-lint --fix`) on a branch checked by the existing tests, and leave Tier 2 (idiomatic) and Tier 3 (architectural) to the agent.
- **Exit gate:** initializing a legacy Go repository creates both folders, runs Tier 1 cleanly and registers the units.

### T3.3: Parity driver

- **Depends on:** T1.1, T1.3, T3.2
- **Scope:** `adapters/source/golang/parity/`
- **Tasks:** generate a driver that calls the public API of `legacy` and `modern` with identical inputs, compares results and errors with `go-cmp`, and catches panics with `recover`.
- **Exit gate:** the driver reports a deliberately injected behavior change as a reproducible JSONL case.

### T3.4: Differential fuzzing and Go-to-Go traps

- **Depends on:** T3.3
- **Scope:** `adapters/source/golang/parity/`
- **Tasks:** add differential fuzzing with `go test -fuzz` that feeds both versions and saves any divergence as a `fuzz` case. Apply the trap mitigations: treat a `go` directive bump in `go.mod` as its own unit with replay before and after, sort map output before comparing, inject `time.Now`, randomness and environment, deep-copy mutable arguments per version, wait for spawned goroutines and require `-race`, and flag `reflect`, struct tags and unexported behavior for explicit characterization cases.
- **Exit gate:** the fuzzer finds an injected bug and each mitigation has a test.

### T3.5: `adapterkit` conformance suite

- **Depends on:** T0.2, T0.6, T3.3
- **Scope:** `pkg/adapterkit/conformance/`
- **Tasks:** write a public, importable test suite that any adapter must pass over each transport it supports. It checks detection, analysis, unit discovery, recording, replay results that match across transports, honoring of declared capabilities, declared field semantics and declared coverage limits. The Go adapter is the first to pass it.
- **Exit gate:** the Go adapter passes, and a deliberately broken fixture adapter fails with a clear message for each violation.

### T3.6: Go facade, library shadow helper and batch fan-out

- **Depends on:** T3.3
- **Scope:** `adapters/source/golang/facade/`, `adapters/application/library/`
- **Tasks:** generate facade packages with modes `old`, `shadow` and `new`, and a registry that switches HTTP handlers per route. Implement the generic `Shadow[T](old, new)` with a bounded worker pool and a cloned input; the shared shadow policy plugs in later through T5.1. Implement batch fan-out: items enter once, the function runs over all of them on every core, and results return in input order.
- **Exit gate:** tests pass under `-race` for each mode, overflow is dropped and counted, and fan-out preserves order.

### T3.7: Go dogfooding

- **Depends on:** T3.4, T3.5, T3.6
- **Scope:** the target service repository, `docs/case-studies/`
- **Tasks:** run the adapter on a real legacy Go service, port its first wave of units through the facade, and verify them under the `replay` profile.
- **Exit gate:** the ported units reach Verified with zero mismatches and no race warning.

**M3 exit gate:** a real Go unit is ported with zero mismatches.

## M4: quality gate, scorecard and MCP server (v0.1.0 line)

### T4.1: Gate runner with toolchain checks

- **Depends on:** T0.3
- **Scope:** `internal/gate/`
- **Tasks:** run `go build ./...`, `gofmt -s`, `go vet`, `staticcheck`, `golangci-lint` (with `errcheck` and `forcetypeassert`), `go test -race ./...` and `govulncheck`. Write one report per check in a common format.
- **Exit gate:** the runner fails on an injected race, an unhandled error and a vet issue, and passes on clean code.

### T4.2: Custom analyzers

- **Depends on:** T4.1
- **Scope:** `internal/gate/analyzers/`
- **Tasks:** write `go/analysis` analyzers for `panic` inside HTTP handlers and for goroutines started without a `context` or a worker pool, and register them in the runner.
- **Exit gate:** each analyzer flags its fixture and stays silent on clean code.

### T4.3: Behavior and API checks, commit-tied evidence, `full` profile

- **Depends on:** T4.1, T1.4, T3.3
- **Scope:** `internal/gate/`, `internal/state/`
- **Tasks:** add full replay with zero mismatches, a check that every error the original returns has a recorded failing case, and `apidiff` or `gorelease` for Go to Go (contract checks elsewhere). Store every report in `.go-anything/evidence/` stamped with the current commit SHA. Define the `full` profile from these checks, make it the default, and teach the state guards to accept it.
- **Exit gate:** the gate fails on an API break and on a missing error-path case, and passes and writes commit-tied evidence when clean.

### T4.4: Benchmarks and parity scorecard

- **Depends on:** T4.3
- **Scope:** `internal/bench/`, `internal/gate/scorecard/`
- **Tasks:** capture latency and allocations (informational, never blocking). Generate `.go-anything/reports/scorecard.md` with units, cases by `kind`, mismatches, coverage of the original, the race detector result and the `unreplayable` fields. Never print `Mismatches: 0` without the case breakdown and coverage beside it.
- **Exit gate:** the scorecard matches the v3 format and pastes cleanly into a pull request.

### T4.5: MCP payload sanitizer

- **Depends on:** T0.2
- **Scope:** `internal/mcp/sanitize/`
- **Tasks:** truncate long strings, strip control characters, and wrap recorded data in explicit delimiters with a warning that it is untrusted. Document that this lowers prompt-injection risk and does not remove it.
- **Exit gate:** tests show control characters removed, long strings truncated, and known injection strings kept inside the delimiters.

### T4.6: `go-anything mcp` server

- **Depends on:** T0.3, T1.1, T1.4, T4.3, T4.4, T4.5
- **Scope:** `internal/mcp/`, `cmd/go-anything/`
- **Tasks:** serve MCP over `stdio` only, with no network listener and no telemetry. Expose exactly eight tools: `status`, `claim_unit`, `inspect_unit`, `record_cases`, `verify_diff`, `run_gate`, `mark_verified` and `handoff` (writes `HANDOFF.md`). Pass every payload-returning tool through the sanitizer. Enforce the state machine on the server: `mark_verified` rejects the move unless evidence tied to `HEAD` exists for the unit's profile. Do not expose `approve`, `promote`, `lift` or routing.
- **Exit gate:** an end-to-end MCP test drives claim, inspect, record, `verify_diff`, `run_gate`, `mark_verified` and handoff; unauthorized transitions fail; and a test asserts the tool list is exactly these eight.

### T4.7: Agent templates and skills

- **Depends on:** T4.6
- **Scope:** `templates/`, `skills/`, `cmd/go-anything/`
- **Tasks:** build a pluggable template loader and write default instruction files for `CLAUDE.md`, `.cursorrules`, Codex and Gemini that enforce the agent loop and the escalate-to-human rules.
- **Exit gate:** the template command renders every agent file cleanly.

### T4.8: Agent acceptance run

- **Depends on:** T4.6, T4.7, T3.7
- **Scope:** `docs/case-studies/`
- **Tasks:** have a real coding agent port one unit of the T3.7 service through the MCP server with no manual help, and re-verify the T3.7 units under the `full` profile.
- **Exit gate:** the agent's unit reaches Verified under `full`, and no state file was edited by hand.

**M4 exit gate:** an agent ports a unit unaided.

## M5: shadow mode, capture and the v0.1.0 release

### T5.1: Shadow mode engine

- **Depends on:** T0.4, T2.2, T3.6
- **Scope:** `internal/shadow/`
- **Tasks:** support `GO_ANYTHING_MODE=old|shadow|new`, globally and per unit. Shadow only idempotent requests (`GET`, `HEAD`) by default, through a bounded worker pool that drops and counts overflow instead of queueing without limit, with a configurable sample rate. Pass every shadow diff through `internal/redactor` in memory before appending it to `.go-anything/reports/shadow_mismatches.jsonl`. Gate non-idempotent shadowing behind `--allow-side-effects` and print the startup warning exactly as written in the v3 plan. Make the library `Shadow[T]` from T3.6 use this policy.
- **Exit gate:** load tests show bounded memory under overflow, no unredacted secret in `shadow_mismatches.jsonl`, and `POST`, `PUT` and `DELETE` blocked by default.

### T5.2: Production capture ingestion

- **Depends on:** T0.4, T1.1
- **Scope:** `internal/recorder/capture/`
- **Tasks:** add capture sources for gateway or sidecar traffic mirroring, broker consumers, traces, and read-only observability tools such as `platform-mcp` that supply decoded traffic. Send everything through fail-closed redaction before it is stored, and give observability sources no control over routing.
- **Exit gate:** captured traffic becomes redacted `kind: captured` cases with no credential leaked.

### T5.3: Release pipeline

- **Depends on:** T0.1
- **Scope:** `.goreleaser.yaml`, `.github/workflows/release.yml`
- **Tasks:** configure `goreleaser` in GitHub Actions and sign binaries and checksums with `cosign`.
- **Exit gate:** a snapshot build produces signed artifacts that verify with `cosign`.

### T5.4: Go case study and v0.1.0

- **Depends on:** T4.8, T5.1, T5.2, T5.3
- **Scope:** `docs/case-studies/`
- **Tasks:** publish the case study of the T3.7 service migration with its commits, recorded cases, scorecard and lessons learned, then tag and release `v0.1.0`.
- **Exit gate:** the signed release verifies with `cosign` and the case study links a scorecard under the `full` profile.

**M5 exit gate:** the shadow log is clean on idempotent traffic and the Go case study is published.

## M6: Python adapter and foreign bridges (v0.2.0)

### T6.1: Python source adapter over subprocess and HTTP

- **Depends on:** T0.6, T3.5
- **Scope:** `adapters/source/python/`
- **Tasks:** implement Python parsing, dependency discovery, test discovery and field semantics, plus a `cProfile`-based hot-path report. Implement the `Subprocess` and `HTTP` transports first and defer `c-shared`. Declare the bridge limits: ABI, memory ownership, error mapping and concurrency.
- **Exit gate:** the adapter passes the `adapterkit` conformance suite over both transports.

### T6.2: Foreign library and worker application adapters

- **Depends on:** T6.1
- **Scope:** `adapters/application/library/`, `adapters/application/worker/`
- **Tasks:** route a foreign library through a subprocess or HTTP wrapper. Support worker and service units as Go sidecars over HTTP, gRPC, a Unix socket, `stdio` or a message broker.
- **Exit gate:** integration tests move a Python library and a background worker over one unit at a time.

### T6.3: Python dogfooding and v0.2.0

- **Depends on:** T6.2
- **Scope:** `docs/case-studies/`
- **Tasks:** port a real Python CLI and a FastAPI service to Go, publish the case study with scorecards under the `full` profile, and tag `v0.2.0`.
- **Exit gate:** both projects reach Completed and the release verifies with `cosign`.

### T6.4: `c-shared` evaluation (experimental)

- **Depends on:** T6.1
- **Scope:** `adapters/source/python/cshared/`
- **Tasks:** build test suites for CPython GIL interaction, `fork()` and signal handling against a `c-shared` Go library. Ship `c-shared` only if all three suites pass; otherwise document the limits and keep subprocess and HTTP as the only transports.
- **Exit gate:** either all three suites pass and the transport is marked experimental, or a written decision records why it stays out.

**M6 exit gate:** a Python CLI and API are ported with the same suite.

## M7: freeze, TypeScript adapter and v1.0.0

### T7.1: TypeScript source adapter

- **Depends on:** T3.5, T6.2
- **Scope:** `adapters/source/typescript/`
- **Tasks:** implement TypeScript and Node dependency analysis, test discovery and a replay driver. Support subprocess and HTTP transports first, with an optional `koffi` or N-API bridge for foreign libraries.
- **Exit gate:** the adapter passes the `adapterkit` conformance suite on a sample Node CLI and HTTP service.

### T7.2: Freeze `adapterkit` and schemas, add `migrate`

- **Depends on:** T6.3, T7.1
- **Scope:** `pkg/adapterkit/`, `schemas/v1/`, `cmd/go-anything/`
- **Tasks:** promote the schemas from `v1alpha1` to `v1` and freeze the public API of `adapterkit`. Implement `go-anything migrate`, which upgrades a `.go-anything/` workspace from `v1alpha1` to `v1` and is safe to run twice.
- **Exit gate:** `apidiff` confirms the `v1` surface is stable, and migration tests upgrade real `v1alpha1` workspaces with no data loss.

### T7.3: `GOVERNANCE.md` and quickstart

- **Depends on:** T5.4
- **Scope:** `GOVERNANCE.md`, `README.md`
- **Tasks:** write `GOVERNANCE.md` covering maintainer roles, the RFC process and adapter certification through the conformance suite. Finish the five-minute vertical-slice quickstart in `README.md`.
- **Exit gate:** a new contributor can follow the quickstart from a clean machine to a replayed case without help.

### T7.4: Multi-unit open-source dogfooding and v1.0.0

- **Depends on:** T7.2, T7.3
- **Scope:** `docs/case-studies/`
- **Tasks:** lift a multi-unit open-source project, publish the final case study with scorecards under the `full` profile, and tag and sign `v1.0.0`.
- **Exit gate:** the project reaches Completed with zero fallback traffic and the release verifies with `cosign`.

**M7 exit gate:** an open-source project is lifted with fallback at zero.

## Changes from the previous task plan

| Previous plan | This plan | Why |
| --- | --- | --- |
| M2 `approve`, `promote` and `lift` ran before any gate existed | Two evidence profiles, `replay` and `full`; `verify` added in T2.4 | The state guards demanded gate evidence that M4 had not built yet |
| `lift` removed the bridge in place | T2.5 `lift` works on a copy, refuses on fallback or stale evidence, and leaves the project untouched on failure | The v3 plan requires it, and `lift` must never lose the original |
| No task built the conformance suite | T3.5 builds it; the Go adapter passes first | T6.1 and T7.1 required a suite nobody wrote |
| Sanitizer came after the MCP server | T4.5 sanitizer comes before T4.6 server | The server would return raw payloads in between |
| T0.3 did not depend on T0.0 | T0.3 depends on T0.0 and T0.2 | ADR-002 decides the lease design |
| T0.1 waited for T0.0 | T0.1 starts immediately | Scaffolding needs no decision |
| No `init` command | T0.7 CLI skeleton with `init` and `status` | The M0 gate runs `init` |
| M4 gate was only protocol tests | T4.8 runs a real agent through the MCP server | The gate is that an agent ports a unit unaided |
| M1 gate had no repeat check | M1 exit gate requires three identical runs | Matches the v3 gate |
| Batch fan-out and parallel wave analysis missing | Added in T3.6 and T3.1 | They are in the v3 concurrency model |
| Large tasks T3.3 and T4.1 | Split into T3.3 and T3.4, and T4.1 to T4.4 | Smaller tasks are easier for an agent to finish and check |
| No guidance on parallel work | Parallel lanes table | Parallelism should follow dependencies |
| No test style rule | Rules 7 to 10: test style, public contract, evidence profiles, scope | Keeps agents consistent |

**Matching v3 edits:** the v3 roadmap now places the quality gate, scorecard and MCP server in M4 and shadow mode, capture and the release in M5, and the unit lifecycle section gains a note on evidence profiles.
