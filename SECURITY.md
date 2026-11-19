# Security Policy

## Threat Model

`go-anything` operates at the boundary between legacy applications and modern Go implementations, frequently orchestrated by autonomous AI coding assistants. Because migration involves executing tests, capturing runtime payloads, and inspecting source trees, the security model is built around defense-in-depth and fail-closed safety defaults:

1. **In-Memory Redaction:** Sensitive credentials (Authorization headers, session cookies, API tokens, JWTs, PEM private keys, cardholder data) are detected and substituted in memory prior to persistence.
2. **Format-Preserving Substitution:** Redacted values preserve data shapes and syntactic validity (e.g., test BINs with Luhn validity, synthetic emails on reserved domains) to avoid breaking test logic while ensuring real secrets never hit disk.
3. **No-Network Sandboxing:** Application execution occurs within restricted sandbox boundaries with network isolation enabled by default (`--allow-network` to opt in), environment variable allowlists, ephemeral directories, and timeout enforcement.
4. **Local-Only & Zero-Telemetry:** All operations, state files, and evidence remain strictly local. No usage telemetry, metrics, or logs are transmitted across the network.
5. **Human-Only Privileged Actions:** Destructive and routing-altering operations (`approve`, `promote`, `lift`, cutover) are strictly restricted to interactive CLI commands and are never exposed via the MCP protocol.
6. **Prompt-Injection Mitigation:** Payloads retrieved via MCP tools (`inspect_unit`, `verify_diff`) are truncated, stripped of terminal control characters, and enclosed in explicit boundary delimiters.

---

## ⚠️ Trust-the-Repo Warning

> [!WARNING]
> **Only execute `go-anything` in repositories and environments you trust.**
> 
> Recording behavioral fixtures and analyzing legacy applications may execute local build scripts, tests, and binaries associated with the target codebase. While `go-anything` enforces process timeouts, environment allowlists, and sandboxing restrictions, it is not an arbitrary code sandbox against malicious repository contents. Never run `go-anything` on untrusted or uninspected third-party repositories without dedicated containerization.

---

## Reporting a Vulnerability

We take the security of `go-anything` seriously. If you discover a vulnerability or potential security concern:

1. **Do not disclose the issue publicly** via GitHub issues, discussions, or social media.
2. Send an email with details and reproduction steps to the maintainers or open a private security advisory through GitHub Security Advisories.
3. Include:
   - A description of the issue and potential impact
   - Minimal steps or fixtures to reproduce
   - Any proposed remediations or patches
4. You will receive an acknowledgment within 48 hours, followed by updates on triage and mitigation timelines.
