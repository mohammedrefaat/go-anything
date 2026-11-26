package state_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createFullProfileChecks() []adapterkit.GateCheck {
	return []adapterkit.GateCheck{
		{Name: "build", Status: adapterkit.StatusPass, Output: "build clean"},
		{Name: "format", Status: adapterkit.StatusPass, Output: "format clean"},
		{Name: "vet", Status: adapterkit.StatusPass, Output: "vet clean"},
		{Name: "staticcheck", Status: adapterkit.StatusPass, Output: "staticcheck clean"},
		{Name: "race", Status: adapterkit.StatusPass, Output: "race clean"},
		{Name: "govulncheck", Status: adapterkit.StatusPass, Output: "no vulnerabilities"},
		{Name: "replay", Status: adapterkit.StatusPass, Output: "100% replay matched"},
		{Name: "error_paths", Status: adapterkit.StatusPass, Output: "error paths clean"},
		{Name: "api_compat", Status: adapterkit.StatusPass, Output: "api compat clean"},
	}
}

func fullProfileCheckNames() []string {
	return []string{"build", "format", "vet", "staticcheck", "race", "govulncheck", "replay", "error_paths", "api_compat"}
}

func newValidFullEvidence(id, unitID, commitSHA string, checks []adapterkit.GateCheck) *adapterkit.Evidence {
	return &adapterkit.Evidence{
		SchemaVersion:   adapterkit.SchemaVersionV2,
		EvidenceID:      id,
		UnitID:          unitID,
		CommitSHA:       commitSHA,
		Profile:         adapterkit.ProfileFull,
		ProfileVersion:  adapterkit.FullProfileVersionV201,
		ProfileChecks:   fullProfileCheckNames(),
		VerifiedAt:      time.Now().UTC().Format(time.RFC3339),
		VerifiedBy:      "agent-test",
		Checks:          checks,
		MismatchesCount: 0,
	}
}

