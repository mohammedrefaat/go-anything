<!-- BEGIN GENERATED: mcp-tools -->
The MCP server (`go-anything mcp`, `stdio` only) exposes **14 tools** (agent-accessible):

| Tool | Access | Purpose |
| :--- | :--- | :--- |
| `status` | Agent | Show migration progress, next unit in dependency wave order, active leases, and blockers. |
| `claim_unit` | Agent | Take a Ready migration unit with a time-bounded local lease in .go-anything/local/. |
| `inspect_unit` | Agent | View original source code, sanitized sample cases, detected traps, and constraints for a unit. |
| `record_cases` | Agent | Run the original application, apply fail-closed redaction, and save sanitized cases into .go-anything/cases/. |
| `verify_diff` | Agent | Replay recorded behavioral cases against the ported Go implementation and return PASS or top sanitized mismatches. |
| `run_gate` | Agent | Run the full quality gate pipeline (toolchain checks, error-paths, and API compatibility) and generate commit-tied evidence. |
| `mark_verified` | Agent | Advance unit to Verified state. Strictly rejected unless valid commit-tied evidence exists for the target commit SHA. |
| `handoff` | Agent | Write HANDOFF.md with workspace state, progress, and next steps for the next session or reviewer. |
| `analyze` | Agent | Analyze codebase dependencies and view topological migration wave ordering (read-only). |
| `profile` | Agent | Profile legacy execution and rank candidate hot functions for migration (read-only). |
| `bridge` | Agent | Scaffold strangler sidecar bridge, manifest, and client wrappers (only writes generated scaffold files). |
| `hotpath` | Agent | Execute guided hot-path porting pipeline up to the human boundary; strictly stops before approval (Rule R-1). |
| `fuzz_bench` | Agent | Run differential fuzz testing or latency/throughput benchmarking against recorded cases and target. |
| `shadow_status` | Agent | Inspect front door shadow mode execution metrics, request counts, and mismatch logs (read-only). |

**Not exposed via MCP (CLI-only, Execution Rule 4):**

- `approve`: Human sign-off and state advance to Approved
- `promote`: Front-door routing table cutover
- `lift`: Retirement of original codebase and fallback bridges
- `routing`: Manual routing overrides
- `toggle_shadow`: Live traffic shadow activation
- `kill_switch`: Kill-switch override and emergency fallback engagement
<!-- END GENERATED: mcp-tools -->
