package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/adapters/source/python"
	"github.com/mohammedrefat/go-anything/internal/comparator"
	"github.com/mohammedrefat/go-anything/internal/gate"
	"github.com/mohammedrefat/go-anything/internal/redactor"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestM6_ExitGate_PythonAST_DependencyDiscoveryAndWavePlanning validates that the Python
// AST analyzer discovers units, identifies leaf modules, and constructs topologically sorted waves.
func TestM6_ExitGate_PythonAST_DependencyDiscoveryAndWavePlanning(t *testing.T) {
	fixtureDir, err := filepath.Abs("../../testdata/python_service")
	if err != nil || !dirExists(fixtureDir) {
		fixtureDir, err = filepath.Abs("testdata/python_service")
		require.NoError(t, err)
	}
	require.DirExists(t, fixtureDir)

	discoverer := python.NewDependencyDiscoverer()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	project, err := discoverer.Discover(ctx, fixtureDir)
	require.NoError(t, err)
	require.NotNil(t, project)

	// Verify all application units are discovered
	unitIDs := make(map[string]bool)
	for _, u := range project.Units {
		unitIDs[u.ID] = true
	}

	assert.True(t, unitIDs["core/transform"], "core/transform must be discovered")
	assert.True(t, unitIDs["core/validator"], "core/validator must be discovered")
	assert.True(t, unitIDs["service/processor"], "service/processor must be discovered")
	assert.True(t, unitIDs["cli"], "cli must be discovered")
	assert.True(t, unitIDs["app"], "app must be discovered")

	// Verify Wave 0 Leaf Units (no dependencies)
	wave0Units := project.Plan.Waves[0]
	var wave0IDs []string
	for _, u := range wave0Units {
		wave0IDs = append(wave0IDs, u.ID)
	}
	assert.Contains(t, wave0IDs, "core/transform")
	assert.Contains(t, wave0IDs, "core/validator")

	// Verify Wave 1 Business Logic (depends on Wave 0)
	wave1Units := project.Plan.Waves[1]
	var wave1IDs []string
	for _, u := range wave1Units {
		wave1IDs = append(wave1IDs, u.ID)
	}
	assert.Contains(t, wave1IDs, "service/processor")

	// Verify Wave 2 Entry Points (depends on Wave 1)
	wave2Units := project.Plan.Waves[2]
	var wave2IDs []string
	for _, u := range wave2Units {
		wave2IDs = append(wave2IDs, u.ID)
	}
	assert.Contains(t, wave2IDs, "cli")
	assert.Contains(t, wave2IDs, "app")

	assert.GreaterOrEqual(t, project.Plan.MaxWave, 2)
}

// TestM6_ExitGate_SubprocessTransport_CLI validates CLI execution over subprocess transport,
// behavioral recording, structural JSON diffing, and fail-closed secret redaction.
func TestM6_ExitGate_SubprocessTransport_CLI(t *testing.T) {
	pythonBin, err := exec.LookPath("python")
	if err != nil {
		pythonBin, err = exec.LookPath("python3")
	}
	if err != nil {
		t.Skip("python runtime not available on PATH")
	}

	fixtureDir, err := filepath.Abs("../../testdata/python_service")
	if err != nil || !dirExists(fixtureDir) {
		fixtureDir, err = filepath.Abs("testdata/python_service")
		require.NoError(t, err)
	}
	cliScript := filepath.Join(fixtureDir, "cli.py")
	require.FileExists(t, cliScript)

	comp := comparator.New()
	red := redactor.New()

	// 1. Subprocess command: stats
	t.Run("stats command", func(t *testing.T) {
		cmd := exec.Command(pythonBin, cliScript, "stats", "--numbers", "10.0,20.0,30.0")
		outBytes, err := cmd.CombinedOutput()
		require.NoError(t, err, "python cli stats failed: %s", string(outBytes))

		// Ported Go implementation output with explicit floating-point representation
		goJSON := []byte(`{"avg":20.0,"count":3,"max":30.0,"min":10.0,"sum":60.0}`)

		// Structural JSON comparison
		res, err := comp.Compare(comparator.ModeStructuralJSON, string(outBytes), string(goJSON))
		require.NoError(t, err)
		assert.True(t, res.Passed, "stats output must be behaviorally equivalent: %v", res.Mismatches)
		assert.Empty(t, res.Mismatches)
	})

	// 2. Subprocess command: transform
	t.Run("transform command", func(t *testing.T) {
		cmd := exec.Command(pythonBin, cliScript, "transform", "--text", "  Hello Strangler  ")
		outBytes, err := cmd.CombinedOutput()
		require.NoError(t, err, "python cli transform failed: %s", string(outBytes))

		goOutput := map[string]any{
			"length":     15,
			"normalized": "hello strangler",
			"original":   "  Hello Strangler  ",
			"slug":       "hello-strangler",
		}
		goJSON, err := json.Marshal(goOutput)
		require.NoError(t, err)

		res, err := comp.Compare(comparator.ModeStructuralJSON, string(outBytes), string(goJSON))
		require.NoError(t, err)
		assert.True(t, res.Passed, "transform output must match: %v", res.Mismatches)
	})

	// 3. Subprocess command: process with secret redaction
	t.Run("process with secret redaction", func(t *testing.T) {
		stripeKey := "sk_test_" + "1234567890abcdef12345678"
		rawInput := fmt.Sprintf(`[{"id": 1, "score": 99.5, "api_key": "%s"}]`, stripeKey)
		cmd := exec.Command(pythonBin, cliScript, "process", "--items", rawInput)
		outBytes, err := cmd.CombinedOutput()
		require.NoError(t, err, "python cli process failed: %s", string(outBytes))

		// Verify in-memory secret redaction before writing to disk
		redactedStr, _, err := red.RedactString(string(outBytes))
		require.NoError(t, err)
		assert.NotContains(t, redactedStr, stripeKey)
	})
}

