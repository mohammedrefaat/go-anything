package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_M1_HTTPReplay verifies the end-to-end recording and replay workflow for an HTTP service.
func TestE2E_M1_HTTPReplay(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-e2e-http-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer

	// 1. Initialize workspace
	err = runCLI([]string{"init", "--source=python", "--name=user-service", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	// 2. Start original HTTP application server
	var uptimeCounter int64
	var reqIDCounter int64
	originalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		reqID := atomic.AddInt64(&reqIDCounter, 1)
		w.Header().Set("X-Request-ID", fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), reqID))
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			val := atomic.AddInt64(&uptimeCounter, 100)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"healthy","uptime_ms":%d}`, val)))
		case "/users/1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1,"name":"Alice","role":"admin"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer originalServer.Close()

	// 3. Record cases from the original application
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"record",
		"--unit=users",
		"--kind=happy",
		"--url=" + originalServer.URL + "/health",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "failed to record health case: %s", stderr.String())

	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"record",
		"--unit=users",
		"--kind=happy",
		"--url=" + originalServer.URL + "/users/1",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "failed to record users case: %s", stderr.String())

	// Verify case files were created in .go-anything/cases/users.jsonl
	caseFile := filepath.Join(tempDir, ".go-anything", "cases", "users.jsonl")
	assert.FileExists(t, caseFile)

	cases, err := loadCasesForUnit(tempDir, "users")
	require.NoError(t, err)
	assert.Len(t, cases, 2)

	// 4. Start ported Go implementation
	// Ported implementation matches the contract and structural behavior
	portedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", fmt.Sprintf("req-ported-%d", time.Now().UnixNano()))
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"healthy","uptime_ms":%d}`, time.Now().UnixNano())))
		case "/users/1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"role":"admin","name":"Alice","id":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer portedServer.Close()

	// 5. Replay cases against the ported Go implementation
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"replay",
		"--unit=users",
		"--target=" + portedServer.URL,
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "replay failed: %s\n%s", stderr.String(), stdout.String())
	assert.Contains(t, stdout.String(), "PASS: 100% replay matched (2/2 cases)")

	// 6. Prove M1 Exit Gate: 100% replay pass across 3 consecutive runs with byte-identical reports!
	var reports [][]byte
	for i := 1; i <= 3; i++ {
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"replay",
			"--unit=users",
			"--target=" + portedServer.URL,
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err, "run %d failed: %s", i, stderr.String())

		reportFile := filepath.Join(tempDir, ".go-anything", "reports", "rep-users-replay.json")
		require.FileExists(t, reportFile)

		data, err := os.ReadFile(reportFile)
		require.NoError(t, err)

		// Normalize variable execution durations for exact byte comparison
		var repMap map[string]any
		require.NoError(t, json.Unmarshal(data, &repMap))
		repMap["timestamp"] = "normalized"
		if resList, ok := repMap["results"].([]any); ok {
			for _, r := range resList {
				if rMap, ok := r.(map[string]any); ok {
					rMap["duration_ms"] = 0
				}
			}
		}
		normData, err := json.MarshalIndent(repMap, "", "  ")
		require.NoError(t, err)

		reports = append(reports, normData)
	}

	// Assert all 3 consecutive runs produce byte-identical reports
	assert.True(t, bytes.Equal(reports[0], reports[1]), "Run 1 and Run 2 reports must be byte-identical")
	assert.True(t, bytes.Equal(reports[1], reports[2]), "Run 2 and Run 3 reports must be byte-identical")
}

