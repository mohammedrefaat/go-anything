# Frequently Asked Questions (FAQ)

### Is go-anything an AI code translator?
**No.** `go-anything` does not perform automatic AST translation or transpile legacy code into pseudo-Go. Automatic translators produce brittle, unidiomatic code that is difficult to maintain. With `go-anything`, you or an AI agent write native, idiomatic Go, while `go-anything` organizes the migration waves, records behavioral evidence from the original, deterministically replays cases, and verifies equivalence.

### What languages can go-anything migrate?
As of v2.0, `go-anything` supports:
- **Go modernization:** Upgrading legacy Go (e.g. Go 1.12) to modern Go 1.27+ with standard tooling.
- **Node.js / TypeScript:** Full support for CommonJS, ESM, Express/Fastify, and microservices.
- **JavaScript:** Pure Node.js and JavaScript applications.
- **Python:** CLI utilities, background workers, and FastAPI/Flask services.
Additional adapters (Java/Kotlin, C#, Ruby, C) follow the standardized `adapterkit` contract.

### What does "parity" mean in go-anything?
Per **Rule R-6**, parity is claimed **strictly and only for recorded and fuzzed inputs**. We never claim mathematical or unobserved equivalence. If an endpoint or feature was never recorded in `.go-anything/cases/`, it is not certified as identical.

### Can AI agents deploy or cutover traffic to production?
**Never.** Per **Rule R-1 (Human-Only Controls)**, the commands `approve`, `promote`, `lift`, and any production routing changes are CLI-only. They require human judgment and are strictly excluded from MCP tools and automated execution.

### How does secret redaction work?
Secret redaction is **core-owned and fail-closed** (Rule R-4). Passwords, Bearer/Basic tokens, JWTs, API keys (Google `AIza...`, Stripe, SendGrid, npm, Twilio), AWS access/secret keys, URL credentials, private keys, and sensitive assignments are masked in-memory before writing to `.go-anything/cases/`. Redaction cannot be disabled by third-party language adapters.

### What are the 10 states in a unit's lifecycle?
1. **Planned:** Identified in dependency wave order.
2. **Recorded:** Baseline behavioral test cases captured with redaction.
3. **Ready:** All upstream dependencies are verified.
4. **In Progress:** Claimed by an agent/developer with an active local lease.
5. **Verified:** Replay and full quality gate pass with commit-tied evidence.
6. **Shadow:** Running in parallel on real traffic with mismatch tracking.
7. **Approved:** Human operator CLI sign-off recorded.
8. **Cutover:** Primary routing directed to Go port with active fallback.
9. **Completed:** Zero fallback traffic observed; legacy bridge lifted.
- **Blocked:** Paused pending architectural decision or ADR.

### Where does migration state live?
All persistent migration state is stored in your project's `.go-anything/` directory:
- `manifest.json`: Units, waves, schema version.
- `units/*.json`: Durable unit state files.
- `cases/*.jsonl`: Redacted behavioral test cases.
- `evidence/*.json`: Quality gate evidence cryptographically bound to git commit SHAs.
- `local/`: Local file leases and locks (must be gitignored).

### Does go-anything send data or telemetry to remote servers?
**No.** `go-anything` has zero telemetry and runs entirely locally. Sandboxed execution disables outbound network traffic by default unless explicitly opted into via `--allow-network`.
