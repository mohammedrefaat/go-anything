# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.1] - 2026-11-26

Patch release addressing Part A findings (F1–F4, F7–F11) from `Docs/GO_ANYTHING_FIX_AND_SECURITY_PLAN.md`: canonical owner/module identity, code-derived documentation, CI supply-chain & lint enforcement, expanded redactor coverage, multi-language `inspect_unit`, `staticcheck` in `full` gate profile with fail-closed v2 evidence, and sandboxed sidecar builds.

### Changed
- **Canonical Module & Owner Identity (A1 / F1)**: Standardized module path and repository owner to `mohammedrefaat` (`github.com/mohammedrefaat/go-anything`) across `go.mod`, all imports, packaging, Cosign identity regexes, and docs; recorded ADR-007 and added `TestCanonicalOwnerIdentity` regression guard.
- **Code-Derived Documentation (A2 / F2, F3)**: Extended `tools/genstatus` to generate the 14-tool MCP table and human-only controls into `STATUS.md`, `SECURITY.md`, `CLAUDE.md`, `AGENTS.md`, and `templates/mcp_tools.md`, with `--check` and `TestMCPToolNamesMatchDocs` drift guards; updated `CLAUDE.md`, `AGENTS.md`, `README.md`, and templates to reflect Go 1.27+, all 32 wired commands, and 14 MCP tools.
- **Gate Profiles & Evidence Schema v2 (A6 / F7)**: Added `staticcheck` to `FullProfileChecks()`, introduced `schemas/v2/evidence.schema.json` with `profile_version` (`2.0.1`) and `profile_checks`, and enforced fail-closed validation in `mark_verified` (`internal/state/machine.go`). Recorded ADR-008. **Upgrade note:** Units already `Verified` under the older `full` profile must re-run `gate` before `promote`.
- **Multi-Language `inspect_unit` (A5 / F9)**: Updated `inspect_unit` to discover source files using registered adapter manifests across Go, Python, TypeScript, and JavaScript, capped at 50 files without leaking file contents, wrapped by the MCP sanitizer.
- **Sandboxed Sidecar Builds (A7 / F11)**: Routed hot-path `go build` and `go vet` invocations through `internal/sandbox` with network disabled, `GOFLAGS=-mod=readonly`, `GOTOOLCHAIN=local`, and `GOPROXY=off`, preserving tier-2 platform warnings and documenting offline build prerequisites in `Docs/hotpath.md`.

### Security
- **CI Enforcement & Supply-Chain Hardening (A3 / F4, F10)**: Restored `.github/workflows/ci.yml` with `staticcheck ./...`, `govulncheck ./...`, `go mod verify`, and dogfood `gate`; pinned all third-party GitHub Actions across workflows by 40-char commit SHA; scoped `release.yml` `id-token: write` and `contents: write` permissions strictly to the signing/release job.
- **Expanded Redactor Coverage (A4 / F8)**: Added detectors for Google API keys (`AIza...`), windowed AWS secret access keys, URL credentials, generic secret assignments, SendGrid, npm, and Twilio tokens; tightened `sk-...` matching; exposed pure `redactor.Detect(string) []Finding` for reuse.

## [2.0.0] - 2026-11-24

Major release introducing hot-path strangler sidecars, end-to-end porting pipeline, differential fuzzing & benchmarking, expanded MCP agent surface, multi-tool configuration, real-world external ports, and the JavaScript source adapter.

### Added
- **Phase 1: Differential Testing & Effects (R1–R5)**:
  - `fuzz`: Differential fuzzing command with JSON schema `schemas/v2/fuzz.report.schema.json`.
  - `bench`: Comparative latency and memory allocation benchmark command with `schemas/v2/bench.report.schema.json`.
  - `profile`: Profiling and hot-function candidate ranking command with `schemas/v2/profile.report.schema.json`.
  - `record --from-tests`: Test harness interception capturing characterization cases from existing Go, Python, and TypeScript/Jest tests.
  - `record --effects`: Captures outbound network and side-effects with multiset matching (`ModeEffectMultiset`).
- **Phase 2: Hot-Path Strangler Architecture (B1–B5)**:
  - ADR-005: Architecture decision for stdio JSON-RPC strangler sidecar protocol.
  - `bridge`: Generates Go sidecar bridges, IPC servers, and language wrappers.
  - Python & TypeScript wrappers: Resilient stdio process clients with automatic crash recovery, process cleanup, and fallback to original implementations.
  - `go`: Hot-path guided end-to-end orchestrator pipeline (`profile → bridge → go → gate → shadow → human approve`).
  - Companion commands: `enable`, `disable`, `refresh`, `clean`, `report`, `map` with schemas `schemas/v2/report.output.schema.json` and `schemas/v2/map.output.schema.json`.
