package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefat/go-anything/internal/app/hotpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_Go_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"go", "--help"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "go-anything go")
	assert.Contains(t, out, "--unit, -u")
	assert.Contains(t, out, "--function, -f")
	assert.Contains(t, out, "--dir, -d")
	assert.Contains(t, out, "--json")
	assert.Contains(t, out, "Exit Codes:")
	assert.Contains(t, out, "0   Success / paused waiting for developer implementation")
	assert.Contains(t, out, "1   Pipeline verification check failed")
	assert.Contains(t, out, "2   Usage error")
	assert.Contains(t, out, "3   Internal execution error")
}

func TestCLI_Go_FlagsValidation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"go"}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, ExitCodeUsage, GetExitCode(err))
	assert.Contains(t, err.Error(), "flag --unit (-u) is required")
}

func setupHotpathWorkspace(t *testing.T, unitID string) string {
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

	manifestJSON := fmt.Sprintf(`{
		"schema_version": "v1alpha1",
		"project_name": "hotpath-test",
		"source_language": "python",
		"target_language": "go",
		"units": ["%s"],
		"waves": [{"wave": 1, "unit_ids": ["%s"]}]
	}`, unitID, unitID)
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, ".go-anything", "manifest.json"), []byte(manifestJSON), 0644))

	return wsDir
}

func TestCLI_Go_PipelinePauseOnUnimplemented(t *testing.T) {
	unitID := "pkg/tax"
	wsDir := setupHotpathWorkspace(t, unitID)

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{
		"go",
		"--unit=" + unitID,
		"--function=calculate_tax",
		"--dir=" + wsDir,
	}, &stdout, &stderr)

	require.NoError(t, err)
	out := stdout.String()

	assert.Contains(t, out, "go-anything hot-path orchestrator: pkg/tax (function: calculate_tax)")
	assert.Contains(t, out, "Pipeline Status: paused_for_implementation")
	assert.Contains(t, out, "[OK]    target")
	assert.Contains(t, out, "[OK]    record")
	assert.Contains(t, out, "[OK]    bridge")
	assert.Contains(t, out, "[PAUSE] implement")
	assert.Contains(t, out, "Pipeline paused waiting for developer implementation")
	assert.Contains(t, out, "bridges/pkg/tax/handlers_impl.go")
}

func TestCLI_Go_PipelineCompletesToHumanBoundaryWhenImplemented(t *testing.T) {
	unitID := "pkg/tax"
	wsDir := setupHotpathWorkspace(t, unitID)

	// Run 1: initial run scaffolds bridge and pauses
	var stdout1, stderr1 bytes.Buffer
	err := runCLI([]string{
		"go",
		"--unit=" + unitID,
		"--function=calculate_tax",
		"--dir=" + wsDir,
	}, &stdout1, &stderr1)
	require.NoError(t, err)

	// Developer provides Go implementation in handlers_impl.go (does not return ErrNotPorted)
	implPath := filepath.Join(wsDir, "bridges", filepath.FromSlash(unitID), "handlers_impl.go")
	workingImpl := `package main

import (
	"context"
	"errors"
)

var ErrNotPorted = errors.New("ErrNotPorted")

type HandlersImpl struct{}

func NewHandlers() Handlers {
	return &HandlersImpl{}
}

func (h *HandlersImpl) CalculateTax(ctx context.Context, req *CalculateTaxRequest) (*CalculateTaxResponse, error) {
	return &CalculateTaxResponse{Result: map[string]any{"tax": 10.0}}, nil
}
`
	require.NoError(t, os.WriteFile(implPath, []byte(workingImpl), 0644))

	// Run 2: resume pipeline
	var stdout2, stderr2 bytes.Buffer
	err = runCLI([]string{
		"go",
		"--unit=" + unitID,
		"--dir=" + wsDir,
	}, &stdout2, &stderr2)
	require.NoError(t, err)

	out := stdout2.String()
	assert.Contains(t, out, "Pipeline Status: ready_for_approval")
	assert.Contains(t, out, "Lifecycle State: Shadow")
	assert.Contains(t, out, "[SKIP]  target")
	assert.Contains(t, out, "[SKIP]  record")
	assert.Contains(t, out, "[SKIP]  bridge")
	assert.Contains(t, out, "[OK]    implement")
	assert.Contains(t, out, "[OK]    replay_diff")
	assert.Contains(t, out, "[OK]    fuzz_bench")
	assert.Contains(t, out, "[OK]    gate")
	assert.Contains(t, out, "[OK]    shadow")
	assert.Contains(t, out, "Human sign-off required (Rule R-1):")
	assert.Contains(t, out, "Run: go-anything approve --unit pkg/tax")
}

func TestCLI_Go_JSONOutput(t *testing.T) {
	unitID := "pkg/pricing"
	wsDir := setupHotpathWorkspace(t, unitID)

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{
		"go",
		"--unit=" + unitID,
		"--function=calculate_price",
		"--dir=" + wsDir,
		"--json",
	}, &stdout, &stderr)

	require.NoError(t, err)

	var report hotpath.Output
	err = json.Unmarshal(stdout.Bytes(), &report)
	require.NoError(t, err, "stdout must be valid JSON: %s", stdout.String())

	assert.Equal(t, "v2", report.SchemaVersion)
	assert.Equal(t, unitID, report.UnitID)
	assert.Equal(t, "calculate_price", report.Function)
	assert.Equal(t, hotpath.PipelineStatusPausedForImplementation, report.Status)
	assert.NotEmpty(t, report.Steps)
	assert.Contains(t, report.HumanActionRequired, "bridges/pkg/pricing/handlers_impl.go")
}