func TestFullProfile_GuardsAndEvidence(t *testing.T) {
	_, store := setupTestWorkspace(t)

	// Unit with default empty EvidenceProfile (must default to ProfileFull)
	unit := &adapterkit.Unit{
		ID:    "unit-core",
		Name:  "Core Module",
		State: adapterkit.StateRecorded,
	}
	require.NoError(t, store.SaveUnit(unit))

	// Move to Ready -> InProgress
	_, err := store.Transition("unit-core", adapterkit.StateReady, state.TransitionOptions{})
	require.NoError(t, err)

	_, err = store.Transition("unit-core", adapterkit.StateInProgress, state.TransitionOptions{
		Actor:    "agent-test",
		LeaseTTL: time.Hour,
	})
	require.NoError(t, err)

	validCommitSHA := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"

	t.Run("rejects transition when evidence is nil", func(t *testing.T) {
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor: "agent-test",
		})
		assert.ErrorIs(t, err, state.ErrEvidenceMissing)
	})

	for _, name := range []string{"race", "govulncheck"} {
		t.Run("rejects skipped "+name, func(t *testing.T) {
			checks := createFullProfileChecks()
			for i := range checks {
				if checks[i].Name == name {
					checks[i].Status = adapterkit.StatusSkipped
				}
			}
			_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
				Actor:    "agent-test",
				Evidence: newValidFullEvidence("evi-skipped-"+name, "unit-core", validCommitSHA, checks),
			})
			assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
		})
	}

	t.Run("rejects transition when commit_sha is invalid", func(t *testing.T) {
		ev := &adapterkit.Evidence{
			EvidenceID:      "evi-bad-sha",
			UnitID:          "unit-core",
			CommitSHA:       "invalid-short-sha",
			Profile:         adapterkit.ProfileFull,
			Checks:          createFullProfileChecks(),
			MismatchesCount: 0,
		}
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceMissing)
		assert.Contains(t, err.Error(), "commit_sha must be a 40-character hex string")
	})

	t.Run("rejects transition when mismatches count > 0", func(t *testing.T) {
		ev := &adapterkit.Evidence{
			EvidenceID:      "evi-mismatches",
			UnitID:          "unit-core",
			CommitSHA:       validCommitSHA,
			Profile:         adapterkit.ProfileFull,
			Checks:          createFullProfileChecks(),
			MismatchesCount: 3,
		}
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceMismatches)
	})

	t.Run("rejects transition when API compatibility check is missing", func(t *testing.T) {
		checks := createFullProfileChecks()
		// Remove api_compat check
		var filtered []adapterkit.GateCheck
		for _, c := range checks {
			if c.Name != "api_compat" {
				filtered = append(filtered, c)
			}
		}

		ev := newValidFullEvidence("evi-missing-api", "unit-core", validCommitSHA, filtered)
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
		assert.Contains(t, err.Error(), "requires check \"api_compat\"")
	})

	t.Run("rejects transition when API compatibility check has failed status", func(t *testing.T) {
		checks := createFullProfileChecks()
		for i := range checks {
			if checks[i].Name == "api_compat" {
				checks[i].Status = adapterkit.StatusFail
				checks[i].Output = "removed exported func Convert"
			}
		}

		ev := newValidFullEvidence("evi-failed-api", "unit-core", validCommitSHA, checks)
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
		assert.Contains(t, err.Error(), "must be pass")
	})

	t.Run("rejects transition when error paths check is missing", func(t *testing.T) {
		checks := createFullProfileChecks()
		var filtered []adapterkit.GateCheck
		for _, c := range checks {
			if c.Name != "error_paths" {
				filtered = append(filtered, c)
			}
		}

		ev := newValidFullEvidence("evi-missing-err", "unit-core", validCommitSHA, filtered)
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
		assert.Contains(t, err.Error(), "requires check \"error_paths\"")
	})

	t.Run("rejects transition when error paths check failed", func(t *testing.T) {
		checks := createFullProfileChecks()
		for i := range checks {
			if checks[i].Name == "error_paths" {
				checks[i].Status = adapterkit.StatusFail
				checks[i].Output = "missing error-path case for: invalid amount"
			}
		}

		ev := newValidFullEvidence("evi-failed-err", "unit-core", validCommitSHA, checks)
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
	})

	t.Run("rejects transition when toolchain check failed", func(t *testing.T) {
		checks := createFullProfileChecks()
		for i := range checks {
			if checks[i].Name == "race" {
				checks[i].Status = adapterkit.StatusFail
				checks[i].Output = "DATA RACE detected"
			}
		}

		ev := newValidFullEvidence("evi-failed-race", "unit-core", validCommitSHA, checks)
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
	})

	t.Run("explicit vulnerability failure overrides a passing alias", func(t *testing.T) {
		checks := append(createFullProfileChecks(), adapterkit.GateCheck{Name: "vulncheck", Status: adapterkit.StatusFail})
		ev := newValidFullEvidence("evi-failed-vuln-alias", "unit-core", validCommitSHA, checks)
		ev.ProfileChecks = append(fullProfileCheckNames(), "vulncheck")
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		require.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
	})

	t.Run("verification accepts a passing vulnerability alias and keeps commit-tied evidence queryable", func(t *testing.T) {
		checks := createFullProfileChecks()
		for i := range checks {
			if checks[i].Name == "govulncheck" {
				checks[i].Status = adapterkit.StatusSkipped
			}
		}
		checks = append(checks, adapterkit.GateCheck{Name: "vulncheck", Status: adapterkit.StatusPass})
		cleanEvidence := newValidFullEvidence(fmt.Sprintf("evi-core-%s", validCommitSHA[:8]), "unit-core", validCommitSHA, checks)
		cleanEvidence.ProfileChecks = append(fullProfileCheckNames(), "vulncheck")

		verifiedUnit, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: cleanEvidence,
		})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateVerified, verifiedUnit.State)

		// Verify stored evidence by commit SHA
		retrievedByCommit, err := store.GetEvidenceForCommit("unit-core", validCommitSHA)
		require.NoError(t, err)
		assert.Equal(t, cleanEvidence.EvidenceID, retrievedByCommit.EvidenceID)
		assert.Equal(t, validCommitSHA, retrievedByCommit.CommitSHA)
		assert.Equal(t, adapterkit.ProfileFull, retrievedByCommit.Profile)
		assert.Equal(t, adapterkit.SchemaVersionV2, retrievedByCommit.SchemaVersion)
		assert.Equal(t, adapterkit.FullProfileVersionV201, retrievedByCommit.ProfileVersion)

		// Verify latest evidence lookup
		latest, err := store.GetLatestEvidenceForUnit("unit-core")
		require.NoError(t, err)
		assert.Equal(t, cleanEvidence.EvidenceID, latest.EvidenceID)
	})

	t.Run("rejects stale evidence profile definitions fail-closed", func(t *testing.T) {
		staleUnit := &adapterkit.Unit{
			ID:              "unit-stale-check",
			Name:            "Stale Check Unit",
			State:           adapterkit.StateInProgress,
			EvidenceProfile: adapterkit.ProfileFull,
		}
		require.NoError(t, store.SaveUnit(staleUnit))
		_, err := store.AcquireLease("unit-stale-check", "agent-test", time.Hour)
		require.NoError(t, err)

		tests := []struct {
			name   string
			mutate func(ev *adapterkit.Evidence)
			errSub string
		}{
			{
				name:   "empty schema_version",
				mutate: func(ev *adapterkit.Evidence) { ev.SchemaVersion = "" },
				errSub: "stale evidence schema_version",
			},
			{
				name:   "v1alpha1 schema_version",
				mutate: func(ev *adapterkit.Evidence) { ev.SchemaVersion = adapterkit.SchemaVersionV1Alpha1 },
				errSub: "stale evidence schema_version",
			},
			{
				name:   "v1 schema_version",
				mutate: func(ev *adapterkit.Evidence) { ev.SchemaVersion = adapterkit.SchemaVersionV1 },
				errSub: "stale evidence schema_version",
			},
			{
				name:   "missing profile_version",
				mutate: func(ev *adapterkit.Evidence) { ev.ProfileVersion = "" },
				errSub: "stale or missing profile_version",
			},
			{
				name:   "older profile_version",
				mutate: func(ev *adapterkit.Evidence) { ev.ProfileVersion = "2.0.0" },
				errSub: "stale or missing profile_version",
			},
			{
				name:   "missing profile_checks",
				mutate: func(ev *adapterkit.Evidence) { ev.ProfileChecks = nil },
				errSub: "missing profile_checks",
			},
			{
				name: "profile_checks missing staticcheck",
				mutate: func(ev *adapterkit.Evidence) {
					ev.ProfileChecks = []string{"build", "format", "vet", "race", "govulncheck", "replay", "error_paths", "api_compat"}
				},
				errSub: "missing required check \"staticcheck\"",
			},
			{
				name: "checks missing staticcheck",
				mutate: func(ev *adapterkit.Evidence) {
					var filtered []adapterkit.GateCheck
					for _, c := range ev.Checks {
						if c.Name != "staticcheck" {
							filtered = append(filtered, c)
						}
					}
					ev.Checks = filtered
				},
				errSub: "requires check \"staticcheck\"",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				ev := newValidFullEvidence("evi-stale-"+tc.name, "unit-stale-check", validCommitSHA, createFullProfileChecks())
				tc.mutate(ev)
				_, err := store.Transition("unit-stale-check", adapterkit.StateVerified, state.TransitionOptions{
					Actor:    "agent-test",
					Evidence: ev,
				})
				require.Error(t, err)
				assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
				assert.Contains(t, err.Error(), tc.errSub)
			})
		}
	})

	t.Run("explicit replay profile override continues to function", func(t *testing.T) {
		replayUnit := &adapterkit.Unit{
			ID:              "unit-replay-only",
			Name:            "Replay Unit",
			State:           adapterkit.StateRecorded,
			EvidenceProfile: adapterkit.ProfileReplay,
		}
		require.NoError(t, store.SaveUnit(replayUnit))

		_, err := store.Transition("unit-replay-only", adapterkit.StateReady, state.TransitionOptions{})
		require.NoError(t, err)

		_, err = store.Transition("unit-replay-only", adapterkit.StateInProgress, state.TransitionOptions{
			Actor: "worker-replay",
		})
		require.NoError(t, err)

		replayEvidence := &adapterkit.Evidence{
			EvidenceID:      "evi-replay-001",
			UnitID:          "unit-replay-only",
			CommitSHA:       validCommitSHA,
			Profile:         adapterkit.ProfileReplay,
			VerifiedAt:      time.Now().UTC().Format(time.RFC3339),
			VerifiedBy:      "worker-replay",
			Checks:          []adapterkit.GateCheck{{Name: "replay", Status: adapterkit.StatusPass}},
			MismatchesCount: 0,
		}

		u, err := store.Transition("unit-replay-only", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "worker-replay",
			Evidence: replayEvidence,
		})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateVerified, u.State)
	})
}