// TestE2E_M1_CLIReplay verifies end-to-end recording and replay workflow for a black-box CLI binary.
func TestE2E_M1_CLIReplay(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-e2e-cli-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Build sample original CLI binary
	origSrc := `package main
import (
	"fmt"
	"os"
	"time"
)
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: app <cmd>")
		return
	}
	switch os.Args[1] {
	case "greet":
		fmt.Printf("{\"message\":\"hello %s\",\"timestamp\":\"%s\"}\n", os.Args[2], time.Now().UTC().Format(time.RFC3339Nano))
	case "calc":
		fmt.Println("{\"result\":42,\"status\":\"success\"}")
	default:
		fmt.Println("{\"error\":\"unknown command\"}")
	}
}
`
	origSrcFile := filepath.Join(tempDir, "orig.go")
	require.NoError(t, os.WriteFile(origSrcFile, []byte(origSrc), 0644))

	origBin := filepath.ToSlash(filepath.Join(tempDir, "orig-cli.exe"))
	cmd := exec.Command("go", "build", "-o", origBin, origSrcFile)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to build orig-cli: %s", string(out))

	// Build ported Go CLI binary (returns equivalent structural JSON)
	portedSrc := `package main
import (
	"fmt"
	"os"
	"time"
)
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: app <cmd>")
		return
	}
	switch os.Args[1] {
	case "greet":
		// Matches message, timestamp is volatile
		fmt.Printf("{\"timestamp\":\"%s\",\"message\":\"hello %s\"}\n", time.Now().UTC().Format(time.RFC3339Nano), os.Args[2])
	case "calc":
		// Key order is different; structural JSON comparator handles it
		fmt.Println("{\"status\":\"success\",\"result\":42}")
	default:
		fmt.Println("{\"error\":\"unknown command\"}")
	}
}
`
	portedSrcFile := filepath.Join(tempDir, "ported.go")
	require.NoError(t, os.WriteFile(portedSrcFile, []byte(portedSrc), 0644))

	portedBin := filepath.ToSlash(filepath.Join(tempDir, "ported-cli.exe"))
	cmd = exec.Command("go", "build", "-o", portedBin, portedSrcFile)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "failed to build ported-cli: %s", string(out))

	// 1. Initialize workspace
	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"init", "--name=calculator-cli", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	// 2. Record CLI cases
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"record",
		"--unit=calc",
		"--cmd=" + origBin + " greet alice",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "record greet failed: %s", stderr.String())

	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"record",
		"--unit=calc",
		"--cmd=" + origBin + " calc",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "record calc failed: %s", stderr.String())

	// 3. Replay against ported binary
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"replay",
		"--unit=calc",
		"--target=" + portedBin,
		"--mode=structural_json",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "replay failed: %s\n%s", stderr.String(), stdout.String())
	assert.Contains(t, stdout.String(), "PASS: 100% replay matched (2/2 cases)")

	// 4. Prove 3 consecutive runs produce byte-identical reports
	var repOutputs [][]byte
	for i := 1; i <= 3; i++ {
		stdout.Reset()
		stderr.Reset()
		err = runCLI([]string{
			"replay",
			"--unit=calc",
			"--target=" + portedBin,
			"--mode=structural_json",
			"--dir=" + tempDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		reportFile := filepath.Join(tempDir, ".go-anything", "reports", "rep-calc-replay.json")
		data, err := os.ReadFile(reportFile)
		require.NoError(t, err)

		var repMap map[string]any
		require.NoError(t, json.Unmarshal(data, &repMap))
		repMap["timestamp"] = "normalized"
		if resList, ok := repMap["results"].([]any); ok {
			for _, r := range resList {
				if rMap, ok := r.(map[string]any); ok {
					rMap["duration_ms"] = 0
				}
			}
		}
		norm, _ := json.MarshalIndent(repMap, "", "  ")
		repOutputs = append(repOutputs, norm)
	}

	assert.True(t, bytes.Equal(repOutputs[0], repOutputs[1]))
	assert.True(t, bytes.Equal(repOutputs[1], repOutputs[2]))
}

// TestE2E_M1_StatusReflectsLifecycle verifies status updates through recording and lifecycle.
func TestE2E_M1_StatusReflectsLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-e2e-status-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer

	// Initialize
	err = runCLI([]string{"init", "--name=shop", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	store := state.NewStore(tempDir)
	unit := &adapterkit.Unit{
		ID:              "auth",
		Name:            "Authentication",
		Wave:            0,
		State:           adapterkit.StatePlanned,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(unit))

	// Before record: State is Planned
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"status", tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Planned")

	// Start test server and record
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"record",
		"--unit=auth",
		"--url=" + ts.URL + "/health",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	// After record: Unit automatically advances to Recorded
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"status", tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Recorded")
}
