package state_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createFullProfileChecks() []adapterkit.GateCheck {
	return []adapterkit.GateCheck{
		{Name: "build", Status: adapterkit.StatusPass, Output: "build clean"},
		{Name: "format", Status: adapterkit.StatusPass, Output: "format clean"},
		{Name: "vet", Status: adapterkit.StatusPass, Output: "vet clean"},
		{Name: "race", Status: adapterkit.StatusPass, Output: "race clean"},
		{Name: "govulncheck", Status: adapterkit.StatusPass, Output: "no vulnerabilities"},
		{Name: "replay", Status: adapterkit.StatusPass, Output: "100% replay matched"},
		{Name: "error_paths", Status: adapterkit.StatusPass, Output: "error paths clean"},
		{Name: "api_compat", Status: adapterkit.StatusPass, Output: "api compat clean"},
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

		ev := &adapterkit.Evidence{
			EvidenceID:      "evi-missing-api",
			UnitID:          "unit-core",
			CommitSHA:       validCommitSHA,
			Profile:         adapterkit.ProfileFull,
			Checks:          filtered,
			MismatchesCount: 0,
		}
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

		ev := &adapterkit.Evidence{
			EvidenceID:      "evi-failed-api",
			UnitID:          "unit-core",
			CommitSHA:       validCommitSHA,
			Profile:         adapterkit.ProfileFull,
			Checks:          checks,
			MismatchesCount: 0,
		}
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

		ev := &adapterkit.Evidence{
			EvidenceID:      "evi-missing-err",
			UnitID:          "unit-core",
			CommitSHA:       validCommitSHA,
			Profile:         adapterkit.ProfileFull,
			Checks:          filtered,
			MismatchesCount: 0,
		}
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

		ev := &adapterkit.Evidence{
			EvidenceID:      "evi-failed-err",
			UnitID:          "unit-core",
			CommitSHA:       validCommitSHA,
			Profile:         adapterkit.ProfileFull,
			Checks:          checks,
			MismatchesCount: 0,
		}
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

		ev := &adapterkit.Evidence{
			EvidenceID:      "evi-failed-race",
			UnitID:          "unit-core",
			CommitSHA:       validCommitSHA,
			Profile:         adapterkit.ProfileFull,
			Checks:          checks,
			MismatchesCount: 0,
		}
		_, err := store.Transition("unit-core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-test",
			Evidence: ev,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
	})

	t.Run("clean verification with commit-tied evidence succeeds and is queryable", func(t *testing.T) {
		cleanEvidence := &adapterkit.Evidence{
			EvidenceID:      fmt.Sprintf("evi-core-%s", validCommitSHA[:8]),
			UnitID:          "unit-core",
			CommitSHA:       validCommitSHA,
			Profile:         adapterkit.ProfileFull,
			VerifiedAt:      time.Now().UTC().Format(time.RFC3339),
			VerifiedBy:      "agent-test",
			Checks:          createFullProfileChecks(),
			MismatchesCount: 0,
		}

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

		// Verify latest evidence lookup
		latest, err := store.GetLatestEvidenceForUnit("unit-core")
		require.NoError(t, err)
		assert.Equal(t, cleanEvidence.EvidenceID, latest.EvidenceID)
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