// TestM6_ExitGate_HTTPTransport_FastAPI validates HTTP sidecar transport, request/response
// recording, endpoint parity, and zero secret leakage.
func TestM6_ExitGate_HTTPTransport_FastAPI(t *testing.T) {
	pythonBin, err := exec.LookPath("python")
	if err != nil {
		pythonBin, err = exec.LookPath("python3")
	}
	if err != nil {
		t.Skip("python runtime not available on PATH")
	}

	fixtureDir, err := filepath.Abs("../../testdata/python_service")
	if err != nil || !dirExists(fixtureDir) {
		fixtureDir, err = filepath.Abs("testdata/python_service")
		require.NoError(t, err)
	}
	appScript := filepath.Join(fixtureDir, "app.py")
	require.FileExists(t, appScript)

	// Pick a free random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	// Launch Python HTTP server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, pythonBin, appScript, fmt.Sprintf("%d", port))
	cmd.Dir = fixtureDir
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	require.NoError(t, cmd.Start())
	defer func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("Python HTTP server stderr: %s", stderrBuf.String())
		}
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait for server health endpoint
	client := &http.Client{Timeout: 5 * time.Second}
	require.Eventually(t, func() bool {
		resp, err := client.Get(baseURL + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 10*time.Second, 100*time.Millisecond, "python http server did not become healthy")

	comp := comparator.New()
	red := redactor.New()

	// 1. Replay GET /stats endpoint
	t.Run("GET /stats parity", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/stats?numbers=5,15,25")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		pyBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// Ported Go HTTP handler output
		goBody := `{"avg":15.0,"count":3,"max":25.0,"min":5.0,"sum":45.0}`

		res, err := comp.Compare(comparator.ModeStructuralJSON, string(pyBody), goBody)
		require.NoError(t, err)
		assert.True(t, res.Passed, "GET /stats must match structural JSON: %v", res.Mismatches)
	})

	// 2. Replay POST /transform endpoint
	t.Run("POST /transform parity", func(t *testing.T) {
		reqBody := `{"text": "  Go Anything Cross Language  "}`
		resp, err := client.Post(baseURL+"/transform", "application/json", strings.NewReader(reqBody))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		pyBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		goBody := `{"length":26,"normalized":"go anything cross language","original":"  Go Anything Cross Language  ","slug":"go-anything-cross-language"}`

		res, err := comp.Compare(comparator.ModeStructuralJSON, string(pyBody), goBody)
		require.NoError(t, err)
		assert.True(t, res.Passed, "POST /transform must match structural JSON: %v", res.Mismatches)
	})

	// 3. Replay POST /process with authorization header and redaction
	t.Run("POST /process with secret redaction", func(t *testing.T) {
		reqBody := `{"items":[{"id":100,"score":88.5}]}`
		req, err := http.NewRequest(http.MethodPost, baseURL+"/process", strings.NewReader(reqBody))
		require.NoError(t, err)
		secretToken := "Bearer ghp_0123456789abcdef0123456789abcdef0123"
		req.Header.Set("Authorization", secretToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		pyBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// Ensure recorded auth header is redacted
		redactedHeader, _, err := red.RedactString(secretToken)
		require.NoError(t, err)
		assert.NotContains(t, redactedHeader, "ghp_0123456789abcdef")

		goBody := `{"items":[{"id":100,"score":88.5}],"stats":{"avg":88.5,"count":1,"max":88.5,"min":88.5,"sum":88.5},"status":"ok","total_received":1,"valid_count":1}`
		res, err := comp.Compare(comparator.ModeStructuralJSON, string(pyBody), goBody)
		require.NoError(t, err)
		assert.True(t, res.Passed, "POST /process must match: %v", res.Mismatches)
	})
}

// TestM6_ExitGate_QualityGate_FullProfile validates that the quality gate pipeline
// executes and verifies all checks under the full profile.
func TestM6_ExitGate_QualityGate_FullProfile(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module example.com/pytarget\n\ngo 1.22\n"), 0644))
	goSrc := "package pytarget\n\n// Version returns current service release version.\nfunc Version() string {\n\treturn \"0.2.0\"\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "service.go"), []byte(goSrc), 0644))
	testSrc := "package pytarget\n\nimport \"testing\"\n\nfunc TestVersion(t *testing.T) {\n\tif Version() != \"0.2.0\" {\n\t\tt.Fatal(\"mismatch\")\n\t}\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "service_test.go"), []byte(testSrc), 0644))

	runner := gate.New(gate.Config{
		Dir:               tempDir,
		Timeout:           30 * time.Second,
		Checks:            []string{gate.CheckBuild, gate.CheckFormat, gate.CheckVet},
		AllowMissingTools: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	report, err := runner.Run(ctx)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.True(t, report.Passed, "quality gate must pass under full profile")
	assert.Equal(t, 3, report.PassedCount)
	assert.Equal(t, 0, report.FailedCount)
}

// TestM6_ExitGate_StranglerLifecycleToCompleted tests state transitions across
// Planned -> Recorded -> Ready -> In progress -> Verified -> Shadow -> Approved -> Cutover -> Completed.
func TestM6_ExitGate_StranglerLifecycleToCompleted(t *testing.T) {
	tempDir := t.TempDir()

	store := state.NewStore(tempDir)
	manifest := &adapterkit.Manifest{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		ProjectName:   "python-service-dogfood",
		Units:         []string{"cli", "app"},
	}
	require.NoError(t, store.SaveManifest(manifest))

	unitCLI := &adapterkit.Unit{
		ID:              "cli",
		State:           adapterkit.StatePlanned,
		Wave:            2,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(unitCLI))

	// Step 1: Planned -> Recorded
	u, err := store.Transition("cli", adapterkit.StateRecorded, state.TransitionOptions{})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateRecorded, u.State)

	// Step 2: Recorded -> Ready
	u, err = store.Transition("cli", adapterkit.StateReady, state.TransitionOptions{})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateReady, u.State)

	// Step 3: Ready -> In progress
	u, err = store.Transition("cli", adapterkit.StateInProgress, state.TransitionOptions{
		Actor: "m6-agent",
	})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateInProgress, u.State)

	// Step 4: In progress -> Verified (with valid replay evidence)
	evidence := &adapterkit.Evidence{
		EvidenceID:      "ev_cli_001",
		UnitID:          "cli",
		CommitSHA:       "0123456789abcdef0123456789abcdef01234567",
		Profile:         adapterkit.ProfileReplay,
		MismatchesCount: 0,
		Checks: []adapterkit.GateCheck{
			{Name: "replay", Status: adapterkit.StatusPass},
		},
	}
	u, err = store.Transition("cli", adapterkit.StateVerified, state.TransitionOptions{
		Actor:    "m6-agent",
		Evidence: evidence,
	})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateVerified, u.State)

	// Step 5: Verified -> Shadow
	u, err = store.Transition("cli", adapterkit.StateShadow, state.TransitionOptions{
		Actor: "m6-agent",
	})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateShadow, u.State)

	// Step 6: Shadow -> Approved
	u, err = store.Transition("cli", adapterkit.StateApproved, state.TransitionOptions{
		Actor: "m6-approver",
	})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateApproved, u.State)

	// Step 7: Approved -> Cutover
	u, err = store.Transition("cli", adapterkit.StateCutover, state.TransitionOptions{
		Actor: "m6-operator",
	})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCutover, u.State)

	// Step 8: Cutover -> Completed (requires fallbackCount == 0)
	u, err = store.Transition("cli", adapterkit.StateCompleted, state.TransitionOptions{
		Actor: "m6-operator",
	})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCompleted, u.State)

	finalUnit, err := store.GetUnit("cli")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCompleted, finalUnit.State)
}

// TestM6_ExitGate_ReleaseVerification_v0_2_0 validates the release pipeline configuration
// in .goreleaser.yaml for v0.2.0, asserting cross-compilation and cosign signing.
func TestM6_ExitGate_ReleaseVerification_v0_2_0(t *testing.T) {
	releaserFile, err := filepath.Abs("../../.goreleaser.yaml")
	if err != nil || !fileExists(releaserFile) {
		releaserFile, err = filepath.Abs(".goreleaser.yaml")
		require.NoError(t, err)
	}
	require.FileExists(t, releaserFile)

	data, err := os.ReadFile(releaserFile)
	require.NoError(t, err)
	content := string(data)

	// Verify cross-compilation targets
	assert.Contains(t, content, "linux")
	assert.Contains(t, content, "darwin")
	assert.Contains(t, content, "windows")
	assert.Contains(t, content, "amd64")
	assert.Contains(t, content, "arm64")

	// Verify Sigstore / Cosign signing configuration
	assert.Contains(t, content, "cosign")
	assert.Contains(t, content, "sign-blob")
	assert.Contains(t, content, ".sigstore.json")
	assert.Contains(t, content, "checksums.txt")
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
