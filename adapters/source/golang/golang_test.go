package golang_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/adapters/source/golang"
	"github.com/mohammedrefat/go-anything/internal/planner"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoSourceAdapter_MultiPackageWavePlanning(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-source-go-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// 1. Create multi-package Go module
	goMod := `module example.com/shop
go 1.22
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644))

	// Package 1: pkg/core (Wave 0 leaf)
	coreDir := filepath.Join(tempDir, "pkg", "core")
	require.NoError(t, os.MkdirAll(coreDir, 0755))
	coreSrc := `package core
type Item struct {
	ID string
}
`
	require.NoError(t, os.WriteFile(filepath.Join(coreDir, "item.go"), []byte(coreSrc), 0644))

	// Package 2: pkg/logger (Wave 0 leaf)
	logDir := filepath.Join(tempDir, "pkg", "logger")
	require.NoError(t, os.MkdirAll(logDir, 0755))
	logSrc := `package logger
func Log(msg string) {}
`
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "log.go"), []byte(logSrc), 0644))

	// Package 3: pkg/store (Wave 1: depends on core and logger)
	storeDir := filepath.Join(tempDir, "pkg", "store")
	require.NoError(t, os.MkdirAll(storeDir, 0755))
	storeSrc := `package store
import (
	"example.com/shop/pkg/core"
	"example.com/shop/pkg/logger"
)
type Store struct {
	Items []core.Item
}
func (s *Store) Save(item core.Item) {
	logger.Log("saved")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(storeDir, "store.go"), []byte(storeSrc), 0644))

	// Package 4: pkg/api (Wave 2: depends on store)
	apiDir := filepath.Join(tempDir, "pkg", "api")
	require.NoError(t, os.MkdirAll(apiDir, 0755))
	apiSrc := `package api
import (
	"example.com/shop/pkg/store"
)
type API struct {
	Store *store.Store
}
`
	require.NoError(t, os.WriteFile(filepath.Join(apiDir, "api.go"), []byte(apiSrc), 0644))

	// 2. Initialize .go-anything store
	wsStore, err := state.InitializeWorkspace(tempDir, "example.com/shop", "go")
	require.NoError(t, err)

	adapter := golang.NewAdapter()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 3. Plan migration
	manifest, plan, err := adapter.PlanMigration(ctx, tempDir, wsStore)
	require.NoError(t, err)
	require.NotNil(t, manifest)
	require.NotNil(t, plan)

	assert.Equal(t, 4, plan.TotalUnits)
	assert.Equal(t, 2, plan.MaxWave)

	// Verify Wave 0 (leaves: core, logger)
	require.Len(t, plan.Waves[0], 2)
	assert.Equal(t, "pkg/core", plan.Waves[0][0].ID)
	assert.Equal(t, "pkg/logger", plan.Waves[0][1].ID)

	// Verify Wave 1 (store)
	require.Len(t, plan.Waves[1], 1)
	assert.Equal(t, "pkg/store", plan.Waves[1][0].ID)
	assert.Contains(t, plan.Waves[1][0].Dependencies, "pkg/core")
	assert.Contains(t, plan.Waves[1][0].Dependencies, "pkg/logger")

	// Verify Wave 2 (api)
	require.Len(t, plan.Waves[2], 1)
	assert.Equal(t, "pkg/api", plan.Waves[2][0].ID)
	assert.Contains(t, plan.Waves[2][0].Dependencies, "pkg/store")

	// 4. Verify durable persistence under .go-anything/units/<unit>.json
	for _, expectedID := range []string{"pkg/core", "pkg/logger", "pkg/store", "pkg/api"} {
		u, err := wsStore.GetUnit(expectedID)
		require.NoError(t, err, "unit %s should be persisted in store", expectedID)
		assert.Equal(t, adapterkit.StatePlanned, u.State)
		assert.Equal(t, adapterkit.ProfileReplay, u.EvidenceProfile)
		assert.NotEmpty(t, u.Metadata["public_apis"])
	}

	// 5. Verify manifest.json contains all units
	savedManifest, err := wsStore.GetManifest()
	require.NoError(t, err)
	assert.Len(t, savedManifest.Units, 4)
}

func TestGoSourceAdapter_ImportCycleHandling(t *testing.T) {
	// A repository graph containing an import cycle:
	// A <-> B (circular dependencies)
	// C depends on B
	// Leaf D has no dependencies
	p := planner.New()
	graph := map[string][]string{
		"pkg/a": {"pkg/b"},
		"pkg/b": {"pkg/a"},
		"pkg/c": {"pkg/b"},
		"pkg/d": {},
	}

	plan := p.PlanWaves(graph)
	require.NotNil(t, plan)

	assert.Equal(t, 4, plan.TotalUnits)
	assert.Len(t, plan.Cycles, 1)
	assert.Equal(t, []string{"pkg/a", "pkg/b"}, plan.Cycles[0])

	// Leaf D is in Wave 0
	// Cycle {pkg/a, pkg/b} has no outside dependencies, so it is in Wave 0!
	require.Len(t, plan.Waves[0], 3)
	// Units in wave 0 sorted deterministically: pkg/a, pkg/b, pkg/d
	assert.Equal(t, "pkg/a", plan.Waves[0][0].ID)
	assert.Equal(t, "pkg/b", plan.Waves[0][1].ID)
	assert.Equal(t, "pkg/d", plan.Waves[0][2].ID)

	assert.True(t, plan.Waves[0][0].Metadata["in_cycle"].(bool))
	assert.True(t, plan.Waves[0][1].Metadata["in_cycle"].(bool))
	assert.False(t, plan.Waves[0][2].Metadata["in_cycle"].(bool))

	// pkg/c depends on cycle, so it is in Wave 1
	require.Len(t, plan.Waves[1], 1)
	assert.Equal(t, "pkg/c", plan.Waves[1][0].ID)
}

func TestGoSourceAdapter_ContractImplementation(t *testing.T) {
	var _ adapterkit.LanguageAdapter = golang.NewAdapter()

	adapter := golang.NewAdapter()
	assert.Equal(t, "golang", adapter.Name())
	assert.Equal(t, "go", adapter.SourceLanguage())
	assert.NotEmpty(t, adapter.Capabilities())
	assert.NotEmpty(t, adapter.SupportedTransports())
}
