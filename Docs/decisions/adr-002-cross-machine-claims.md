# ADR-002: Cross-Machine Unit Claims and Coordination

* **Status:** Resolved
* **Date:** 2026-11-19
* **Deciders:** go-anything core team / task T0.0
* **Consulted:** Architecture Plan v3, Task Plan v3.1

---

## Context

In `go-anything`, codebases undergoing migration are partitioned into modular migration units. Durable unit state is stored in `.go-anything/units/<unit>.json` (one file per unit), tracking the unit's lifecycle across states (`Ready`, `Claimed`, `InProgress`, `Verified`, `Approved`, `Promoted`, `Lifted`, `Blocked`, `Failed`, `Reverted`).

Migrations can be executed concurrently by multiple actors:
1. Multiple AI coding agents operating on a single local developer machine or workstation.
2. Multiple developers and agents working across separate physical machines, cloud instances, or CI pipelines.

Earlier designs attempted to track active agent claims and leases directly within the repository-tracked `manifest.json`. That approach failed because committing lease metadata and heartbeat updates to git caused severe merge conflicts, git churn, and stale claims blocking progress across pull requests.

The v3 architecture resolved the single-machine problem by isolating transient leases and locks in a gitignored directory: `.go-anything/local/`. However, an architectural question remained open before Milestone 0:
> **Question:** Is pull-request ownership of units sufficient for cross-machine coordination through v1.0, or is a dedicated, shared claim service (e.g., Redis, etcd, or a hosted coordinator API) required?

---

## Alternatives Considered

### Alternative 1: Centralized / Shared Claim Service (HTTP/gRPC Coordinator, Redis, DynamoDB)
- **Mechanism:** Provide a centralized network service where agents on any machine register heartbeats, claim units, and query current lock holders in real-time.
- **Pros:**
  - Instant visibility across all physical machines and CI runners.
  - Immediate preemption or rejection when two agents on different machines attempt to claim the same unit concurrently.
- **Cons:**
  - Violates `go-anything`'s core principles: local-first, zero external infrastructure dependencies, and offline-capable operation.
  - Introduces operational burden (hosting, provisioning, security credentials, network connectivity).
  - Creates a single point of failure: network downtime or service outage halts migration workflows.
  - Substantially complicates setup for solo developers and small teams.

### Alternative 2: Tracking Claims in Git Branches or Metadata Commits
- **Mechanism:** Commit claim records to dedicated git metadata branches (e.g., `refs/notes/` or a `claims` branch) pushed and fetched before claiming.
- **Pros:**
  - Uses Git as the unified transport layer without an auxiliary service.
- **Cons:**
  - Race conditions during concurrent `git push` operations require complex retry loops and merge resolution.
  - Unclean failures or network interruptions leave orphaned claims in git history that require manual administrative pruning.
  - Adds synchronization latency and token overhead for agent workflows.

### Alternative 3: Pull-Request Ownership + Local Leases in `.go-anything/local/`
- **Mechanism:**
  - **Single Machine:** Coordination between agents running on the same host is handled via time-bounded leases (`owner`, `expires_at`) stored in `.go-anything/local/` under OS file locks.
  - **Cross-Machine:** Coordination between separate machines is mediated by standard Git branches and Pull Requests (PRs). An agent or developer claims a unit by creating a feature branch (e.g. `unit/<unit-name>`) and opening a PR.
- **Pros:**
  - **Zero External Dependencies:** Operates fully offline and locally without deploying or maintaining a coordination server.
  - **No Git Churn on Leases:** Leases in `.go-anything/local/` are gitignored; pull requests contain only meaningful code and durable state transitions.
  - **Natural Conflict Detection:** Git hosting platforms (GitHub, GitLab, Bitbucket) and branch namespaces naturally surface duplicate or conflicting efforts.
  - **Fail-Safe Crash Recovery:** Local leases have a defined time-to-live (`expires_at`). If an agent process terminates abruptly, its local lease expires automatically and returns the unit to `Ready`.
  - **Fits Existing Engineering Workflows:** Developers and automated agents already work via branches and pull requests; no new mental model is imposed.
- **Cons:**
  - Does not prevent two agents on different machines from starting work on the same unassigned unit simultaneously if neither has pushed a branch or PR yet.

---

## Decision

We decide that **Pull-Request ownership of units is sufficient through v1.0**, with **local leases in `.go-anything/local/` coordinating agents on a single machine**. **No shared claim service will be required or built prior to v1.0.**

### Key Rules & Behaviors:

1. **Local Machine Coordination (Task T0.3):**
   - Agents running on the same machine coordinate through `.go-anything/local/<unit>.lease`.
   - Each lease contains `owner` (agent/worker ID), `claimed_at`, and `expires_at` (ISO 8601 timestamp with a bounded TTL, default 30 minutes, renewed on active work).
   - Lease acquisition and state mutations are executed under an OS file lock (flock on Unix, LockFileEx on Windows) with atomic write patterns (`temp file -> fsync -> rename`).
   - If a lease expires without renewal, the local state engine considers the unit `Ready` and allows other local agents to claim it.

2. **Cross-Machine Coordination via Git & PR Ownership:**
   - Units are claimed across distributed workers through standard Git workflows: creating a branch (conventionally `unit/<unit-id>` or `go-anything/<unit-id>`) and opening a draft Pull Request or remote tracking branch.
   - Humans or agent orchestrators partition units across developers or CI jobs before kickoff.
   - When reviewing or merging, PR ownership constitutes authoritative assignment of the unit.

3. **Scope and Post-v1.0 Extensibility:**
   - For all milestones up to v1.0 (M0 through M7), no external network service for distributed locking will be created.
   - If enterprise teams running distributed multi-node agent swarms request centralized coordination in a post-v1.0 release, it will be designed as an optional, pluggable claim backend interface in `internal/state/`, preserving the local lease engine as the default.

---

## Consequences

### Positive
- **Simplicity & Reliability:** Zero infrastructure, zero configuration, zero cloud cost, and no failure modes stemming from external service outages.
- **Clean Git History:** Git history contains durable unit specifications, evidence, and code, completely free from lease heartbeat commits or merge conflicts.
- **Resilient Recovery:** Crashed or abandoned agents automatically release their claims when local leases expire, preventing stalled migrations.
- **Security & Privacy:** Preserves the core principle of fail-closed, zero-telemetry, local-only execution.

### Negative / Trade-offs
- **Potential Duplicate Work Across Disconnected Hosts:** If two developers on different machines start work on the exact same unit without checking remote branches or opening a draft PR, collision is only caught upon push or PR creation. This is acceptable for v1.0 and matches normal Git-based software development practices.

---

## Implementation Guidance for Task T0.3 & MCP
- Package: `internal/state`
- Store durable unit state in `.go-anything/units/<unit>.json`.
- Store transient lease files in `.go-anything/local/<unit>.lease` (with `local/` included in `.gitignore`).
- Enforce atomic persistence (`temp file -> fsync -> rename`) under OS file lock.
- Implement lease expiry checking: an expired lease returns the unit state to `Ready`.
- In MCP tool `claim_unit` (T3.2), check and acquire the local lease before transitioning unit state.
