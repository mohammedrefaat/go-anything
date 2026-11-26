package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/adapters/source/golang/facade"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDogfood_PaymentsServiceMigration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-dogfood-payments-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// 1. Copy testdata/dogfood_service into tempDir
	srcDir, err := filepath.Abs("../../testdata/dogfood_service")
	if err != nil || !dirExists(srcDir) {
		srcDir, err = filepath.Abs("testdata/dogfood_service")
		require.NoError(t, err)
	}

	require.NoError(t, copyDir(srcDir, tempDir))

	// 2. Initialize git repository for commit-tied evidence
	initGitRepo(t, tempDir)

	// 3. Execute modernization: 'go-anything modernize <dir>'
	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"modernize", tempDir}, &stdout, &stderr)
	require.NoError(t, err, "stderr: %s", stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "Initialized empty go-anything workspace")
	assert.Contains(t, out, "Project: ")
	assert.Contains(t, out, "Three-tier:      legacy/ (frozen original) and modern/ (working copy)")
	assert.Contains(t, out, "Tier 1:          gofmt:")
	assert.Contains(t, out, "Planned units:   4 registered across 3 waves")

	// Verify workspace structure
	assert.DirExists(t, filepath.Join(tempDir, "legacy"))
	assert.DirExists(t, filepath.Join(tempDir, "modern"))
	assert.FileExists(t, filepath.Join(tempDir, "go.work"))
	assert.FileExists(t, filepath.Join(tempDir, "go.mod"))

	store := state.NewStore(tempDir)
	manifest, err := store.GetManifest()
	require.NoError(t, err)
	assert.Equal(t, "example.com/payments", manifest.ProjectName)
	assert.Len(t, manifest.Units, 4)

	// Verify Wave 0 leaf units
	uCurrency, err := store.GetUnit("pkg/currency")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StatePlanned, uCurrency.State)
	assert.Equal(t, 0, uCurrency.Wave)
	assert.Empty(t, uCurrency.Dependencies)

	uFraud, err := store.GetUnit("pkg/fraud")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StatePlanned, uFraud.State)
	assert.Equal(t, 0, uFraud.Wave)

	// Verify Wave 1 and Wave 2 units
	uBilling, err := store.GetUnit("pkg/billing")
	require.NoError(t, err)
	assert.Equal(t, 1, uBilling.Wave)

	uServer, err := store.GetUnit("cmd/server")
	require.NoError(t, err)
	assert.Equal(t, 2, uServer.Wave)

	// 4. Setup facade and start target HTTP service with the ported unit
	facadeRegistry := facade.NewHTTPRegistry(4, 50)
	defer facadeRegistry.Close()

	// Handler serving /convert using the ported currency logic
	convertHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amountStr := r.URL.Query().Get("amount")
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")

		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid amount"})
			return
		}

		rates := map[string]float64{
			"USD": 1.0,
			"EUR": 0.92,
			"GBP": 0.79,
			"JPY": 155.0,
		}

		rateFrom, okFrom := rates[from]
		rateTo, okTo := rates[to]
		if amount < 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "amount cannot be negative"})
			return
		}
		if !okFrom || !okTo {
			bad := from
			if !okTo {
				bad = to
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprintf("unsupported currency code: %s", bad)})
			return
		}

		result := math.Round((amount/rateFrom*rateTo)*100) / 100
		formatted := fmt.Sprintf("%.2f %s", result, to)
		if to == "EUR" {
			formatted = fmt.Sprintf("€%.2f", result)
		} else if to == "GBP" {
			formatted = fmt.Sprintf("£%.2f", result)
		} else if to == "USD" {
			formatted = fmt.Sprintf("$%.2f", result)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"amount":    amount,
			"from":      from,
			"to":        to,
			"result":    result,
			"formatted": formatted,
		})
	})

	facadeRegistry.RegisterRoute("/convert", convertHandler, convertHandler, facade.ModeNew)
	server := httptest.NewServer(facadeRegistry)
	defer server.Close()

	// 5. Record behavioral test cases into .go-anything/cases/pkg/currency.jsonl
	casesDir := filepath.Join(tempDir, ".go-anything", "cases", "pkg")
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	recordedCases := []*adapterkit.Case{
		{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "case-curr-0001",
			UnitID:         "pkg/currency",
			Kind:           adapterkit.KindHappy,
			SourceRevision: "v1.0.0",
			Runtime:        "go",
			CapturedAt:     time.Now().UTC().Format(time.RFC3339),
			Input: adapterkit.CaseInput{
				Method: "GET",
				Path:   "/convert?amount=100&from=USD&to=EUR",
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: http.StatusOK,
				Body: map[string]any{
					"amount":    float64(100),
					"from":      "USD",
					"to":        "EUR",
					"result":    float64(92),
					"formatted": "€92.00",
				},
			},
		},
		{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "case-curr-0002",
			UnitID:         "pkg/currency",
			Kind:           adapterkit.KindHappy,
			SourceRevision: "v1.0.0",
			Runtime:        "go",
			CapturedAt:     time.Now().UTC().Format(time.RFC3339),
			Input: adapterkit.CaseInput{
				Method: "GET",
				Path:   "/convert?amount=50&from=USD&to=GBP",
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: http.StatusOK,
				Body: map[string]any{
					"amount":    float64(50),
					"from":      "USD",
					"to":        "GBP",
					"result":    float64(39.5),
					"formatted": "£39.50",
				},
			},
		},
		{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "case-curr-0003",
			UnitID:         "pkg/currency",
			Kind:           adapterkit.KindEdge,
			SourceRevision: "v1.0.0",
			Runtime:        "go",
			CapturedAt:     time.Now().UTC().Format(time.RFC3339),
			Input: adapterkit.CaseInput{
				Method: "GET",
				Path:   "/convert?amount=10&from=EUR&to=USD",
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: http.StatusOK,
				Body: map[string]any{
					"amount":    float64(10),
					"from":      "EUR",
					"to":        "USD",
					"result":    float64(10.87),
					"formatted": "$10.87",
				},
			},
		},
		{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "case-curr-0004",
			UnitID:         "pkg/currency",
			Kind:           adapterkit.KindTrap,
			SourceRevision: "v1.0.0",
			Runtime:        "go",
			CapturedAt:     time.Now().UTC().Format(time.RFC3339),
			Input: adapterkit.CaseInput{
				Method: "GET",
				Path:   "/convert?amount=-5&from=USD&to=EUR",
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: http.StatusBadRequest,
				Body: map[string]any{
					"error": "amount cannot be negative",
				},
			},
		},
		{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "case-curr-0005",
			UnitID:         "pkg/currency",
			Kind:           adapterkit.KindEdge,
			SourceRevision: "v1.0.0",
			Runtime:        "go",
			CapturedAt:     time.Now().UTC().Format(time.RFC3339),
			Input: adapterkit.CaseInput{
				Method: "GET",
				Path:   "/convert?amount=100&from=XYZ&to=USD",
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: http.StatusBadRequest,
				Body: map[string]any{
					"error": "unsupported currency code: XYZ",
				},
			},
		},
	}

	casesFile := filepath.Join(casesDir, "currency.jsonl")
	var casesBuffer bytes.Buffer
	for _, c := range recordedCases {
		line, err := json.Marshal(c)
		require.NoError(t, err)
		casesBuffer.Write(line)
		casesBuffer.WriteString("\n")
	}
	require.NoError(t, os.WriteFile(casesFile, casesBuffer.Bytes(), 0644))

	// Transition unit to Recorded state
	_, err = store.Transition("pkg/currency", adapterkit.StateRecorded, state.TransitionOptions{})
	require.NoError(t, err)

	uRecorded, err := store.GetUnit("pkg/currency")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateRecorded, uRecorded.State)

	// 6. Verify unit using 'go-anything verify pkg/currency --profile=replay --target=...'
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"verify",
		"pkg/currency",
		"--profile=replay",
		"--target=" + server.URL,
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "stderr: %s", stderr.String())

	verifyOut := stdout.String()
	assert.Contains(t, verifyOut, `PASS: Unit "pkg/currency" successfully verified under profile "replay".`)
	assert.Contains(t, verifyOut, "State advanced:")
	assert.Contains(t, verifyOut, "-> Verified")

	// 7. Check unit state is now Verified
	uVerified, err := store.GetUnit("pkg/currency")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateVerified, uVerified.State)

	// 8. Confirm evidence file is saved and valid
	evidenceList, err := store.ListEvidenceForUnit("pkg/currency")
	require.NoError(t, err)
	require.NotEmpty(t, evidenceList)
	assert.Equal(t, 0, evidenceList[0].MismatchesCount)
	assert.Equal(t, adapterkit.ProfileReplay, evidenceList[0].Profile)

	// 9. Verify 'go-anything status' reflects Verified state
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"status", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	statusOut := stdout.String()
	assert.Contains(t, statusOut, "pkg/currency")
	assert.Contains(t, statusOut, "Verified")
	assert.Contains(t, statusOut, "Summary: 0 Completed · 0 Cutover · 0 Approved · 0 Shadow · 1 Verified · 0 In progress · 0 Ready · 3 Planned · 0 Blocked")
}

func initGitRepo(t *testing.T, dir string) {
	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "go-anything-bot")
	runCmd(t, dir, "git", "config", "user.email", "bot@go-anything.local")
	runCmd(t, dir, "git", "add", ".")
	runCmd(t, dir, "git", "commit", "-m", "initial commit of legacy service")
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
