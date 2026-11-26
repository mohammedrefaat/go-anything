# ADR-008: Gate Profile Alignment and Staged Security Rollout

* **Status:** Resolved
* **Date:** 2026-11-25
* **Deciders:** Mohammed Refaat / go-anything core team
* **Consulted:** Docs/GO_ANYTHING_FIX_AND_SECURITY_PLAN.md (Finding F7, Task A6)

---

## 1. Context & Problem Statement

Previously, the quality gate check lists in `internal/gate/types.go` and the evidence validation in `internal/state/machine.go` diverged:
1. `AllChecks()` included `build`, `format`, `vet`, `staticcheck`, `golangci-lint`, `race`, and `govulncheck`.
2. `FullProfileChecks()` included `build`, `format`, `vet`, `race`, `govulncheck`, `replay`, `error_paths`, and `api_compat` — omitting `staticcheck` and `golangci-lint`, making the release-grade `full` profile weaker than the default gate check list.
3. Evidence records (`.go-anything/evidence/*.json` under `v1alpha1` / `v1`) did not record the profile version or required check list (`profile_version`, `profile_checks`), so `mark_verified` could not distinguish evidence generated under an older profile definition from current evidence.
4. Upcoming security engine checks (`security` and `security_parity`) need a predictable rollout path into the `full` profile without abruptly breaking existing workflows.

---

## 2. Decision

We adopt **Option C: Staged Rollout of the `full` Gate Profile** with fail-closed evidence schema versioning:

1. **v2.0.1 (Immediate — Task A6):**
   - Add `staticcheck` to `FullProfileChecks()` in `internal/gate/types.go` and require it in `internal/state/machine.go` so the release-grade `full` profile is never weaker than the default `staticcheck` check.
   - Bump the evidence contract to `schemas/v2/evidence.schema.json` (`schema_version: "v2"`), adding `profile_version` (`"2.0.1"`) and `profile_checks` (`[]string`).
   - Enforce fail-closed validation in `mark_verified` (`internal/state/machine.go`): any `full`-profile evidence produced under an older schema (`v1`, `v1alpha1`, or empty), an older or missing `profile_version`, or a `profile_checks` list missing required checks is rejected.
   - **Migration impact:** Units already in `Verified` state under the pre-v2.0.1 `full` profile definition must re-run `go-anything gate` (or MCP `run_gate`) before `promote`.

2. **v2.1.0 (Security Advisory):**
   - Introduce the `security` gate check (backed by `internal/audit/`) into `go-anything gate` and scorecards as **advisory** — findings are reported in output and scorecards, but do not block `full` profile verification.

3. **v2.2.0 (Security Required):**
   - Promote `security` (and `security_parity` when security cases exist) to **required** in `FullProfileChecks()`, accompanied by a second evidence-schema/profile-version bump in the same release.

*(Open item deferred for owner decision: whether `golangci-lint` also joins `FullProfileChecks()` in a future release.)*

---

## 3. Consequences

### Positive
- Eliminates the gap where `full` profile verification could pass without `staticcheck`.
- Prevents stale or weaker historical evidence from being accepted by `mark_verified` after profile upgrades (fail-closed).
- Gives teams a clear, versioned ramp-up path for `security` checks (advisory in v2.1.0, enforced in v2.2.0).

### Negative / Neutral
- Existing units verified with `v1alpha1`/`v1` evidence for `full` must re-run `gate` under v2.0.1 before `promote`.
- Environments running `full` profile must have `staticcheck` available (or use `--profile replay` when only behavioral parity is targeted).
