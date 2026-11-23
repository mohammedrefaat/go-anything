package main

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCLI_Help_AllFiveCommands verifies all 5 commands run with --help and exit with code 0.
func TestCLI_Help_AllFiveCommands(t *testing.T) {
	cmds := []string{"analyze", "diff", "gate", "shadow", "cutover"}

	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runCLI([]string{cmd, "--help"}, &stdout, &stderr)
			require.NoError(t, err)
			out := stdout.String()
			assert.Contains(t, out, "Usage:")
			assert.Contains(t, out, "go-anything "+cmd)
			assert.Contains(t, out, "Exit Codes:")
			assert.Contains(t, out, "0   Success")
		})
	}
}

// TestCLI_ExitCodes verifies the 4 standard exit codes (0 ok, 1 check fail, 2 usage error, 3 internal error).
func TestCLI_ExitCodes(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-exitcodes-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// 1. Exit code 2: Usage errors
	t.Run("UsageError_UnknownCommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"nonexistent-command"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
	})

	t.Run("UsageError_InvalidGlobalFlag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"--jobs=invalid", "version"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
	})

	t.Run("UsageError_AnalyzeMissingFlagVal", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"analyze", "--source"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
	})

	t.Run("UsageError_DiffMissingUnit", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"diff", "--target=http://localhost:8080"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "flag --unit (-u) is required")
	})

	t.Run("UsageError_DiffMissingTarget", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"diff", "--unit=auth"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "flag --target (-t) is required")
	})

	t.Run("UsageError_GateMissingUnit", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"gate"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "flag --unit (-u) is required")
	})

	t.Run("UsageError_ShadowMissingUnit", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"shadow", "status"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "flag --unit (-u) is required")
	})

	t.Run("UsageError_ShadowInvalidAction", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"shadow", "invalid-action", "--unit=auth"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "invalid shadow action")
	})

	t.Run("UsageError_ShadowInvalidPort", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"shadow", "start", "--unit=auth", "--port=999999"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "invalid port")
	})

	t.Run("UsageError_ShadowInvalidSampleRate", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"shadow", "start", "--unit=auth", "--sample-rate=2.5"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "invalid sample-rate")
	})

	t.Run("UsageError_CutoverMissingUnit", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"cutover", "status"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "flag --unit (-u) is required")
	})

	t.Run("UsageError_CutoverInvalidAction", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI([]string{"cutover", "invalid-action", "--unit=auth"}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeUsage, GetExitCode(err))
		assert.Contains(t, err.Error(), "invalid cutover action")
	})

	// 2. Exit code 3: Internal error (e.g. invalid directory)
	t.Run("InternalError_DiffWorkspaceNotFound", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		nonExistentDir := filepath.Join(tempDir, "nonexistent-dir")
		err := runCLI([]string{"diff", "--unit=auth", "--target=http://localhost:8080", "--dir=" + nonExistentDir}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeInternal, GetExitCode(err))
	})
}

// TestCLI_RuleR1_AST verifies that cmd/go-anything/cutover.go never imports internal/lift or performs lift.
func TestCLI_RuleR1_AST(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "cutover.go", nil, parser.ImportsOnly)
	require.NoError(t, err)

	for _, imp := range node.Imports {
		pathVal := strings.Trim(imp.Path.Value, `"`)
		assert.NotContains(t, pathVal, "internal/lift", "Rule R-1 VIOLATION: cutover.go must never import internal/lift")
	}
}

