package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/frontdoor"
	"github.com/mohammedrefat/go-anything/internal/lift"
	"github.com/mohammedrefat/go-anything/internal/planner"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestM7_ExitGate_MultiUnitDependencyWaves validates that multi-unit open-source projects
// decompose cleanly into acyclic topological waves (Wave 0 leaves through Wave 3 entry points).
func TestM7_ExitGate_MultiUnitDependencyWaves(t *testing.T) {
	// Model 6 interdependent units across 4 waves of an open-source task orchestrator
	graph := map[string][]string{
		"pkg/config":    {},
		"pkg/crypto":    {},
		"pkg/storage":   {"pkg/config", "pkg/crypto"},
		"pkg/scheduler": {"pkg/storage"},
		"cmd/api":       {"pkg/scheduler", "pkg/config"},
		"cmd/worker":    {"pkg/scheduler", "pkg/crypto"},
	}

	p := planner.New()
	plan := p.PlanWaves(graph)
	require.NotNil(t, plan)

	assert.Equal(t, 6, plan.TotalUnits)
	assert.Equal(t, 3, plan.MaxWave)
	assert.Empty(t, plan.Cycles, "dependency graph must be strictly acyclic")

	// Helper to extract IDs for a wave
	waveIDs := func(waveNum int) []string {
		var ids []string
		for _, u := range plan.Waves[waveNum] {
			ids = append(ids, u.ID)
		}
		return ids
	}

	// Wave 0: Leaf units with 0 internal dependencies
	w0 := waveIDs(0)
	assert.Contains(t, w0, "pkg/config")
	assert.Contains(t, w0, "pkg/crypto")

	// Wave 1: Persistence layer
	w1 := waveIDs(1)
	assert.Contains(t, w1, "pkg/storage")

	// Wave 2: Orchestration engine
	w2 := waveIDs(2)
	assert.Contains(t, w2, "pkg/scheduler")

	// Wave 3: Entry points and daemon workers
	w3 := waveIDs(3)
	assert.Contains(t, w3, "cmd/api")
	assert.Contains(t, w3, "cmd/worker")
}

// TestM7_ExitGate_SidecarRoutingAndKillSwitch validates emergency kill-switch activation,
// instant fallback to legacy path, and durable fallback count tracking.
func TestM7_ExitGate_SidecarRoutingAndKillSwitch(t *testing.T) {
	tempDir := t.TempDir()
	unitID := "pkg/scheduler"

	// 1. Initially inactive
	assert.False(t, frontdoor.IsUnitKillSwitchActive(tempDir, unitID))
	assert.False(t, frontdoor.IsGlobalKillSwitchActive())

	// 2. Unit-specific kill switch via environment variable
	t.Setenv("GO_ANYTHING_UNIT_PKG_SCHEDULER", "original")
	assert.True(t, frontdoor.IsUnitKillSwitchActive(tempDir, unitID))
	assert.False(t, frontdoor.IsUnitKillSwitchActive(tempDir, "pkg/storage"))

	// 3. Global emergency kill switch
	t.Setenv("GO_ANYTHING_DISABLE_ALL", "true")
	assert.True(t, frontdoor.IsGlobalKillSwitchActive())
	assert.True(t, frontdoor.IsUnitKillSwitchActive(tempDir, "pkg/storage"))

	// 4. Durable fallback counter tracking
	t.Setenv("GO_ANYTHING_DISABLE_ALL", "")
	t.Setenv("GO_ANYTHING_UNIT_PKG_SCHEDULER", "")

	initialCount, err := frontdoor.GetFallbackCount(tempDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), initialCount)

	// Simulate emergency fallback event
	c1, err := frontdoor.IncrementFallback(tempDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), c1)

	c2, err := frontdoor.IncrementFallback(tempDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), c2)

	persistedCount, err := frontdoor.GetFallbackCount(tempDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), persistedCount)
}

