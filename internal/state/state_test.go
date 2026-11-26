package state_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestWorkspace(t *testing.T) (string, *state.Store) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "ga-state-test-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})

	store, err := state.InitializeWorkspace(tempDir, "test-service", "go")
	require.NoError(t, err)
	return tempDir, store
}

func TestInitializeWorkspace(t *testing.T) {
	tempDir, store := setupTestWorkspace(t)

	// Verify directory structure
	assert.DirExists(t, store.RootDir())
	assert.DirExists(t, store.UnitsDir())
	assert.DirExists(t, store.LocalDir())
	assert.DirExists(t, store.CasesDir())
	assert.DirExists(t, store.ReportsDir())
	assert.DirExists(t, store.EvidenceDir())

	// Verify manifest and rules
	manifest, err := store.GetManifest()
	require.NoError(t, err)
	assert.Equal(t, adapterkit.SchemaVersionV1Alpha1, manifest.SchemaVersion)
	assert.Equal(t, "test-service", manifest.ProjectName)

	rules, err := store.GetRules()
	require.NoError(t, err)
	assert.Equal(t, adapterkit.SchemaVersionV1Alpha1, rules.SchemaVersion)

	// Verify .gitignore coverage
	gitignorePath := filepath.Join(tempDir, state.GitignoreFileName)
	assert.FileExists(t, gitignorePath)
	data, err := os.ReadFile(gitignorePath)
	require.NoError(t, err)
	assert.Contains(t, string(data), state.LocalGitignoreEntry)

	// Idempotent initialization
	store2, err := state.InitializeWorkspace(tempDir, "test-service", "go")
	require.NoError(t, err)
	assert.Equal(t, store.RootDir(), store2.RootDir())
}

func TestAtomicWritesAndConcurrentLocks(t *testing.T) {
	_, store := setupTestWorkspace(t)

	unitCount := 10
	iterations := 5
	var wg sync.WaitGroup

	for i := 0; i < unitCount; i++ {
		unitID := fmt.Sprintf("unit-%d", i)
		initialUnit := &adapterkit.Unit{
			ID:              unitID,
			Name:            fmt.Sprintf("Unit %d", i),
			State:           adapterkit.StatePlanned,
			EvidenceProfile: adapterkit.ProfileReplay,
		}
		require.NoError(t, store.SaveUnit(initialUnit))
	}

	// Concurrent writes
	for i := 0; i < unitCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			unitID := fmt.Sprintf("unit-%d", id)
			for j := 0; j < iterations; j++ {
				u, err := store.GetUnit(unitID)
				if assert.NoError(t, err) {
					u.FallbackCount = j + 1
					err = store.SaveUnit(u)
					assert.NoError(t, err)
				}
			}
		}(i)
	}
	wg.Wait()

	// Verify final state of all units
	for i := 0; i < unitCount; i++ {
		unitID := fmt.Sprintf("unit-%d", i)
		u, err := store.GetUnit(unitID)
		require.NoError(t, err)
		assert.Equal(t, iterations, u.FallbackCount)
	}
}