// TestCLI_DogfoodService_EndToEnd verifies all 5 commands work against testdata/dogfood_service
// and their JSON outputs strictly validate against schemas/v2/.
func TestCLI_DogfoodService_EndToEnd(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-dogfood-h1-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	srcDir, err := filepath.Abs("../../testdata/dogfood_service")
	if err != nil || !dirExists(srcDir) {
		srcDir, err = filepath.Abs("testdata/dogfood_service")
		require.NoError(t, err)
	}

	require.NoError(t, copyDir(srcDir, tempDir))
	initGitRepo(t, tempDir)

	var stdout, stderr bytes.Buffer

	// Step 0: Initialize workspace via modernize
	err = runCLI([]string{"modernize", tempDir}, &stdout, &stderr)
	require.NoError(t, err, "stderr: %s", stderr.String())

	// Step 1: Test analyze command (both table and JSON)
	t.Run("analyze", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		err := runCLI([]string{"analyze", "--dir=" + tempDir}, &stdout, &stderr)
		require.NoError(t, err, "stderr: %s", stderr.String())
		out := stdout.String()
		assert.Contains(t, out, "go-anything analyze:")
		assert.Contains(t, out, "Topological Waves:")
		assert.Contains(t, out, "pkg/currency")
		assert.Contains(t, out, "pkg/fraud")
		assert.Contains(t, out, "pkg/billing")
		assert.Contains(t, out, "cmd/server")

		// Analyze with --output flag
		outFile := filepath.Join(tempDir, "analysis_output.txt")
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{"analyze", "--output=" + outFile, "--dir=" + tempDir}, &stdout, &stderr)
		require.NoError(t, err)
		assert.FileExists(t, outFile)

		// Analyze with --json flag and validate against schemas/v2/analyze.output.schema.json
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{"analyze", "--json", "--dir=" + tempDir}, &stdout, &stderr)
		require.NoError(t, err)
		jsonData := stdout.Bytes()

		err = schemas.ValidateV2("analyze.output.schema.json", jsonData)
		require.NoError(t, err, "analyze JSON must validate against analyze.output.schema.json")

		var parsed map[string]any
		require.NoError(t, json.Unmarshal(jsonData, &parsed))
		assert.Equal(t, "v2", parsed["schema_version"])
		assert.Equal(t, "go", parsed["source_language"])
	})

	// Step 2: Setup HTTP test server and recorded cases for diff & gate
	casesDir := filepath.Join(tempDir, ".go-anything", "cases")
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	matchingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amountStr := r.URL.Query().Get("amount")
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")

		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid amount parameter"})
			return
		}
		if amount < 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "amount cannot be negative"})
			return
		}
		if from == "XYZ" || to == "XYZ" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unsupported currency code: XYZ"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"amount":    amount,
			"from":      from,
			"to":        to,
			"result":    108.7,
			"formatted": "$108.70",
		})
	}))
	defer matchingServer.Close()

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
				Path:   "/convert?amount=100&from=EUR&to=USD",
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: http.StatusOK,
				Body: map[string]any{
					"amount":    float64(100),
					"from":      "EUR",
					"to":        "USD",
					"result":    float64(108.7),
					"formatted": "$108.70",
				},
			},
		},
		{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "case-curr-0002",
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
	}

	casesFile := filepath.Join(casesDir, "currency.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Join(casesDir, "pkg"), 0755))
	casesFilePkg := filepath.Join(casesDir, "pkg", "currency.jsonl")
	var casesBuf bytes.Buffer
	for _, c := range recordedCases {
		data, err := json.Marshal(c)
		require.NoError(t, err)
		casesBuf.Write(data)
		casesBuf.WriteString("\n")
	}
	require.NoError(t, os.WriteFile(casesFile, casesBuf.Bytes(), 0644))
	require.NoError(t, os.WriteFile(casesFilePkg, casesBuf.Bytes(), 0644))

	store := state.NewStore(tempDir)
	_, err = store.Transition("pkg/currency", adapterkit.StateRecorded, state.TransitionOptions{})
	require.NoError(t, err)

	// Step 3: Test diff command
	t.Run("diff_match", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		err := runCLI([]string{
			"diff",
			"--unit=pkg/currency",
			"--target=" + matchingServer.URL,
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err, "stderr: %s", stderr.String())
		out := stdout.String()
		assert.Contains(t, out, "go-anything diff:")
		assert.Contains(t, out, "Matched: 2")
		assert.Contains(t, out, "Mismatched: 0")
		assert.Contains(t, out, "PASS: 100% replay matched")

		// JSON diff output and schema validation
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"diff",
			"--unit=pkg/currency",
			"--target=" + matchingServer.URL,
			"--dir=" + tempDir,
			"--json",
		}, &stdout, &stderr)
		require.NoError(t, err)
		diffJSON := stdout.Bytes()

		err = schemas.ValidateV2("diff.output.schema.json", diffJSON)
		require.NoError(t, err, "diff JSON must validate against diff.output.schema.json")
	})

	t.Run("diff_mismatch_exitcode1", func(t *testing.T) {
		mismatchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"amount": 999, // Intentional mismatch
			})
		}))
		defer mismatchServer.Close()

		stdout.Reset()
		stderr.Reset()
		err := runCLI([]string{
			"diff",
			"--unit=pkg/currency",
			"--target=" + mismatchServer.URL,
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.Error(t, err)
		assert.Equal(t, ExitCodeFail, GetExitCode(err))
	})

	// Step 4: Test gate command
	t.Run("gate_replay_profile", func(t *testing.T) {
		// Set target in unit metadata so gate knows how to replay
		u, err := store.GetUnit("pkg/currency")
		require.NoError(t, err)
		if u.Metadata == nil {
			u.Metadata = make(map[string]any)
		}
		u.Metadata["target"] = matchingServer.URL
		require.NoError(t, store.SaveUnit(u))

		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"gate",
			"--unit=pkg/currency",
			"--profile=replay",
			"--scorecard",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err, "stderr: %s", stderr.String())
		out := stdout.String()
		assert.Contains(t, out, "go-anything gate: unit \"pkg/currency\"")
		assert.Contains(t, out, "[PASS] replay")
		assert.Contains(t, out, "Scorecard generated at:")

		// Gate with --json flag and validate against schemas/v2/gate.output.schema.json
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"gate",
			"--unit=pkg/currency",
			"--profile=replay",
			"--json",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)
		gateJSON := stdout.Bytes()

		err = schemas.ValidateV2("gate.output.schema.json", gateJSON)
		require.NoError(t, err, "gate JSON must validate against gate.output.schema.json")
	})

	// Step 5: Test shadow command (status, start, stop)
	t.Run("shadow_subcommands", func(t *testing.T) {
		// 1. shadow status
		stdout.Reset()
		stderr.Reset()
		err := runCLI([]string{
			"shadow",
			"status",
			"--unit=pkg/currency",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)
		assert.Contains(t, stdout.String(), "go-anything shadow: unit \"pkg/currency\" (action: status)")

		// 2. shadow start with JSON
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"shadow",
			"start",
			"--unit=pkg/currency",
			"--port=8080",
			"--sample-rate=0.75",
			"--json",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)
		startJSON := stdout.Bytes()

		err = schemas.ValidateV2("shadow.output.schema.json", startJSON)
		require.NoError(t, err, "shadow start JSON must validate against shadow.output.schema.json")

		var startOut map[string]any
		require.NoError(t, json.Unmarshal(startJSON, &startOut))
		assert.Equal(t, true, startOut["active"])
		assert.Equal(t, "start", startOut["action"])
		assert.Equal(t, float64(8080), startOut["port"])

		// 3. shadow stop with JSON
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"shadow",
			"stop",
			"--unit=pkg/currency",
			"--json",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)
		stopJSON := stdout.Bytes()

		err = schemas.ValidateV2("shadow.output.schema.json", stopJSON)
		require.NoError(t, err, "shadow stop JSON must validate against shadow.output.schema.json")

		var stopOut map[string]any
		require.NoError(t, json.Unmarshal(stopJSON, &stopOut))
		assert.Equal(t, false, stopOut["active"])
		assert.Equal(t, "stop", stopOut["action"])
	})

	// Step 6: Test cutover command (plan, status, kill)
	t.Run("cutover_subcommands", func(t *testing.T) {
		// 1. cutover plan
		stdout.Reset()
		stderr.Reset()
		err := runCLI([]string{
			"cutover",
			"plan",
			"--unit=pkg/currency",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)
		assert.Contains(t, stdout.String(), "go-anything cutover: unit \"pkg/currency\" (action: plan)")

		// 2. cutover status with JSON
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"cutover",
			"status",
			"--unit=pkg/currency",
			"--json",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)
		statusJSON := stdout.Bytes()

		err = schemas.ValidateV2("cutover.output.schema.json", statusJSON)
		require.NoError(t, err, "cutover status JSON must validate against cutover.output.schema.json")

		// 3. cutover kill with JSON
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"cutover",
			"kill",
			"--unit=pkg/currency",
			"--json",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)
		killJSON := stdout.Bytes()

		err = schemas.ValidateV2("cutover.output.schema.json", killJSON)
		require.NoError(t, err, "cutover kill JSON must validate against cutover.output.schema.json")

		var killOut map[string]any
		require.NoError(t, json.Unmarshal(killJSON, &killOut))
		assert.Equal(t, "kill", killOut["action"])
		assert.Equal(t, true, killOut["kill_switch_active"])
		assert.Equal(t, "original", killOut["route_state"])
	})
}
