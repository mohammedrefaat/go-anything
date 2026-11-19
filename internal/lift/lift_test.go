package lift_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/frontdoor"
	"github.com/mohammedrefat/go-anything/internal/lift"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testCommitSHA  = "0123456789abcdef0123456789abcdef01234567"
	staleCommitSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func setupTestWorkspace(t *testing.T, unitID string, unitState adapterkit.UnitState, commitSHA string, fallbackCount int64) (string, *state.Store) {
	t.Helper()
	wsDir := t.TempDir()

	store, err := state.InitializeWorkspace(wsDir, "test-project", "python")
	require.NoError(t, err)

	// Create unit
	unit := &adapterkit.Unit{
		SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
		ID:              unitID,
		Name:            unitID,
		State:           unitState,
		EvidenceProfile: adapterkit.ProfileReplay,
		FallbackCount:   int(fallbackCount),
		Metadata: map[string]any{
			"original_path": "legacy/original.py",
			"bridge_path":   "bridge/bridge.go",
		},
	}
	require.NoError(t, store.SaveUnit(unit))

	// Create manifest
	manifest := &adapterkit.Manifest{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		ProjectName:   "test-project",
		Units:         []string{unitID},
	}
	require.NoError(t, store.SaveManifest(manifest))

	// If commitSHA provided, record valid replay evidence
	if commitSHA != "" {
		ev := &adapterkit.Evidence{
			SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
			EvidenceID:      "ev-" + unitID,
			UnitID:          unitID,
			CommitSHA:       commitSHA,
			Profile:         adapterkit.ProfileReplay,
			VerifiedAt:      time.Now().UTC().Format(time.RFC3339),
			VerifiedBy:      "test-verifier",
			MismatchesCount: 0,
			Checks: []adapterkit.GateCheck{
				{
					Name:   "replay",
					Status: adapterkit.StatusPass,
				},
			},
		}
		require.NoError(t, store.SaveEvidence(ev))
	}

	// If fallbackCount > 0, set it in durable frontdoor storage
	if fallbackCount > 0 {
		for i := int64(0); i < fallbackCount; i++ {
			_, err := frontdoor.IncrementFallback(wsDir, unitID)
			require.NoError(t, err)
		}
	}

	// Create sample project files: ported, original, and bridge
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, "legacy"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "legacy", "original.py"), []byte("print('legacy')\n"), 0644))

	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, "bridge"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "bridge", "bridge.go"), []byte("package bridge\n"), 0644))

	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, "ported"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "ported", "main.go"), []byte("package main\n"), 0644))

	return wsDir, store
}

func TestLift_RefusesNonZeroFallback(t *testing.T) {
	unitID := "cli-calc"
	wsDir, store := setupTestWorkspace(t, unitID, adapterkit.StateCutover, testCommitSHA, 3)

	opts := lift.Options{
		WorkspaceRoot: wsDir,
		CommitSHA:     testCommitSHA,
		SkipBuild:     true,
	}

	engine := lift.NewEngine(opts)
	res, err := engine.Run(context.Background())

	require.Error(t, err)
	assert.True(t, errors.Is(err, lift.ErrLiftBlocked))
	require.NotNil(t, res)
	assert.False(t, res.Success)
	assert.NotEmpty(t, res.Blockers)
	assert.Contains(t, res.Blockers[0], "non-zero fallback count")

	// Verify workspace remained untouched
	assert.FileExists(t, filepath.Join(wsDir, "legacy", "original.py"))
	assert.FileExists(t, filepath.Join(wsDir, "bridge", "bridge.go"))
	assert.FileExists(t, filepath.Join(wsDir, "ported", "main.go"))

	u, err := store.GetUnit(unitID)
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCutover, u.State)
}

func TestLift_RefusesStaleOrMissingEvidence(t *testing.T) {
	unitID := "api-users"
	// Evidence recorded for staleCommitSHA, but lift target is testCommitSHA
	wsDir, store := setupTestWorkspace(t, unitID, adapterkit.StateCutover, staleCommitSHA, 0)

	opts := lift.Options{
		WorkspaceRoot: wsDir,
		CommitSHA:     testCommitSHA,
		SkipBuild:     true,
	}

	engine := lift.NewEngine(opts)
	res, err := engine.Run(context.Background())

	require.Error(t, err)
	assert.True(t, errors.Is(err, lift.ErrLiftBlocked))
	require.NotNil(t, res)
	assert.False(t, res.Success)
	assert.NotEmpty(t, res.Blockers)
	assert.Contains(t, res.Blockers[0], "missing valid evidence")

	// Workspace untouched
	assert.FileExists(t, filepath.Join(wsDir, "legacy", "original.py"))
	assert.FileExists(t, filepath.Join(wsDir, "bridge", "bridge.go"))

	u, err := store.GetUnit(unitID)
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCutover, u.State)
}

