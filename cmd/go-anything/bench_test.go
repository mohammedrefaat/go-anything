package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_BenchHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"bench", "--help"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "go-anything bench")
	assert.Contains(t, out, "--unit, -u")
	assert.Contains(t, out, "--iterations, -n")
	assert.Contains(t, out, "--warmup")
	assert.Contains(t, out, "--target, -t")
	assert.Contains(t, out, "--dir, -d")
	assert.Contains(t, out, "--json")
	assert.Contains(t, out, "Exit Codes:")
}

func TestCLI_BenchValidationErrors(t *testing.T) {
	t.Run("missing_unit", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"bench"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "flag --unit (-u) is required")
	})

	t.Run("invalid_flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"bench", "--unit=pkg/test", "--invalid-flag"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
	})
}

func setupBenchWorkspace(t *testing.T, unitID string) (string, *httptest.Server) {
	tempDir := t.TempDir()

	casesDir := filepath.Join(tempDir, ".go-anything", "cases", filepath.Dir(unitID))
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	seedCase := adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         "bench-seed-001",
		UnitID:         unitID,
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

	caseFile := filepath.Join(tempDir, ".go-anything", "cases", filepath.FromSlash(unitID)+".jsonl")
	require.NoError(t, os.WriteFile(caseFile, append(seedData, '\n'), 0644))

	// Fast in-memory server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":108.7}`))
	}))

	return tempDir, server
}

func TestCLI_BenchDogfoodService(t *testing.T) {
	tempDir, server := setupBenchWorkspace(t, "pkg/currency")
	defer server.Close()

	// 1. Markdown output test
	t.Run("markdown_table_output", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"bench",
			"--unit=pkg/currency",
			"--target=" + server.URL,
			"--iterations=50",
			"--warmup=5",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		out := stdout.String()
		assert.Contains(t, out, "# Benchmark Report: unit `pkg/currency`")
		assert.Contains(t, out, "## Performance Comparison")
		assert.Contains(t, out, "p50 Latency")
		assert.Contains(t, out, "p95 Latency")
		assert.Contains(t, out, "p99 Latency")
		assert.Contains(t, out, "Throughput")
		assert.Contains(t, out, "## Memory Allocations (Go Implementation)")
		assert.Contains(t, out, "Allocs / Op:")
		assert.Contains(t, out, "Bytes / Op:")
		assert.Contains(t, out, "## Execution Environment & Reproducibility Parameters")
		assert.Contains(t, out, "CPU Model")
		assert.Contains(t, out, "Operating System")
		assert.Contains(t, out, "Go Version")
		assert.Contains(t, out, "Measured Iterations")
		assert.Contains(t, out, "50")
	})

	// 2. JSON output and schema validation test
	t.Run("json_schema_validation", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"bench",
			"--unit=pkg/currency",
			"--target=" + server.URL,
			"--iterations=40",
			"--warmup=5",
			"--dir=" + tempDir,
			"--json",
		}, &stdout, &stderr)
		require.NoError(t, err)

		jsonData := stdout.Bytes()
		err = schemas.ValidateV2("bench.report.schema.json", jsonData)
		require.NoError(t, err, "bench --json output must validate against bench.report.schema.json")

		var report map[string]any
		require.NoError(t, json.Unmarshal(jsonData, &report))
		assert.Equal(t, "pkg/currency", report["unit_id"])
		assert.Equal(t, float64(40), report["iterations"])
		assert.Equal(t, float64(5), report["warmup"])
		assert.NotNil(t, report["original"])
		assert.NotNil(t, report["go"])
		assert.NotNil(t, report["environment"])
		assert.NotEmpty(t, report["summary"])

		// Confirm report was persisted under .go-anything/reports/bench_pkg_currency.json
		reportPath := filepath.Join(tempDir, ".go-anything", "reports", "bench_pkg_currency.json")
		assert.FileExists(t, reportPath)
	})
}

func TestCLI_BenchReproducibility(t *testing.T) {
	tempDir, server := setupBenchWorkspace(t, "pkg/currency")
	defer server.Close()

	var speedups []float64

	for i := 0; i < 3; i++ {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"bench",
			"--unit=pkg/currency",
			"--target=" + server.URL,
			"--iterations=60",
			"--warmup=10",
			"--dir=" + tempDir,
			"--json",
		}, &stdout, &stderr)
		require.NoError(t, err)

		var report struct {
			SpeedupRatio float64 `json:"speedup_ratio"`
			Go           struct {
				P50NS float64 `json:"p50_ns"`
			} `json:"go"`
			Environment struct {
				CPUModel string `json:"cpu_model"`
			} `json:"environment"`
		}
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
		require.NotEmpty(t, report.Environment.CPUModel, "CPU model must be populated for reproducibility")
		speedups = append(speedups, report.SpeedupRatio)
	}

	// Calculate mean speedup
	sum := 0.0
	for _, s := range speedups {
		sum += s
	}
	mean := sum / float64(len(speedups))

	for i, s := range speedups {
		diffRatio := math.Abs(s-mean) / mean
		assert.True(t, diffRatio <= 0.10,
			fmt.Sprintf("run %d speedup %.2f drifted by %.1f%% from mean %.2f (must be within ±10%%)", i+1, s, diffRatio*100, mean))
	}
}