func TestLeaseLifecycleAndExpiry(t *testing.T) {
	_, store := setupTestWorkspace(t)

	unit := &adapterkit.Unit{
		ID:              "auth",
		Name:            "Authentication",
		State:           adapterkit.StateReady,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(unit))

	// Transition to In progress with a 100ms short lease
	_, err := store.Transition("auth", adapterkit.StateInProgress, state.TransitionOptions{
		Actor:    "agent-1",
		LeaseTTL: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	// Another agent cannot claim the unit while active
	_, err = store.AcquireLease("auth", "agent-2", 10*time.Minute)
	assert.ErrorIs(t, err, state.ErrLeaseActive)

	// Same agent can renew
	renewed, err := store.AcquireLease("auth", "agent-1", 100*time.Millisecond)
	require.NoError(t, err)
	assert.True(t, renewed.IsValid())

	// Wait for lease to expire
	time.Sleep(150 * time.Millisecond)

	// Expired lease: GetUnit returns unit to Ready automatically per ADR-002
	u, err := store.GetUnit("auth")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateReady, u.State)

	// Now agent-2 can claim
	lease, err := store.AcquireLease("auth", "agent-2", 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "agent-2", lease.Owner)

	// Releasing lease allows others to claim
	require.NoError(t, store.ReleaseLease("auth", "agent-2"))
	leaseAfterRelease, err := store.GetLease("auth")
	require.NoError(t, err)
	assert.Nil(t, leaseAfterRelease)
}

func TestStateTransitionsAndGuards(t *testing.T) {
	_, store := setupTestWorkspace(t)

	// Create dependency unit "db" and main unit "api"
	dbUnit := &adapterkit.Unit{
		ID:              "db",
		Name:            "Database Unit",
		State:           adapterkit.StatePlanned,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(dbUnit))

	apiUnit := &adapterkit.Unit{
		ID:              "api",
		Name:            "API Unit",
		State:           adapterkit.StateRecorded,
		Dependencies:    []string{"db"},
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(apiUnit))

	t.Run("dependency guard blocks Ready when dependency is unverified", func(t *testing.T) {
		_, err := store.Transition("api", adapterkit.StateReady, state.TransitionOptions{})
		assert.ErrorIs(t, err, state.ErrDependencyNotMet)
	})

	t.Run("advance dependency to Verified", func(t *testing.T) {
		_, err := store.Transition("db", adapterkit.StateRecorded, state.TransitionOptions{})
		require.NoError(t, err)

		_, err = store.Transition("db", adapterkit.StateReady, state.TransitionOptions{})
		require.NoError(t, err)

		_, err = store.Transition("db", adapterkit.StateInProgress, state.TransitionOptions{
			Actor:    "worker-db",
			LeaseTTL: time.Minute,
		})
		require.NoError(t, err)

		validEvidence := &adapterkit.Evidence{
			EvidenceID:      "evi-db-001",
			UnitID:          "db",
			CommitSHA:       "1111222233334444555566667777888899990000",
			Profile:         adapterkit.ProfileReplay,
			VerifiedAt:      time.Now().UTC().Format(time.RFC3339),
			VerifiedBy:      "verifier",
			Checks:          []adapterkit.GateCheck{{Name: "replay", Status: adapterkit.StatusPass}},
			MismatchesCount: 0,
		}
		_, err = store.Transition("db", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "worker-db",
			Evidence: validEvidence,
		})
		require.NoError(t, err)
	})

	t.Run("api can now transition to Ready once db is Verified", func(t *testing.T) {
		u, err := store.Transition("api", adapterkit.StateReady, state.TransitionOptions{})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateReady, u.State)
	})

	t.Run("In progress requires actor", func(t *testing.T) {
		_, err := store.Transition("api", adapterkit.StateInProgress, state.TransitionOptions{})
		assert.Error(t, err)

		u, err := store.Transition("api", adapterkit.StateInProgress, state.TransitionOptions{
			Actor: "worker-api",
		})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateInProgress, u.State)
	})

	t.Run("Blocked state transitions", func(t *testing.T) {
		// Requires reason
		_, err := store.Transition("api", adapterkit.StateBlocked, state.TransitionOptions{
			Actor: "worker-api",
		})
		assert.Error(t, err)

		u, err := store.Transition("api", adapterkit.StateBlocked, state.TransitionOptions{
			Actor:  "worker-api",
			Reason: "Needs human decision on schema deprecation",
		})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateBlocked, u.State)
		assert.Equal(t, "Needs human decision on schema deprecation", u.Metadata["blocked_reason"])

		// Unblock back to Ready
		u, err = store.Transition("api", adapterkit.StateReady, state.TransitionOptions{})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateReady, u.State)

		// Claim again
		_, err = store.Transition("api", adapterkit.StateInProgress, state.TransitionOptions{
			Actor: "worker-api",
		})
		require.NoError(t, err)
	})

	t.Run("Verified requires valid evidence", func(t *testing.T) {
		// Missing evidence
		_, err := store.Transition("api", adapterkit.StateVerified, state.TransitionOptions{
			Actor: "worker-api",
		})
		assert.ErrorIs(t, err, state.ErrEvidenceMissing)

		// Mismatches non-zero
		badEvidence := &adapterkit.Evidence{
			EvidenceID:      "evi-bad",
			UnitID:          "api",
			CommitSHA:       "1111222233334444555566667777888899990000",
			Profile:         adapterkit.ProfileReplay,
			Checks:          []adapterkit.GateCheck{{Name: "replay", Status: adapterkit.StatusPass}},
			MismatchesCount: 2, // Fails
		}
		_, err = store.Transition("api", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "worker-api",
			Evidence: badEvidence,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceMismatches)

		// Replay check failed
		failedReplayEvidence := &adapterkit.Evidence{
			EvidenceID:      "evi-fail",
			UnitID:          "api",
			CommitSHA:       "1111222233334444555566667777888899990000",
			Profile:         adapterkit.ProfileReplay,
			Checks:          []adapterkit.GateCheck{{Name: "replay", Status: adapterkit.StatusFail}},
			MismatchesCount: 0,
		}
		_, err = store.Transition("api", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "worker-api",
			Evidence: failedReplayEvidence,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)

		// Valid replay evidence succeeds
		goodEvidence := &adapterkit.Evidence{
			EvidenceID:      "evi-api-001",
			UnitID:          "api",
			CommitSHA:       "1111222233334444555566667777888899990000",
			Profile:         adapterkit.ProfileReplay,
			Checks:          []adapterkit.GateCheck{{Name: "replay", Status: adapterkit.StatusPass}},
			MismatchesCount: 0,
		}
		u, err := store.Transition("api", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "worker-api",
			Evidence: goodEvidence,
		})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateVerified, u.State)
	})

	t.Run("Shadow and Approved transitions", func(t *testing.T) {
		// Verified -> Shadow
		u, err := store.Transition("api", adapterkit.StateShadow, state.TransitionOptions{})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateShadow, u.State)

		// Shadow -> Approved
		u, err = store.Transition("api", adapterkit.StateApproved, state.TransitionOptions{
			Actor: "human-approver",
		})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateApproved, u.State)

		// Approved -> Cutover
		u, err = store.Transition("api", adapterkit.StateCutover, state.TransitionOptions{})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateCutover, u.State)
	})

	t.Run("Cutover to Completed requires zero fallback count", func(t *testing.T) {
		// Injected fallback count blocks Completed
		u, err := store.GetUnit("api")
		require.NoError(t, err)
		u.FallbackCount = 3
		require.NoError(t, store.SaveUnit(u))

		_, err = store.Transition("api", adapterkit.StateCompleted, state.TransitionOptions{})
		assert.ErrorIs(t, err, state.ErrFallbackCountNonZero)

		// Reset to 0 passes
		u.FallbackCount = 0
		require.NoError(t, store.SaveUnit(u))

		completedUnit, err := store.Transition("api", adapterkit.StateCompleted, state.TransitionOptions{})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateCompleted, completedUnit.State)
	})
}