func TestLift_RefusesReplayFailureInCopy(t *testing.T) {
	unitID := "http-auth"
	wsDir, store := setupTestWorkspace(t, unitID, adapterkit.StateCutover, testCommitSHA, 0)

	opts := lift.Options{
		WorkspaceRoot: wsDir,
		CommitSHA:     testCommitSHA,
		SkipBuild:     true,
		ReplayRunner: func(ctx context.Context, copyDir string, units []*adapterkit.Unit) error {
			return errors.New("simulated replay mismatch on case #12")
		},
	}

	engine := lift.NewEngine(opts)
	res, err := engine.Run(context.Background())

	require.Error(t, err)
	assert.True(t, errors.Is(err, lift.ErrLiftBlocked))
	require.NotNil(t, res)
	assert.False(t, res.Success)
	assert.NotEmpty(t, res.Blockers)
	assert.Contains(t, res.Blockers[0], "simulated replay mismatch")

	// Workspace untouched
	assert.FileExists(t, filepath.Join(wsDir, "legacy", "original.py"))
	assert.FileExists(t, filepath.Join(wsDir, "bridge", "bridge.go"))

	u, err := store.GetUnit(unitID)
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCutover, u.State)
}

func TestLift_RefusesBuildFailureInCopy(t *testing.T) {
	unitID := "billing-service"
	wsDir, store := setupTestWorkspace(t, unitID, adapterkit.StateCutover, testCommitSHA, 0)

	opts := lift.Options{
		WorkspaceRoot: wsDir,
		CommitSHA:     testCommitSHA,
		SkipBuild:     false,
		BuildRunner: func(ctx context.Context, copyDir string) (string, error) {
			return "undefined reference to legacy bridge in ported/main.go", errors.New("exit status 1")
		},
	}

	engine := lift.NewEngine(opts)
	res, err := engine.Run(context.Background())

	require.Error(t, err)
	assert.True(t, errors.Is(err, lift.ErrLiftBlocked))
	require.NotNil(t, res)
	assert.False(t, res.Success)
	assert.NotEmpty(t, res.Blockers)
	assert.Contains(t, res.Blockers[0], "build failed in copy")

	// Workspace untouched
	assert.FileExists(t, filepath.Join(wsDir, "legacy", "original.py"))
	assert.FileExists(t, filepath.Join(wsDir, "bridge", "bridge.go"))

	u, err := store.GetUnit(unitID)
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCutover, u.State)
}

func TestLift_SucceedsEndToEnd(t *testing.T) {
	unitID := "inventory-service"
	wsDir, store := setupTestWorkspace(t, unitID, adapterkit.StateCutover, testCommitSHA, 0)

	replayInvoked := false
	buildInvoked := false

	opts := lift.Options{
		WorkspaceRoot: wsDir,
		CommitSHA:     testCommitSHA,
		ReplayRunner: func(ctx context.Context, copyDir string, units []*adapterkit.Unit) error {
			replayInvoked = true
			// Verify this is running inside temporary copy, not the real workspace
			assert.NotEqual(t, wsDir, copyDir)
			assert.FileExists(t, filepath.Join(copyDir, "legacy", "original.py"))
			return nil
		},
		BuildRunner: func(ctx context.Context, copyDir string) (string, error) {
			buildInvoked = true
			// In copy, bridge and original should have been removed
			assert.NoFileExists(t, filepath.Join(copyDir, "legacy", "original.py"))
			assert.NoFileExists(t, filepath.Join(copyDir, "bridge", "bridge.go"))
			assert.FileExists(t, filepath.Join(copyDir, "ported", "main.go"))
			return "build ok", nil
		},
	}

	engine := lift.NewEngine(opts)
	res, err := engine.Run(context.Background())

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)
	assert.Empty(t, res.Blockers)
	assert.Equal(t, []string{unitID}, res.CompletedUnits)
	assert.True(t, replayInvoked)
	assert.True(t, buildInvoked)

	// Workspace has had bridges and originals cleanly removed
	assert.NoFileExists(t, filepath.Join(wsDir, "legacy", "original.py"))
	assert.NoFileExists(t, filepath.Join(wsDir, "bridge", "bridge.go"))
	assert.FileExists(t, filepath.Join(wsDir, "ported", "main.go"))

	// Unit in store is marked Completed
	u, err := store.GetUnit(unitID)
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCompleted, u.State)
}

func TestLift_LifecycleTransitionFromApproved(t *testing.T) {
	unitID := "orders-service"
	wsDir, store := setupTestWorkspace(t, unitID, adapterkit.StateApproved, testCommitSHA, 0)

	opts := lift.Options{
		WorkspaceRoot: wsDir,
		CommitSHA:     testCommitSHA,
		SkipBuild:     true,
	}

	engine := lift.NewEngine(opts)
	res, err := engine.Run(context.Background())

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)
	assert.Equal(t, []string{unitID}, res.CompletedUnits)

	u, err := store.GetUnit(unitID)
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCompleted, u.State)
}
