package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_ModernizeCommand_Success(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-cli-mod-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set up legacy Go repository
	goMod := `module example.com/ecommerce

go 1.26
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644))

	// Unit 1: pkg/calc (leaf)
	calcDir := filepath.Join(tempDir, "pkg", "calc")
	require.NoError(t, os.MkdirAll(calcDir, 0755))

	unformattedCalc := `package calc

func   Add( a , b int ) int {
	return a + b
}
`
	require.NoError(t, os.WriteFile(filepath.Join(calcDir, "calc.go"), []byte(unformattedCalc), 0644))

	calcTest := `package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(3, 4) != 7 {
		t.Fatalf("expected 7")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(calcDir, "calc_test.go"), []byte(calcTest), 0644))

	// Unit 2: pkg/gateway (depends on calc)
	gwDir := filepath.Join(tempDir, "pkg", "gateway")
	require.NoError(t, os.MkdirAll(gwDir, 0755))

	gwCode := `package gateway

import "example.com/ecommerce/pkg/calc"

func ProcessOrder(items []int) int {
	total := 0
	for _, item := range items {
		total = calc.Add(total, item)
	}
	return total
}
`
	require.NoError(t, os.WriteFile(filepath.Join(gwDir, "gateway.go"), []byte(gwCode), 0644))

	// Execute `go-anything modernize <dir>`
	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"modernize", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Initialized empty go-anything workspace")
	assert.Contains(t, out, "Source language: go")
	assert.Contains(t, out, "Target language: go")
	assert.Contains(t, out, "Three-tier:      legacy/ (frozen original) and modern/ (working copy)")
	assert.Contains(t, out, "Tier 1:          gofmt:")
	assert.Contains(t, out, "Tier 1:          tests: verified existing unit tests pass cleanly")
	assert.Contains(t, out, "Planned units:   2 registered across 2 waves")

	// Verify disk structure: legacy/, modern/, root go.mod, root go.work
	assert.DirExists(t, filepath.Join(tempDir, "legacy"))
	assert.DirExists(t, filepath.Join(tempDir, "modern"))
	assert.FileExists(t, filepath.Join(tempDir, "go.work"))
	assert.FileExists(t, filepath.Join(tempDir, "go.mod"))

	// Verify .go-anything store
	store := state.NewStore(tempDir)
	manifest, err := store.GetManifest()
	require.NoError(t, err)
	assert.Equal(t, "example.com/ecommerce", manifest.ProjectName)
	assert.Len(t, manifest.Units, 2)
	assert.Len(t, manifest.Waves, 2)

	// Verify units in Planned state
	uCalc, err := store.GetUnit("pkg/calc")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StatePlanned, uCalc.State)
	assert.Equal(t, 0, uCalc.Wave)

	uGW, err := store.GetUnit("pkg/gateway")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StatePlanned, uGW.State)
	assert.Equal(t, 1, uGW.Wave)
	assert.Contains(t, uGW.Dependencies, "pkg/calc")

	// Verify status command displays both units
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"status", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	statusOut := stdout.String()
	assert.Contains(t, statusOut, "pkg/calc")
	assert.Contains(t, statusOut, "pkg/gateway")
	assert.Contains(t, statusOut, "Planned")
}

func TestCLI_InitSourceGo(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-cli-init-go-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	goMod := `module example.com/inittest

go 1.26
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644))

	mathDir := filepath.Join(tempDir, "pkg", "math")
	require.NoError(t, os.MkdirAll(mathDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mathDir, "math.go"), []byte(`package math

func Square(x int) int { return x * x }
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mathDir, "math_test.go"), []byte(`package math
import "testing"
func TestSquare(t *testing.T) {
	if Square(4) != 16 { t.Fatal("fail") }
}
`), 0644))

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"init", "--source=go", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Three-tier:      legacy/ (frozen original) and modern/ (working copy)")
	assert.Contains(t, out, "Planned units:   1 registered across 1 waves")

	assert.DirExists(t, filepath.Join(tempDir, "legacy"))
	assert.DirExists(t, filepath.Join(tempDir, "modern"))

	store := state.NewStore(tempDir)
	uMath, err := store.GetUnit("pkg/math")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StatePlanned, uMath.State)
}
