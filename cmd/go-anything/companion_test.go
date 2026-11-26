package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/app/companion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCompanionWorkspace(t *testing.T, unitID string) string {
	t.Helper()
	wsDir := t.TempDir()

	dirs := []string{
		filepath.Join(wsDir, ".go-anything", "units"),
		filepath.Join(wsDir, ".go-anything", "cases"),
		filepath.Join(wsDir, "bridges", filepath.FromSlash(unitID)),
	}
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(d, 0755))
	}

	unitJSON := fmt.Sprintf(`{
		"schema_version": "v1alpha1",
		"id": "%s",
		"name": "%s",
		"state": "Verified",
		"evidence_profile": "replay",
		"metadata": {
			"kind": "hotpath",
			"routing": "go"
		}
	}`, unitID, unitID)
	unitPath := filepath.Join(wsDir, ".go-anything", "units", filepath.FromSlash(unitID)+".json")
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0755))
	require.NoError(t, os.WriteFile(unitPath, []byte(unitJSON), 0644))

	casesContent := `{"case_id": "c1", "input": {"x": 1}, "output": {"y": 2}}
{"case_id": "c2", "input": {"x": 2}, "output": {"y": 4}}
`
	casesPath := filepath.Join(wsDir, ".go-anything", "cases", filepath.FromSlash(unitID)+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(casesPath), 0755))
	require.NoError(t, os.WriteFile(casesPath, []byte(casesContent), 0644))

	bridgeDir := filepath.Join(wsDir, "bridges", filepath.FromSlash(unitID))
	manifestContent := fmt.Sprintf(`
version: "v1alpha1"
unit: "%s"
service: "test-bridge"
functions:
  - name: "calculate_tax"
    idempotent: true
    returns:
      type: "object"
`, unitID)
	require.NoError(t, os.WriteFile(filepath.Join(bridgeDir, "bridge.yaml"), []byte(manifestContent), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(bridgeDir, "main.go"), []byte("package main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(bridgeDir, "handlers_impl.go"), []byte("package main\n// Ported Go code"), 0644))

	return wsDir
}

func TestCLI_Enable_And_Disable(t *testing.T) {
	unitID := "pkg/billing"
	wsDir := setupCompanionWorkspace(t, unitID)

	// 1. Disable unit routing
	var stdout1, stderr1 bytes.Buffer
	err := runCLI([]string{
		"disable",
		"--unit=" + unitID,
		"--dir=" + wsDir,
		"--json",
	}, &stdout1, &stderr1)
	require.NoError(t, err)

	var out1 companion.ToggleOutput
	require.NoError(t, json.Unmarshal(stdout1.Bytes(), &out1))
	assert.Equal(t, unitID, out1.UnitID)
	assert.Equal(t, "original", out1.Routing)

	// 2. Enable unit routing
	var stdout2, stderr2 bytes.Buffer
	err = runCLI([]string{
		"enable",
		"--unit=" + unitID,
		"--dir=" + wsDir,
		"--json",
	}, &stdout2, &stderr2)
	require.NoError(t, err)

	var out2 companion.ToggleOutput
	require.NoError(t, json.Unmarshal(stdout2.Bytes(), &out2))
	assert.Equal(t, unitID, out2.UnitID)
	assert.Equal(t, "go", out2.Routing)

	// 3. Disable specific function
	var stdout3, stderr3 bytes.Buffer
	err = runCLI([]string{
		"disable",
		"--unit=" + unitID,
		"--function=calculate_tax",
		"--dir=" + wsDir,
	}, &stdout3, &stderr3)
	require.NoError(t, err)
	assert.Contains(t, stdout3.String(), "calculate_tax")
	assert.Contains(t, stdout3.String(), "routing: original")
}

func TestCLI_Refresh(t *testing.T) {
	unitID := "pkg/billing"
	wsDir := setupCompanionWorkspace(t, unitID)

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{
		"refresh",
		"--unit=" + unitID,
		"--dir=" + wsDir,
		"--json",
	}, &stdout, &stderr)
	require.NoError(t, err)

	var out companion.RefreshOutput
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &out))
	assert.Equal(t, unitID, out.UnitID)
	assert.Equal(t, 2, out.CasesRefreshed)
	assert.Equal(t, "clean", out.DiffStatus)
}

