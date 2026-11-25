package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_FuzzHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"fuzz", "--help"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "go-anything fuzz")
	assert.Contains(t, out, "--unit, -u")
	assert.Contains(t, out, "--time, -t")
	assert.Contains(t, out, "--runs, -r")
	assert.Contains(t, out, "--seed")
	assert.Contains(t, out, "--max-input-bytes")
	assert.Contains(t, out, "--promote-findings")
	assert.Contains(t, out, "--dir, -d")
	assert.Contains(t, out, "--json")
	assert.Contains(t, out, "Exit Codes:")
	assert.Contains(t, out, "Rule R-3: divergence blocks gate")
}

func TestCLI_FuzzValidationErrors(t *testing.T) {
	t.Run("missing_unit", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"fuzz"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "flag --unit (-u) is required")
	})

	t.Run("invalid_flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"fuzz", "--unit=pkg/test", "--invalid-flag"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
	})
}

func TestCLI_FuzzDogfoodService(t *testing.T) {
	tempDir := t.TempDir()

	// Initialize workspace layout
	casesDir := filepath.Join(tempDir, ".go-anything", "cases", "pkg")
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	seedCase := adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         "dogfood-curr-001",
		UnitID:         "pkg/currency",
		Kind:           adapterkit.KindHappy,
		SourceRevision: "v1.0.0",
		Runtime:        "go",
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Method: "GET",
			Path:   "/convert?amount=100&from=EUR&to=USD",
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: http.StatusOK,
			Body: map[string]any{
				"result": 108.7,
			},
		},
	}
	seedData, err := json.Marshal(seedCase)
	require.NoError(t, err)

	caseFile := filepath.Join(casesDir, "currency.jsonl")
	require.NoError(t, os.WriteFile(caseFile, append(seedData, '\n'), 0644))

	// Run fuzz command without target (pure mutation / zero divergence baseline)
	t.Run("table_output", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"fuzz",
			"--unit=pkg/currency",
			"--runs=10",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		out := stdout.String()
		assert.Contains(t, out, `go-anything fuzz: unit "pkg/currency"`)
		assert.Contains(t, out, "Total Runs:        10")
		assert.Contains(t, out, "Divergences:       0")
		assert.Contains(t, out, "Status:            PASS")
	})

	// Run fuzz command with --json and validate against schema
	t.Run("json_schema_validation", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"fuzz",
			"--unit=pkg/currency",
			"--runs=15",
			"--dir=" + tempDir,
			"--json",
		}, &stdout, &stderr)
		require.NoError(t, err)

		jsonData := stdout.Bytes()
		err = schemas.ValidateV2("fuzz.report.schema.json", jsonData)
		require.NoError(t, err, "fuzz --json output must validate against fuzz.report.schema.json")

		var report map[string]any
		require.NoError(t, json.Unmarshal(jsonData, &report))
		assert.Equal(t, "pkg/currency", report["unit_id"])
		assert.Equal(t, float64(15), report["total_runs"])
		assert.Equal(t, float64(0), report["divergences_count"])
		assert.Equal(t, true, report["passed"])

		// Verify report persisted to .go-anything/reports/fuzz_pkg_currency.json
		reportFile := filepath.Join(tempDir, ".go-anything", "reports", "fuzz_pkg_currency.json")
		assert.FileExists(t, reportFile)
	})
}

func TestCLI_FuzzPythonService(t *testing.T) {
	tempDir := t.TempDir()

	casesDir := filepath.Join(tempDir, ".go-anything", "cases", "core")
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	pyCase := adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         "py-validator-001",
		UnitID:         "core/validator",
		Kind:           adapterkit.KindHappy,
		SourceRevision: "v0.2.0",
		Runtime:        "python3.10",
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Body: `{"email": "user@example.com", "age": 25}`,
		},
		ExpectedOutput: adapterkit.CaseOutput{
			ExitCode: 0,
			Stdout:   `{"valid": true}`,
		},
	}
	pyData, err := json.Marshal(pyCase)
	require.NoError(t, err)

	caseFile := filepath.Join(casesDir, "validator.jsonl")
	require.NoError(t, os.WriteFile(caseFile, append(pyData, '\n'), 0644))

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{
		"fuzz",
		"--unit=core/validator",
		"--runs=20",
		"--dir=" + tempDir,
		"--json",
	}, &stdout, &stderr)
	require.NoError(t, err)

	jsonData := stdout.Bytes()
	err = schemas.ValidateV2("fuzz.report.schema.json", jsonData)
	require.NoError(t, err)

	var report map[string]any
	require.NoError(t, json.Unmarshal(jsonData, &report))
	assert.Equal(t, "core/validator", report["unit_id"])
	assert.Equal(t, float64(20), report["total_runs"])
	assert.Equal(t, true, report["passed"])
}

func TestCLI_FuzzDivergenceAndPromotion(t *testing.T) {
	tempDir := t.TempDir()

	casesDir := filepath.Join(tempDir, ".go-anything", "cases")
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	seedCase := adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         "seed-001",
		UnitID:         "unit-diverge",
		Kind:           adapterkit.KindHappy,
		SourceRevision: "v1.0.0",
		Runtime:        "go",
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Method: "GET",
			Path:   "/test",
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: http.StatusOK,
			Body: map[string]any{
				"status": "expected-legacy",
			},
		},
	}
	seedData, err := json.Marshal(seedCase)
	require.NoError(t, err)

	caseFile := filepath.Join(casesDir, "unit-diverge.jsonl")
	require.NoError(t, os.WriteFile(caseFile, append(seedData, '\n'), 0644))

	// Mock server that deliberately produces diverging responses
	divergentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "divergent-modern-output",
		})
	}))
	defer divergentServer.Close()

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{
		"fuzz",
		"--unit=unit-diverge",
		"--target=" + divergentServer.URL,
		"--runs=5",
		"--promote-findings",
		"--dir=" + tempDir,
		"--json",
	}, &stdout, &stderr)

	// Rule R-3: divergence blocks gate -> exit code 1
	require.Error(t, err)
	assert.Equal(t, ExitCodeFail, GetExitCode(err))

	jsonData := stdout.Bytes()
	err = schemas.ValidateV2("fuzz.report.schema.json", jsonData)
	require.NoError(t, err)

	var report map[string]any
	require.NoError(t, json.Unmarshal(jsonData, &report))
	assert.Equal(t, "unit-diverge", report["unit_id"])
	assert.Equal(t, false, report["passed"])
	assert.Greater(t, report["divergences_count"], float64(0))
	assert.Greater(t, report["promoted_count"], float64(0))

	// Confirm promoted findings were appended to .go-anything/cases/unit-diverge.jsonl with kind "fuzz"
	content, err := os.ReadFile(caseFile)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	assert.Greater(t, len(lines), 1, "divergent cases should be promoted to corpus")

	var lastCase adapterkit.Case
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &lastCase))
	assert.Equal(t, adapterkit.KindFuzz, lastCase.Kind, "promoted cases must have Kind 'fuzz'")
	assert.Contains(t, lastCase.CaseID, "fuzz-unit-diverge")
}
