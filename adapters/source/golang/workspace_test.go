package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupThreeTierWorkspace_Success(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-workspace-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create sample legacy project
	goModContent := `module example.com/sampleapp

go 1.26
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644))

	calcDir := filepath.Join(tempDir, "pkg", "calc")
	require.NoError(t, os.MkdirAll(calcDir, 0755))

	calcGo := `package calc

func Add(a, b int) int {
	return a + b
}
`
	require.NoError(t, os.WriteFile(filepath.Join(calcDir, "calc.go"), []byte(calcGo), 0644))

	calcTestGo := `package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatalf("expected 5")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(calcDir, "calc_test.go"), []byte(calcTestGo), 0644))

	serverDir := filepath.Join(tempDir, "pkg", "server")
	require.NoError(t, os.MkdirAll(serverDir, 0755))

	serverGo := `package server

import (
	"example.com/sampleapp/pkg/calc"
)

func Compute(a, b int) int {
	return calc.Add(a, b) * 2
}
`
	require.NoError(t, os.WriteFile(filepath.Join(serverDir, "server.go"), []byte(serverGo), 0644))

	// Setup three-tier workspace
	ws, err := SetupThreeTierWorkspace(tempDir)
	require.NoError(t, err)
	require.NotNil(t, ws)

	assert.Equal(t, "example.com/sampleapp", ws.ModulePath)
	assert.Equal(t, "1.26", ws.GoVersion)

	// Verify legacy/ structure
	assert.DirExists(t, ws.LegacyDir)
	assert.FileExists(t, filepath.Join(ws.LegacyDir, "go.mod"))
	assert.FileExists(t, filepath.Join(ws.LegacyDir, "pkg", "calc", "calc.go"))
	assert.FileExists(t, filepath.Join(ws.LegacyDir, "pkg", "server", "server.go"))

	legacyModData, err := os.ReadFile(filepath.Join(ws.LegacyDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(legacyModData), "module example.com/sampleapp/legacy")

	legacyServerData, err := os.ReadFile(filepath.Join(ws.LegacyDir, "pkg", "server", "server.go"))
	require.NoError(t, err)
	assert.Contains(t, string(legacyServerData), `"example.com/sampleapp/legacy/pkg/calc"`)

	// Verify modern/ structure
	assert.DirExists(t, ws.ModernDir)
	assert.FileExists(t, filepath.Join(ws.ModernDir, "go.mod"))
	assert.FileExists(t, filepath.Join(ws.ModernDir, "pkg", "calc", "calc.go"))
	assert.FileExists(t, filepath.Join(ws.ModernDir, "pkg", "server", "server.go"))

	modernModData, err := os.ReadFile(filepath.Join(ws.ModernDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(modernModData), "module example.com/sampleapp/modern")

	modernServerData, err := os.ReadFile(filepath.Join(ws.ModernDir, "pkg", "server", "server.go"))
	require.NoError(t, err)
	assert.Contains(t, string(modernServerData), `"example.com/sampleapp/modern/pkg/calc"`)

	// Verify root go.mod and go.work
	rootModData, err := os.ReadFile(filepath.Join(tempDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(rootModData), "replace example.com/sampleapp/legacy => ./legacy")
	assert.Contains(t, string(rootModData), "replace example.com/sampleapp/modern => ./modern")

	rootWorkData, err := os.ReadFile(filepath.Join(tempDir, "go.work"))
	require.NoError(t, err)
	assert.Contains(t, string(rootWorkData), "./legacy")
	assert.Contains(t, string(rootWorkData), "./modern")
}

func TestRunTier1_FormattingAndTests(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-tier1-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	goModContent := `module example.com/tier1app/modern

go 1.26
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644))

	pkgDir := filepath.Join(tempDir, "calc")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))

	// Intentionally poorly formatted Go code
	unformatted := `package calc

func    Sub(  x , y int  ) int {
	return x - y
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "calc.go"), []byte(unformatted), 0644))

	testCode := `package calc

import "testing"

func TestSub(t *testing.T) {
	if Sub(10, 4) != 6 {
		t.Fatalf("expected 6")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "calc_test.go"), []byte(testCode), 0644))

	res, err := RunTier1(context.Background(), tempDir)
	require.NoError(t, err)
	assert.NotNil(t, res)
	assert.NotEmpty(t, res.AppliedChanges)
	assert.True(t, res.TestsChecked)

	// Verify file was cleanly formatted
	formattedData, err := os.ReadFile(filepath.Join(pkgDir, "calc.go"))
	require.NoError(t, err)
	assert.NotEqual(t, unformatted, string(formattedData))
	assert.Contains(t, string(formattedData), "func Sub(x, y int) int {")
}

func TestModernizeWorkspace_EndToEnd(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-mod-e2e-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	goMod := `module example.com/strangler

go 1.26
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644))

	// Package 1: Leaf
	coreDir := filepath.Join(tempDir, "pkg", "core")
	require.NoError(t, os.MkdirAll(coreDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(coreDir, "core.go"), []byte(`package core

func Base() int { return 100 }
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(coreDir, "core_test.go"), []byte(`package core
import "testing"
func TestBase(t *testing.T) {
	if Base() != 100 { t.Fatal("fail") }
}
`), 0644))

	// Package 2: Depends on Core
	apiDir := filepath.Join(tempDir, "pkg", "api")
	require.NoError(t, os.MkdirAll(apiDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(apiDir, "api.go"), []byte(`package api
import "example.com/strangler/pkg/core"
func Compute() int { return core.Base() + 42 }
`), 0644))

	store := state.NewStore(tempDir)
	adapter := NewAdapter()

	res, err := adapter.ModernizeWorkspace(context.Background(), tempDir, store)
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "example.com/strangler", res.Manifest.ProjectName)
	assert.Len(t, res.Manifest.Units, 2)
	assert.Len(t, res.Manifest.Waves, 2)

	// Wave 0 should be pkg/core (leaf)
	assert.Equal(t, 0, res.Manifest.Waves[0].Wave)
	assert.Contains(t, res.Manifest.Waves[0].UnitIDs, "pkg/core")

	// Wave 1 should be pkg/api (depends on core)
	assert.Equal(t, 1, res.Manifest.Waves[1].Wave)
	assert.Contains(t, res.Manifest.Waves[1].UnitIDs, "pkg/api")

	// Verify Planned state files in store
	uCore, err := store.GetUnit("pkg/core")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StatePlanned, uCore.State)
	assert.Equal(t, 0, uCore.Wave)

	uAPI, err := store.GetUnit("pkg/api")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StatePlanned, uAPI.State)
	assert.Equal(t, 1, uAPI.Wave)
	assert.Contains(t, uAPI.Dependencies, "pkg/core")
}