// TestM7_ExitGate_LiftPreflightEnforcement validates that lift strictly refuses to proceed
// if fallback traffic is non-zero, evidence is missing, or unit states are incomplete.
func TestM7_ExitGate_LiftPreflightEnforcement(t *testing.T) {
	ctx := context.Background()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"

	// Subtest 1: Blocked on non-zero fallback traffic
	t.Run("blocked on non-zero fallback traffic", func(t *testing.T) {
		tempDir := t.TempDir()
		store := state.NewStore(tempDir)
		require.NoError(t, store.SaveManifest(&adapterkit.Manifest{
			SchemaVersion: adapterkit.SchemaVersionV1,
			ProjectName:   "orchestrator-test",
			Units:         []string{"pkg/config"},
		}))

		unit := &adapterkit.Unit{
			ID:              "pkg/config",
			State:           adapterkit.StateCutover,
			Wave:            0,
			EvidenceProfile: adapterkit.ProfileReplay,
			FallbackCount:   5, // Non-zero fallback!
		}
		require.NoError(t, store.SaveUnit(unit))

		// Valid evidence registered
		require.NoError(t, store.SaveEvidence(&adapterkit.Evidence{
			EvidenceID:      "ev_cfg_01",
			UnitID:          "pkg/config",
			CommitSHA:       commitSHA,
			Profile:         adapterkit.ProfileReplay,
			MismatchesCount: 0,
			Checks: []adapterkit.GateCheck{
				{Name: "replay", Status: adapterkit.StatusPass},
			},
		}))

		engine := lift.NewEngine(lift.Options{
			WorkspaceRoot: tempDir,
			TargetUnits:   []string{"pkg/config"},
			Profile:       adapterkit.ProfileReplay,
			CommitSHA:     commitSHA,
			Actor:         "maintainer",
			SkipBuild:     true,
		})

		res, err := engine.Run(ctx)
		assert.ErrorIs(t, err, lift.ErrLiftBlocked)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		require.NotEmpty(t, res.Blockers)
		assert.Contains(t, res.Blockers[0], "non-zero fallback count")
	})

	// Subtest 2: Blocked on unapproved unit state (e.g. Planned)
	t.Run("blocked on unapproved unit state", func(t *testing.T) {
		tempDir := t.TempDir()
		store := state.NewStore(tempDir)
		require.NoError(t, store.SaveManifest(&adapterkit.Manifest{
			SchemaVersion: adapterkit.SchemaVersionV1,
			ProjectName:   "orchestrator-test",
			Units:         []string{"pkg/storage"},
		}))

		unit := &adapterkit.Unit{
			ID:              "pkg/storage",
			State:           adapterkit.StatePlanned, // Unapproved!
			Wave:            1,
			EvidenceProfile: adapterkit.ProfileReplay,
		}
		require.NoError(t, store.SaveUnit(unit))

		engine := lift.NewEngine(lift.Options{
			WorkspaceRoot: tempDir,
			TargetUnits:   []string{"pkg/storage"},
			Profile:       adapterkit.ProfileReplay,
			CommitSHA:     commitSHA,
			Actor:         "maintainer",
			SkipBuild:     true,
		})

		res, err := engine.Run(ctx)
		assert.ErrorIs(t, err, lift.ErrLiftBlocked)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		require.NotEmpty(t, res.Blockers)
		assert.Contains(t, res.Blockers[0], "must be Verified, Approved, or Cutover")
	})

	// Subtest 3: Blocked on missing commit-tied evidence
	t.Run("blocked on missing evidence", func(t *testing.T) {
		tempDir := t.TempDir()
		store := state.NewStore(tempDir)
		require.NoError(t, store.SaveManifest(&adapterkit.Manifest{
			SchemaVersion: adapterkit.SchemaVersionV1,
			ProjectName:   "orchestrator-test",
			Units:         []string{"pkg/crypto"},
		}))

		unit := &adapterkit.Unit{
			ID:              "pkg/crypto",
			State:           adapterkit.StateCutover,
			Wave:            0,
			EvidenceProfile: adapterkit.ProfileReplay,
			FallbackCount:   0,
		}
		require.NoError(t, store.SaveUnit(unit))

		engine := lift.NewEngine(lift.Options{
			WorkspaceRoot: tempDir,
			TargetUnits:   []string{"pkg/crypto"},
			Profile:       adapterkit.ProfileReplay,
			CommitSHA:     commitSHA, // No evidence matching this commit!
			Actor:         "maintainer",
			SkipBuild:     true,
		})

		res, err := engine.Run(ctx)
		assert.ErrorIs(t, err, lift.ErrLiftBlocked)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		require.NotEmpty(t, res.Blockers)
		assert.Contains(t, res.Blockers[0], "evidence")
	})
}

