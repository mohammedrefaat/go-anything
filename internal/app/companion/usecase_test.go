package companion_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefat/go-anything/internal/app/companion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompanion_ToggleEnableDisable(t *testing.T) {
	tempDir := t.TempDir()
	uc := companion.New()

	// 1. Enable unit
	out1, err := uc.Toggle(context.Background(), companion.ToggleInput{
		UnitID: "pkg/auth",
		Dir:    tempDir,
		Enable: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "go", out1.Routing)
	assert.Contains(t, out1.Message, "enabled")

	// 2. Disable unit
	out2, err := uc.Toggle(context.Background(), companion.ToggleInput{
		UnitID: "pkg/auth",
		Dir:    tempDir,
		Enable: false,
	})
	require.NoError(t, err)
	assert.Equal(t, "original", out2.Routing)
	assert.Contains(t, out2.Message, "disabled")

	// 3. Enable specific function
	out3, err := uc.Toggle(context.Background(), companion.ToggleInput{
		UnitID:   "pkg/auth",
		Function: "verify_token",
		Dir:      tempDir,
		Enable:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, "go", out3.Routing)
	assert.Contains(t, out3.Message, "verify_token")
}

func TestCompanion_Refresh(t *testing.T) {
	tempDir := t.TempDir()
	casesDir := filepath.Join(tempDir, ".go-anything", "cases")
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	casesFile := filepath.Join(casesDir, "pkg-crypto.jsonl")
	require.NoError(t, os.WriteFile(casesFile, []byte("{\"id\":\"c1\"}\n{\"id\":\"c2\"}\n"), 0644))

	uc := companion.New()
	out, err := uc.Refresh(context.Background(), companion.RefreshInput{
		UnitID: "pkg/crypto",
		Dir:    tempDir,
	})
	require.NoError(t, err)
	assert.Equal(t, "pkg/crypto", out.UnitID)
	assert.Equal(t, 2, out.CasesRefreshed)
	assert.Equal(t, "clean", out.DiffStatus)
}

func TestCompanion_Clean(t *testing.T) {
	tempDir := t.TempDir()
	bridgeDir := filepath.Join(tempDir, "bridges", "pkg", "billing")
	require.NoError(t, os.MkdirAll(bridgeDir, 0755))

	mainPath := filepath.Join(bridgeDir, "main.go")
	implPath := filepath.Join(bridgeDir, "handlers_impl.go")
	require.NoError(t, os.WriteFile(mainPath, []byte("package main"), 0644))
	require.NoError(t, os.WriteFile(implPath, []byte("package main\n// custom logic"), 0644))

	uc := companion.New()

	// Default clean preserves handlers_impl.go
	out1, err := uc.Clean(context.Background(), companion.CleanInput{
		UnitID: "pkg/billing",
		Dir:    tempDir,
		Force:  false,
	})
	require.NoError(t, err)
	assert.Len(t, out1.FilesRemoved, 1)
	assert.Len(t, out1.PreservedFiles, 1)
	assert.FileExists(t, implPath)
	assert.NoFileExists(t, mainPath)
}

func TestCompanion_Report(t *testing.T) {
	tempDir := t.TempDir()
	uc := companion.New()

	out, err := uc.Report(context.Background(), companion.ReportInput{
		UnitID: "pkg/pricing",
		Dir:    tempDir,
	})
	require.NoError(t, err)
	assert.Equal(t, "pkg/pricing", out.UnitID)
	assert.Equal(t, "clean", out.DiffStatus)
	assert.Contains(t, out.Markdown, "# Migration Report: pkg/pricing")
	assert.Contains(t, out.Markdown, "Benchmark Comparison")
}

func TestCompanion_Map(t *testing.T) {
	tempDir := t.TempDir()
	bridgeDir := filepath.Join(tempDir, "bridges", "pkg", "pricing")
	require.NoError(t, os.MkdirAll(bridgeDir, 0755))

	manifestContent := `
version: "v1alpha1"
unit: "pkg/pricing"
service: "pricing-bridge"
functions:
  - name: "calculate_discount"
    idempotent: true
    returns:
      type: "object"
`
	require.NoError(t, os.WriteFile(filepath.Join(bridgeDir, "bridge.yaml"), []byte(manifestContent), 0644))

	uc := companion.New()
	out, err := uc.Map(context.Background(), companion.MapInput{
		UnitID: "pkg/pricing",
		Dir:    tempDir,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, out.TotalFunctions)
	assert.Equal(t, "calculate_discount", out.Functions[0].Name)
	assert.Equal(t, "scaffolded", out.Functions[0].Status)
}
