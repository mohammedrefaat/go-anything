# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- **Keyless Sigstore Cosign Verification**: Documented exact certificate identity regex (`^https://github\\.com/mohammedrefat/go-anything/\\.github/workflows/release\\.yml@refs/tags/v.*$`) and OIDC issuer (`https://token.actions.githubusercontent.com`) for all release artifacts.

## [1.0.0] - 2026-11-20

### Added
- Initial release of `go-anything`.
- Strangler-fig migration orchestrator supporting Go, Python, and TypeScript source adapters.
- 10-state unit lifecycle engine with durable file locking.
- In-memory fail-closed secret redaction and dual-run volatility detection.
- Multi-mode case comparator and concurrent deterministic replay engine.
- MCP server interface running over stdio.