// TestM7_ExitGate_FullLiftOperation_ZeroFallback validates the end-to-end full strangler lift
// with zero fallback traffic, removing legacy files, and completing all units.
func TestM7_ExitGate_FullLiftOperation_ZeroFallback(t *testing.T) {
	tempDir := t.TempDir()
	commitSHA := "1234567890abcdef1234567890abcdef12345678"

	// 1. Create workspace layout with legacy files and modern Go implementation
	legacyDir := filepath.Join(tempDir, "legacy")
	require.NoError(t, os.MkdirAll(legacyDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "legacy_config.py"), []byte("# legacy python config\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "legacy_storage.py"), []byte("# legacy python storage\n"), 0644))

	modernDir := filepath.Join(tempDir, "pkg", "config")
	require.NoError(t, os.MkdirAll(modernDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(modernDir, "config.go"), []byte("package config\n\nfunc Load() string { return \"prod\" }\n"), 0644))

	// 2. Initialize .go-anything store
	store := state.NewStore(tempDir)
	manifest := &adapterkit.Manifest{
		SchemaVersion: adapterkit.SchemaVersionV1,
		ProjectName:   "oss-orchestrator",
		Units:         []string{"pkg/config", "pkg/storage"},
	}
	require.NoError(t, store.SaveManifest(manifest))

	// 3. Register units in Cutover state with 0 fallback count
	uConfig := &adapterkit.Unit{
		ID:              "pkg/config",
		State:           adapterkit.StateCutover,
		Wave:            0,
		EvidenceProfile: adapterkit.ProfileReplay,
		FallbackCount:   0,
	}
	require.NoError(t, store.SaveUnit(uConfig))

	uStorage := &adapterkit.Unit{
		ID:              "pkg/storage",
		State:           adapterkit.StateCutover,
		Wave:            1,
		EvidenceProfile: adapterkit.ProfileReplay,
		FallbackCount:   0,
		Dependencies:    []string{"pkg/config"},
	}
	require.NoError(t, store.SaveUnit(uStorage))

	// 4. Save valid commit-tied evidence for both units
	require.NoError(t, store.SaveEvidence(&adapterkit.Evidence{
		EvidenceID:      "ev_config_01",
		UnitID:          "pkg/config",
		CommitSHA:       commitSHA,
		Profile:         adapterkit.ProfileReplay,
		MismatchesCount: 0,
		Checks: []adapterkit.GateCheck{
			{Name: "replay", Status: adapterkit.StatusPass},
		},
	}))
	require.NoError(t, store.SaveEvidence(&adapterkit.Evidence{
		EvidenceID:      "ev_storage_01",
		UnitID:          "pkg/storage",
		CommitSHA:       commitSHA,
		Profile:         adapterkit.ProfileReplay,
		MismatchesCount: 0,
		Checks: []adapterkit.GateCheck{
			{Name: "replay", Status: adapterkit.StatusPass},
		},
	}))

	// 5. Execute lift targeting legacy directory
	engine := lift.NewEngine(lift.Options{
		WorkspaceRoot: tempDir,
		TargetUnits:   []string{"pkg/config", "pkg/storage"},
		Profile:       adapterkit.ProfileReplay,
		CommitSHA:     commitSHA,
		Actor:         "lead-maintainer",
		OriginalPaths: []string{"legacy"},
		SkipBuild:     true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := engine.Run(ctx)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success, "lift must succeed")
	assert.Empty(t, res.Blockers)
	assert.ElementsMatch(t, []string{"pkg/config", "pkg/storage"}, res.CompletedUnits)

	// 6. Verify legacy directory was deleted
	assert.NoDirExists(t, legacyDir, "legacy directory must be removed by lift")

	// 7. Verify modern Go code remains intact
	assert.FileExists(t, filepath.Join(modernDir, "config.go"))

	// 8. Verify all units transitioned to Completed state
	finalCfg, err := store.GetUnit("pkg/config")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCompleted, finalCfg.State)

	finalSto, err := store.GetUnit("pkg/storage")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCompleted, finalSto.State)

	// 9. Confirm fallback traffic remained 0
	fbCfg, err := frontdoor.GetFallbackCount(tempDir, "pkg/config")
	require.NoError(t, err)
	assert.Equal(t, int64(0), fbCfg)
}

// TestM7_ExitGate_ReleaseVerification_v1_0_0 asserts that schemas are frozen in schemas/v1/,
// adapterkit API constants indicate v1, and release artifacts are signed with Cosign.
func TestM7_ExitGate_ReleaseVerification_v1_0_0(t *testing.T) {
	// 1. Verify schema freeze in schemas/v1/
	schemasDir, err := filepath.Abs("../../schemas/v1")
	if err != nil || !dirExists(schemasDir) {
		schemasDir, err = filepath.Abs("schemas/v1")
		require.NoError(t, err)
	}
	require.DirExists(t, schemasDir)

	expectedSchemas := []string{
		"manifest.schema.json",
		"units.schema.json",
		"rules.schema.json",
		"cases.schema.json",
		"reports.schema.json",
		"evidence.schema.json",
	}

	for _, sf := range expectedSchemas {
		filePath := filepath.Join(schemasDir, sf)
		require.FileExists(t, filePath)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)

		var schemaMap map[string]any
		require.NoError(t, json.Unmarshal(data, &schemaMap))

		id, ok := schemaMap["$id"].(string)
		require.True(t, ok, "$id must be present in %s", sf)
		assert.Contains(t, id, "https://schemas.go-anything.dev/v1/")
	}

	// 2. Verify adapterkit frozen constants
	assert.Equal(t, "v1", adapterkit.Version)
	assert.Equal(t, "v1", adapterkit.SchemaVersionV1)
	assert.Equal(t, adapterkit.SchemaVersionV1, adapterkit.DefaultSchemaVersion)

	// 3. Verify GoReleaser Cosign signing configuration
	releaserFile, err := filepath.Abs("../../.goreleaser.yaml")
	if err != nil || !fileExists(releaserFile) {
		releaserFile, err = filepath.Abs(".goreleaser.yaml")
		require.NoError(t, err)
	}
	require.FileExists(t, releaserFile)

	data, err := os.ReadFile(releaserFile)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "cosign")
	assert.Contains(t, content, "sign-blob")
	assert.Contains(t, content, "--bundle=${signature}")
	assert.Contains(t, content, "${artifact}.sigstore.json")
}