func TestCLI_Clean(t *testing.T) {
	unitID := "pkg/billing"
	wsDir := setupCompanionWorkspace(t, unitID)

	bridgeDir := filepath.Join(wsDir, "bridges", filepath.FromSlash(unitID))
	mainPath := filepath.Join(bridgeDir, "main.go")
	implPath := filepath.Join(bridgeDir, "handlers_impl.go")

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{
		"clean",
		"--unit=" + unitID,
		"--dir=" + wsDir,
		"--json",
	}, &stdout, &stderr)
	require.NoError(t, err)

	var out companion.CleanOutput
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &out))
	assert.Equal(t, unitID, out.UnitID)
	assert.NotEmpty(t, out.FilesRemoved)
	assert.NotEmpty(t, out.PreservedFiles)

	// User code handlers_impl.go preserved, main.go removed
	assert.FileExists(t, implPath)
	assert.NoFileExists(t, mainPath)
}

func TestCLI_Report(t *testing.T) {
	unitID := "pkg/billing"
	wsDir := setupCompanionWorkspace(t, unitID)

	// 1. Markdown report
	var stdoutMD, stderrMD bytes.Buffer
	err := runCLI([]string{
		"report",
		"--unit=" + unitID,
		"--dir=" + wsDir,
	}, &stdoutMD, &stderrMD)
	require.NoError(t, err)
	assert.Contains(t, stdoutMD.String(), "# Migration Report: pkg/billing")
	assert.Contains(t, stdoutMD.String(), "Benchmark Comparison")

	// 2. JSON report
	var stdoutJSON, stderrJSON bytes.Buffer
	err = runCLI([]string{
		"report",
		"--unit=" + unitID,
		"--dir=" + wsDir,
		"--json",
	}, &stdoutJSON, &stderrJSON)
	require.NoError(t, err)

	var rep companion.ReportOutput
	require.NoError(t, json.Unmarshal(stdoutJSON.Bytes(), &rep))
	assert.Equal(t, unitID, rep.UnitID)
	assert.Equal(t, "Verified", rep.State)
	assert.Equal(t, 2, rep.CasesCount)
	assert.Equal(t, "clean", rep.DiffStatus)
	assert.NotNil(t, rep.Benchmark)
}

func TestCLI_Map(t *testing.T) {
	unitID := "pkg/billing"
	wsDir := setupCompanionWorkspace(t, unitID)

	// 1. Table output
	var stdoutTable, stderrTable bytes.Buffer
	err := runCLI([]string{
		"map",
		"--unit=" + unitID,
		"--dir=" + wsDir,
	}, &stdoutTable, &stderrTable)
	require.NoError(t, err)

	outStr := stdoutTable.String()
	assert.Contains(t, outStr, "Function Map for Unit pkg/billing")
	assert.Contains(t, outStr, "calculate_tax")
	assert.Contains(t, outStr, "verified")

	// 2. JSON output
	var stdoutJSON, stderrJSON bytes.Buffer
	err = runCLI([]string{
		"map",
		"--unit=" + unitID,
		"--dir=" + wsDir,
		"--json",
	}, &stdoutJSON, &stderrJSON)
	require.NoError(t, err)

	var mapOut companion.MapOutput
	require.NoError(t, json.Unmarshal(stdoutJSON.Bytes(), &mapOut))
	assert.Equal(t, unitID, mapOut.UnitID)
	assert.Equal(t, 1, mapOut.TotalFunctions)
	assert.Equal(t, "calculate_tax", mapOut.Functions[0].Name)
	assert.Equal(t, "verified", mapOut.Functions[0].Status)
	assert.Equal(t, "go", mapOut.Functions[0].Routing)
}