func TestFullEvidenceProfileRequirements(t *testing.T) {
	_, store := setupTestWorkspace(t)

	unit := &adapterkit.Unit{
		ID:              "core",
		Name:            "Core Engine",
		State:           adapterkit.StateInProgress,
		EvidenceProfile: adapterkit.ProfileFull,
	}
	require.NoError(t, store.SaveUnit(unit))
	_, err := store.AcquireLease("core", "agent-x", time.Hour)
	require.NoError(t, err)

	t.Run("missing gate checks in full profile rejected", func(t *testing.T) {
		partialEvidence := &adapterkit.Evidence{
			EvidenceID: "evi-partial",
			UnitID:     "core",
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Profile:    adapterkit.ProfileFull,
			Checks: []adapterkit.GateCheck{
				{Name: "build", Status: adapterkit.StatusPass},
				{Name: "replay", Status: adapterkit.StatusPass},
			},
			MismatchesCount: 0,
		}
		_, err := store.Transition("core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-x",
			Evidence: partialEvidence,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
	})

	t.Run("one failed check in full profile rejected", func(t *testing.T) {
		failingEvidence := &adapterkit.Evidence{
			EvidenceID: "evi-fail-check",
			UnitID:     "core",
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Profile:    adapterkit.ProfileFull,
			Checks: []adapterkit.GateCheck{
				{Name: "build", Status: adapterkit.StatusPass},
				{Name: "format", Status: adapterkit.StatusPass},
				{Name: "vet", Status: adapterkit.StatusPass},
				{Name: "race", Status: adapterkit.StatusFail}, // Data race detected!
				{Name: "replay", Status: adapterkit.StatusPass},
				{Name: "vulncheck", Status: adapterkit.StatusPass},
			},
			MismatchesCount: 0,
		}
		_, err := store.Transition("core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-x",
			Evidence: failingEvidence,
		})
		assert.ErrorIs(t, err, state.ErrEvidenceProfileUnsatisfied)
	})

	t.Run("all checks pass in full profile accepted", func(t *testing.T) {
		completeEvidence := &adapterkit.Evidence{
			SchemaVersion:  adapterkit.SchemaVersionV2,
			EvidenceID:     "evi-complete",
			UnitID:         "core",
			CommitSHA:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Profile:        adapterkit.ProfileFull,
			ProfileVersion: adapterkit.FullProfileVersionV201,
			ProfileChecks:  []string{"build", "format", "vet", "staticcheck", "race", "replay", "error_paths", "api_compat", "vulncheck"},
			Checks: []adapterkit.GateCheck{
				{Name: "build", Status: adapterkit.StatusPass},
				{Name: "format", Status: adapterkit.StatusPass},
				{Name: "vet", Status: adapterkit.StatusPass},
				{Name: "staticcheck", Status: adapterkit.StatusPass},
				{Name: "race", Status: adapterkit.StatusPass},
				{Name: "replay", Status: adapterkit.StatusPass},
				{Name: "error_paths", Status: adapterkit.StatusPass},
				{Name: "api_compat", Status: adapterkit.StatusPass},
				{Name: "vulncheck", Status: adapterkit.StatusPass},
			},
			MismatchesCount: 0,
		}
		u, err := store.Transition("core", adapterkit.StateVerified, state.TransitionOptions{
			Actor:    "agent-x",
			Evidence: completeEvidence,
		})
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateVerified, u.State)
	})
}
