# Security Policy

## Threat Model

`go-anything` operates at the boundary between legacy applications and modern Go implementations, frequently orchestrated by autonomous AI coding assistants. Because migration involves executing tests, capturing runtime payloads, and inspecting source trees, the security model is built around defense-in-depth and fail-closed safety defaults.

### Core Security Controls

1. **In-Memory Redaction (`internal/redactor`)**
   Sensitive credentials (Authorization headers, session cookies, API tokens, JWTs, PEM private keys, cardholder data, emails) are detected and substituted in memory prior to persistence. If a payload cannot be parsed or a high-confidence secret appears in an unexpected field, the case is **dropped** and recording fails unless `--unsafe-allow-unredacted` is explicitly passed. This is a core-owned control — no external adapter may disable or bypass fail-closed redaction.

2. **Format-Preserving Substitution**
   Redacted values preserve data shapes and syntactic validity (e.g., test BINs with Luhn validity, synthetic emails on reserved domains, tokens with matching length and prefix) to avoid breaking test logic while ensuring real secrets never hit disk. Fields the original validates against real data (signatures, MACs) are marked `unreplayable` and excluded, not faked.

3. **No-Network Sandboxing (`internal/sandbox`)**
   Application execution occurs within restricted sandbox boundaries:
   - **Linux (Tier 1):** Native kernel namespaces (`CLONE_NEWNET`, `CLONE_NEWNS`, `CLONE_NEWPID`) with `bubblewrap` (`bwrap`) fallback where unprivileged user namespaces are restricted. Full kernel-enforced network and filesystem isolation.
   - **macOS & Windows (Tier 2):** Best-effort process runner using an environment allowlist, temporary directory isolation, timeout/process-group termination, and proxy variables directed to invalid sinks. Prints an explicit warning: `[WARN] Sandbox running in best-effort mode`.
   - Network isolation is on by default (`--allow-network` to opt in), with environment variable allowlists, ephemeral directories, and CPU/time/memory limits enforced on all platforms.
   - See [ADR-001](Docs/decisions/adr-001-sandbox-backend.md) for the full decision record.

4. **Local-Only & Zero-Telemetry**
   All operations, state files, and evidence remain strictly local. No usage telemetry, metrics, or logs are transmitted across the network. `go-anything mcp` speaks `stdio` only — no network listener is opened.

5. **Human-Only Privileged Actions**
   Destructive and routing-altering operations (`approve`, `promote`, `lift`, cutover) are strictly restricted to interactive CLI commands and are **never** exposed via the MCP protocol. The MCP server enforces the state machine: an agent cannot skip a gate or promote unverified code.

6. **Prompt-Injection Mitigation**
   Payloads retrieved via MCP tools (`inspect_unit`, `verify_diff`) are truncated, stripped of terminal control characters, and enclosed in explicit boundary delimiters with a warning that the data is untrusted. This lowers risk without removing it — the primary control is that no MCP tool can promote, lift, or change routing.

7. **External Adapter Threat Model**
   External adapters are executables running with the user's privileges. They are installed explicitly (never downloaded automatically), checked against a pinned SHA-256 checksum, and limited to the capabilities they declare (`read_repo`, `run_original`, `network`). The core refuses any action an adapter did not declare. `pkg/adapterkit` never exports `Redactor` or `ShadowPolicy` — these remain internal core concerns.

8. **Atomic State & Local Leases**
   Unit state files are written atomically (temp file → `fsync` → rename) under an OS file lock. Agent leases are time-bounded and stored in the gitignored `.go-anything/local/` directory; an expired lease returns the unit to Ready, ensuring a crashed agent never permanently blocks a migration. See [ADR-002](Docs/decisions/adr-002-cross-machine-claims.md).

---

## ⚠️ Trust-the-Repo Warning

> [!WARNING]
> **Only execute `go-anything` in repositories and environments you trust.**
>
> Recording behavioral fixtures, differential fuzzing, and analyzing legacy applications may execute local build scripts, tests, and binaries associated with the target codebase. While `go-anything` enforces process timeouts, environment allowlists, and sandboxing restrictions, it is **not** an arbitrary code sandbox against malicious repository contents. Never run `go-anything` on untrusted or uninspected third-party repositories without dedicated containerization.
>
> On macOS and Windows, kernel-level network isolation is not available without virtualization. Ensure the repository is trusted, or use Linux / CI for sandbox-critical workflows.

---

## Security Checklist (`go-anything doctor`)

The built-in `go-anything doctor` command checks for common security misconfigurations:

- Committed secrets in the workspace
- `.gitignore` coverage of `.go-anything/local/` and raw captures
- Unsafe flags (`--unsafe-allow-unredacted`, `--allow-side-effects`)
- Adapter checksum validity

Run `go-anything doctor` after `go-anything init` and before publishing any pull request.

---

## MCP Server Security Boundary

The MCP server (`go-anything mcp`) exposes exactly eight tools:

| Tool | Access | Purpose |
| :--- | :--- | :--- |
| `status` | Agent | Progress, next unit, active leases, blockers |
| `claim_unit` | Agent | Take a unit with a time-bounded local lease |
| `inspect_unit` | Agent | View original source and sanitized sample cases |
| `record_cases` | Agent | Run the original in the sandbox, apply redaction, save JSONL |
| `verify_diff` | Agent | Build, replay all cases, return PASS or top sanitized mismatches |
| `run_gate` | Agent | Run the quality gate, write scorecard, store evidence |
| `mark_verified` | Agent | Move unit to Verified (requires commit-tied evidence) |
| `handoff` | Agent | Write `HANDOFF.md` with state and next steps |

**Not exposed via MCP:** `approve`, `promote`, `lift`, routing changes, shadow mode toggle, kill-switch override. These are CLI-only, human-initiated commands.

---

## Shadow Mode Security

Shadow mode (`GO_ANYTHING_MODE=shadow`) runs the Go handler beside the original on real requests. Security controls:

- Only idempotent requests (`GET`, `HEAD`) are shadowed by default
- Non-idempotent shadowing requires `--allow-side-effects` with an explicit warning
- Shadow diffs are redacted in memory before being appended to `shadow_mismatches.jsonl`
- A bounded worker pool prevents unbounded resource consumption; overflow is dropped and counted

---

## Reporting a Vulnerability

We take the security of `go-anything` seriously. If you discover a vulnerability or potential security concern:

1. **Do not disclose the issue publicly** via GitHub issues, discussions, or social media.
2. Send an email with details and reproduction steps to the maintainers or open a private security advisory through [GitHub Security Advisories](https://github.com/mohammedrefaat/go-anything/security/advisories).
3. Include:
   - A description of the issue and potential impact
   - Minimal steps or fixtures to reproduce
   - Any proposed remediations or patches
4. You will receive an acknowledgment within **48 hours**, followed by updates on triage and mitigation timelines.