- **Phase 3: Agent Orchestration Surface (M1–M3)**:
  - Expanded MCP server: 14 tools (`status`, `claim_unit`, `inspect_unit`, `record_cases`, `verify_diff`, `run_gate`, `mark_verified`, `handoff`, `analyze`, `profile`, `bridge`, `hotpath`, `fuzz_bench`, `shadow_status`).
  - Enforced Rule R-1: Automated safety guard `TestMCPDoesNotExposeHumanOnlyTools` preventing human-only verbs (`approve`, `promote`, `lift`, routing changes) from ever appearing in MCP.
  - Real `setup` command: Non-destructive multi-tool configuration for Claude Code, Codex, Cursor, Gemini, and OpenCode with backup preservation (`.bak`), dry-run diffs, and uninstallation.
  - Agent guide templates: Rich instruction generation (`CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `GEMINI.md`) with lifecycle explanations, scorecard reading, claim process, and golden tests.
- **Phase 4: Real-World Ports & Reach (P1–P5)**:
  - External Port #1 (Node.js REST API): Pinned commit `d651fa15cf1b4f2c0ce9e16d4ea244391e6005d5`, 100% case replay match, 1.38x p50 latency speedup, +41.7% throughput (`demo/real-world/node-rest-api`).
  - External Port #2 (Python Worker): Pinned commit `9f1d82acbf267bb894d01b63ef293b6e8281a1a7`, 100% case replay match, 5.69x p50 latency speedup, +472% throughput (`demo/real-world/python-worker`).
  - Dedicated JavaScript source adapter (`adapters/source/javascript`): Implements `adapterkit.LanguageAdapter` and passes full conformance suite (`conformance.RunSuite`).
  - Comprehensive documentation suite: `Docs/how-it-works.md`, `Docs/faq.md`, `Docs/pitfalls.md`, `Docs/packaging.md`, `Docs/privacy.md`, `Docs/real-world.md`, `Docs/whole-apps.md`, and static documentation site (`Docs/index.html`).
  - Distribution channels: GoReleaser Homebrew tap, Scoop bucket, and npm wrapper package (`packaging/npm`).

### Security
- **Rule R-1 Enforced**: Human-only controls (`approve`, `promote`, `lift`, routing cutovers) are strictly CLI-only.
- **Fail-Closed Core Redaction**: Core-owned in-memory redaction before any disk write.
- **Frozen Contract**: `schemas/v1/` contract remains completely frozen; all new contracts reside in `schemas/v2/`.

## [1.0.1] - 2026-11-23

commands analyze, diff, gate, shadow, cutover are now wired

### Added
- **CLI Commands (Task H1)**: Fully wired handlers for all 5 previously stubbed commands in `cmd/go-anything/`:
  - `analyze`: Static code analysis, dependency wave extraction, and complexity/risk indexing (`--source`, `--json`, `--output`).
  - `diff`: Differential case comparison against target binary or service (`--unit`, `--target`, `--mode`, `--jobs`, `--case`, `--json`).
  - `gate`: Quality gate runner with evidence recording (`--unit`, `--profile`, `--allow-missing-tools`, `--scorecard`, `--json`).
  - `shadow`: Traffic shadow engine controller with subcommands `start`, `status`, `stop` (`--unit`, `--port`, `--sample-rate`, `--json`).
  - `cutover`: Strangler cutover routing and kill switch management with subcommands `plan`, `status`, `kill` (`--unit`, `--json`).
- **Standardized CLI Exit Codes (Task H1)**: Standard exit code semantics documented and enforced across all subcommands: `0` (success), `1` (check/diff failure), `2` (usage error), `3` (internal error).
- **CI Stubs Guard (Task H2)**: Added automated test `TestAllRegisteredCommandsHaveHandlers` in `cmd/go-anything/stubs_guard_test.go` and status verification script `tools/genstatus/main.go` generating `STATUS.md`, integrated into GitHub Actions CI workflow (`.github/workflows/ci.yml`).
- **Documentation Verification (Task H3 & H4)**:
  - Added `Docs/install.md` with step-by-step installation guides for Linux, macOS, and Windows.
  - Added prebuilt binary verification via SHA-256 (`checksums.txt`) and keyless Sigstore Cosign verification (`checksums.txt.sigstore.json`).
  - Added programmatic link and anchor verification tests (`TestREADME_LinksAndAnchors`, `TestInstallDoc_LinksAndAnchors`) in `cmd/go-anything/readme_links_test.go`.
- **User Guide Updates (Task H5)**: Expanded `Docs/USER_GUIDE.md` Section 4 with comprehensive documentation, flag specifications, example commands, exit codes, and Rule R-1 enforcement for all 5 wired commands.

### Changed
- **CLI Dispatcher (`cmd/go-anything/main.go`)**: Replaced all stub messages ("will be wired in milestone M1-M4") with direct dispatching to concrete handler implementations in `internal/app/`.
- **Discovery Documentation (`Docs/V2_DISCOVERY.md`)**: Updated CLI command status tables from `STUBBED` to `WIRED`.
- **README Milestone Roadmap (`README.md`)**: Updated Milestone M0–M7 table linking directly to proving CLI commands and automated tests, added "Known Limitations" section, and updated Quickstart with installation verification.
- **Multilingual Readmes**: Appended canonical English versioning notice to all 10 localized README files (`README.<lang>.md`).

### Fixed
- **Gate behavioral check wiring**: Correctly wired behavioral checks (`replay`, `error_paths`, `api_compat`) into `DefaultGatePort.RunGate`.
- **Nested Unit Paths**: Ensured `os.MkdirAll` recursively creates nested unit directories in `.go-anything/cases/` and `.go-anything/local/`.
- **Flag shorthand collision**: Resolved shorthand conflict on `gate -s` (`--scorecard` bool vs string).

### Security
- **Rule R-1 Strict Boundary**: Enforced that `cutover` does not import `internal/lift` or perform code lift operations. Strangler retirement remains human-governed via `go-anything lift`.
- **Fail-Closed Redaction**: Ensured redaction remains core-owned and applied before writing fixtures to disk.
- **Keyless Sigstore Cosign Verification**: Documented exact certificate identity regex (`^https://github\.com/mohammedrefaat/go-anything/\.github/workflows/release\.yml@refs/tags/v.*$`) and OIDC issuer (`https://token.actions.githubusercontent.com`) for all release artifacts.

## [1.0.0] - 2026-11-20

### Added
- Initial release of `go-anything`.
- Strangler-fig migration orchestrator supporting Go, Python, and TypeScript source adapters.
- 10-state unit lifecycle engine with durable file locking.
- In-memory fail-closed secret redaction and dual-run volatility detection.
- Multi-mode case comparator and concurrent deterministic replay engine.
- MCP server interface running over stdio.
