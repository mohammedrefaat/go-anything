package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_ProfileHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"profile", "--help"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "go-anything profile")
	assert.Contains(t, out, "--unit, -u")
	assert.Contains(t, out, "--top")
	assert.Contains(t, out, "--dir, -d")
	assert.Contains(t, out, "--json")
	assert.Contains(t, out, "Exit Codes:")
}

func TestCLI_ProfileValidationErrors(t *testing.T) {
	t.Run("missing_unit", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"profile"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "flag --unit (-u) is required")
	})

	t.Run("invalid_flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"profile", "--unit=pkg/test", "--invalid-flag"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
	})
}

func TestCLI_ProfilePythonService(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)
	pyDir := filepath.Join(repoRoot, "testdata", "python_service")

	// 1. Table output test
	t.Run("table_output", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"profile",
			"--unit=core/transform",
			"--top=5",
			"--dir=" + pyDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		out := stdout.String()
		assert.Contains(t, out, `go-anything profile: unit "core/transform"`)
		assert.Contains(t, out, "Language:        python")
		assert.Contains(t, out, "Profiler:        cProfile")
		assert.Contains(t, out, "Rank")
		assert.Contains(t, out, "Function")
		assert.Contains(t, out, "File:Line")
		assert.Contains(t, out, "Self %")
		assert.Contains(t, out, "Cumulative %")
		assert.Contains(t, out, "Pure? (Heuristic)")
		assert.Contains(t, out, "Purity Reason")
		assert.Contains(t, out, "Notice: Purity assessment is a static/runtime heuristic")
	})

	// 2. JSON output and schema validation test
	t.Run("json_schema_validation", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"profile",
			"--unit=core/transform",
			"--top=5",
			"--dir=" + pyDir,
			"--json",
		}, &stdout, &stderr)
		require.NoError(t, err)

		jsonData := stdout.Bytes()
		err = schemas.ValidateV2("profile.report.schema.json", jsonData)
		require.NoError(t, err, "profile --json output must validate against profile.report.schema.json")

		var report struct {
			UnitID     string `json:"unit_id"`
			Language   string `json:"language"`
			Profiler   string `json:"profiler"`
			Top        int    `json:"top"`
			Candidates []struct {
				Rank            int     `json:"rank"`
				FunctionName    string  `json:"function_name"`
				File            string  `json:"file"`
				SelfPercent     float64 `json:"self_percent"`
				IsPureHeuristic bool    `json:"is_pure_heuristic"`
				PurityReason    string  `json:"purity_reason"`
			} `json:"candidates"`
			Summary string `json:"summary"`
		}
		require.NoError(t, json.Unmarshal(jsonData, &report))
		assert.Equal(t, "core/transform", report.UnitID)
		assert.Equal(t, "python", report.Language)
		assert.Equal(t, "cProfile", report.Profiler)
		assert.Equal(t, 5, report.Top)
		assert.NotEmpty(t, report.Candidates)

		// Verify 1-based ranking and purity labeling
		for i, c := range report.Candidates {
			assert.Equal(t, i+1, c.Rank)
			assert.NotEmpty(t, c.FunctionName)
			assert.NotEmpty(t, c.File)
			assert.NotEmpty(t, c.PurityReason)
		}

		// Verify report was persisted under .go-anything/reports/profile_core_transform.json
		reportPath := filepath.Join(pyDir, ".go-anything", "reports", "profile_core_transform.json")
		assert.FileExists(t, reportPath)
	})
}

func TestCLI_ProfileTypeScript(t *testing.T) {
	tempDir := t.TempDir()

	// Setup manifest declaring TypeScript
	manifestDir := filepath.Join(tempDir, ".go-anything")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	manifest := adapterkit.Manifest{
		SchemaVersion:  adapterkit.SchemaVersionV1,
		ProjectName:    "ts-service",
		SourceLanguage: "typescript",
		TargetLanguage: "go",
		Units:          []string{"pkg/tokenizer"},
	}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "manifest.json"), manifestData, 0644))

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{
		"profile",
		"--unit=pkg/tokenizer",
		"--top=5",
		"--dir=" + tempDir,
		"--json",
	}, &stdout, &stderr)
	require.NoError(t, err)

	jsonData := stdout.Bytes()
	err = schemas.ValidateV2("profile.report.schema.json", jsonData)
	require.NoError(t, err, "TypeScript profile output must validate against profile.report.schema.json")

	var report struct {
		UnitID     string `json:"unit_id"`
		Language   string `json:"language"`
		Profiler   string `json:"profiler"`
		Candidates []struct {
			Rank            int    `json:"rank"`
			FunctionName    string `json:"function_name"`
			IsPureHeuristic bool   `json:"is_pure_heuristic"`
			PurityReason    string `json:"purity_reason"`
		} `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(jsonData, &report))
	assert.Equal(t, "pkg/tokenizer", report.UnitID)
	assert.Equal(t, "typescript", report.Language)
	assert.Equal(t, "v8_cpuprofile", report.Profiler)
	assert.NotEmpty(t, report.Candidates)
	assert.Equal(t, 1, report.Candidates[0].Rank)
	assert.Equal(t, "parseTokenStream", report.Candidates[0].FunctionName)
	assert.True(t, report.Candidates[0].IsPureHeuristic)
	assert.Contains(t, report.Candidates[0].PurityReason, "heuristic")
}
