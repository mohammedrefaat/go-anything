# Privacy & Security Architecture

`go-anything` is designed from the ground up for strict privacy, local-only execution, and fail-closed security.

---

## Key Security Guarantees

### 1. Zero Telemetry & Local-Only Execution
- `go-anything` contains **no analytics, no telemetry, and no remote phoning home**.
- All data stays on your local developer machine or private CI environment.
- The built-in MCP server (`go-anything mcp`) communicates exclusively over standard input/output (`stdio`) with zero network listeners.

### 2. Core-Owned In-Memory Redaction (Rule R-4)
- Sensitive secrets are intercepted and redacted in memory **before** any disk write to `.go-anything/cases/`.
- Standard patterns masked:
  - Authorization headers (`Bearer` tokens, `Basic` auth, JWTs, API keys).
  - Provider and package tokens (Google `AIza...`, GitHub, Slack, Stripe, SendGrid, npm, Twilio, AWS access key IDs & windowed AWS secret access keys).
  - Embedded URL credentials (`scheme://user:pass@host`) and generic secret assignments (`password`, `secret`, `token`, `api_key`, `private_key`, `client_secret`).
  - Private keys (RSA, SSH, PEM blocks), session cookies, emails, and cardholder numbers.
- Format-preserving substitution ensures token lengths and structure are maintained for deterministic replay without exposing plain-text values.
- Core redaction is mandatory and cannot be disabled or bypassed by third-party language adapters.

### 3. Sandboxed Execution by Default (Rule R-3)
- When capturing baseline behavior or recording cases, `go-anything` runs the original application in an isolated sandbox.
- **No-network default:** External network calls are blocked to prevent data exfiltration. If an application requires loopback network communication, `--allow-network` must be explicitly passed.
- **Environment allowlist:** Only a strictly filtered set of environment variables (`PATH`, `TMPDIR`, minimal locale) are propagated to the sandbox child process.

### 4. File Leases & Local State (Rule R-1)
- Unit state files under `.go-anything/units/` are durable and version-controlled.
- Ephemeral state (claims, active leases, OS file locks) is written strictly to `.go-anything/local/`.
- `.go-anything/local/` is **mandatory gitignored**. `go-anything doctor` automatically validates your `.gitignore` to prevent leaking lease files or captures into version control.

### 5. Sanitized MCP Payloads
- All responses returned to AI agents via the MCP server are sanitized:
  - Strings are truncated to prevent context flooding.
  - Terminal control characters and ANSI escape sequences are stripped.
  - Untrusted output is clearly delimited with data boundary markers to mitigate prompt-injection attacks.
