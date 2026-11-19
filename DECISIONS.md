# Architectural Decision Records (ADRs)

This document tracks architectural decisions for `go-anything`. In accordance with **Execution Rule 6**, when an agent or contributor encounters an unsettled architectural question, they stop and record an ADR with a recommended decision rather than guessing.

---

## Foundational Pre-M0 Decisions

Prior to Milestone 0, key foundational design decisions were settled in the Architecture Plan (v3):

1. **License (Resolved):** Apache-2.0, chosen for its explicit patent grant, commercial compatibility, and permissive open-source terms.
2. **State Store (Resolved):** JSON and JSONL files with atomic writes (`temp file -> fsync -> rename`) under OS file locks, ensuring durable unit state is reviewable in pull requests.
3. **First Non-Go Adapter (Resolved):** Python, implementing subprocess and HTTP transports first.
4. **Go Modernization (Resolved):** Supported as a mode of the Go adapter (`go-anything init --source=go`) with `go-anything modernize` as an alias.

---

## ADR Index

| ADR | Title | Status | Date | Decision Summary | Document |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **ADR-001** | Restricted Execution Sandbox Backend | **Resolved** | 2026-11-19 | Use native Linux namespaces with `bubblewrap` fallback; best-effort isolation with explicit warning and env stripping on macOS/Windows. No mandatory container daemon. | [adr-001-sandbox-backend.md](Docs/decisions/adr-001-sandbox-backend.md) |
| **ADR-002** | Cross-Machine Unit Claims and Coordination | **Resolved** | 2026-11-19 | Pull-request and branch ownership is sufficient through v1.0; local leases in `.go-anything/local/` coordinate agents on a single machine; no shared claim service before v1.0. | [adr-002-cross-machine-claims.md](Docs/decisions/adr-002-cross-machine-claims.md) |

---

## Resolved ADR Summaries

### [ADR-001: Restricted Execution Sandbox Backend](Docs/decisions/adr-001-sandbox-backend.md)
* **Status:** Resolved
* **Scope:** `internal/sandbox/` (Task T0.5)
* **Summary:**
  - **Context:** `go-anything` executes original code during recording and differential fuzzing. Execution Rule 3 requires fail-closed isolation (no-network by default, environment allowlist, temp working copy, resource limits).
  - **Decision:** Tiered sandbox approach:
    1. *Linux:* Native Linux kernel namespaces (`unshare` via `syscall.SysProcAttr`) with `bubblewrap` (`bwrap`) fallback where user namespaces are restricted.
    2. *macOS & Windows:* Best-effort process runner using an environment allowlist, temporary directory isolation, timeout/process-group termination, and an explicit console warning (`[WARN] Sandbox running in best-effort mode...`). Proxy environment variables are directed to invalid sinks when `--allow-network` is not set.
    3. *Container Daemons:* Docker/Podman are not required for standard workflows, avoiding heavyweight dependencies and startup latency.

### [ADR-002: Cross-Machine Unit Claims and Coordination](Docs/decisions/adr-002-cross-machine-claims.md)
* **Status:** Resolved
* **Scope:** `internal/state/` (Task T0.3), MCP `claim_unit` (Task T3.2)
* **Summary:**
  - **Context:** Migration units are tracked in `.go-anything/units/<unit>.json`. Previous designs storing leases in git caused merge conflicts and stale locks.
  - **Decision:**
    1. *Single Machine:* Agents running on the same host coordinate via time-bounded leases (`owner`, `expires_at`) in `.go-anything/local/` under OS file locks. Local leases are gitignored and automatically expire if an agent crashes.
    2. *Cross-Machine:* Standard Git workflow (branches and Pull Requests) is sufficient through v1.0. Branch naming (`unit/<unit-id>`) and PR ownership prevent conflicting claims without needing an external distributed lock service.
    3. *No External Service:* No centralized coordination server (Redis/etcd/API) will be built or required prior to v1.0, preserving offline-first and zero-infrastructure operation.
