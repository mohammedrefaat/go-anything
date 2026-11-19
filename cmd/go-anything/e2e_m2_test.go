package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	clifrontdoor "github.com/mohammedrefat/go-anything/adapters/application/cli/frontdoor"
	httpfrontdoor "github.com/mohammedrefat/go-anything/adapters/application/http/frontdoor"
	"github.com/mohammedrefat/go-anything/internal/frontdoor"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_M2_CLIStranglerLifecycle validates the complete strangler lifecycle for a CLI application:
// record -> replay -> front door & kill switch -> verify -> approve -> promote -> lift -> 100% replay proof.
func TestE2E_M2_CLIStranglerLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-m2-cli-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// 1. Build original sample CLI binary
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
	case "calc":
		fmt.Println("{\"result\":42,\"status\":\"success\"}")
	case "legacy":
		fmt.Println("{\"unported\":\"legacy_value\"}")
	default:
		fmt.Printf("{\"timestamp\":\"%s\",\"error\":\"unknown\"}\n", time.Now().UTC().Format(time.RFC3339Nano))
	}
}
`
	origSrcFile := filepath.Join(tempDir, "orig.go")
	require.NoError(t, os.WriteFile(origSrcFile, []byte(origSrc), 0644))

	origBin := filepath.ToSlash(filepath.Join(tempDir, "orig-cli.exe"))
	cmd := exec.Command("go", "build", "-o", origBin, origSrcFile)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to build orig-cli: %s", string(out))

	// 2. Build ported Go CLI binary
	portedSrc := `package main
import (
	"fmt"
	"os"
)
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: app <cmd>")
		return
	}
	switch os.Args[1] {
	case "calc":
		// Key order is different; structural JSON comparator handles it
		fmt.Println("{\"status\":\"success\",\"result\":42}")
	default:
		fmt.Println("{\"error\":\"unknown\"}")
	}
}
`
	portedSrcFile := filepath.Join(tempDir, "ported.go")
	require.NoError(t, os.WriteFile(portedSrcFile, []byte(portedSrc), 0644))

	portedBin := filepath.ToSlash(filepath.Join(tempDir, "ported-cli.exe"))
	cmd = exec.Command("go", "build", "-o", portedBin, portedSrcFile)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "failed to build ported-cli: %s", string(out))

	// 3. Initialize workspace
	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"init", "--name=calculator-cli", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	store := state.NewStore(tempDir)

	// 4. Register unit "calc"
	unit := &adapterkit.Unit{
		SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
		ID:              "calc",
		Name:            "Calculator Subcommand",
		Wave:            0,
		State:           adapterkit.StatePlanned,
		EvidenceProfile: adapterkit.ProfileReplay,
		Metadata: map[string]any{
			"original_path": filepath.Base(origBin),
			"bridge_path":   "orig.go",
		},
	}
	require.NoError(t, store.SaveUnit(unit))

	// 5. Record baseline cases from original binary
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"record",
		"--unit=calc",
		"--cmd=" + origBin + " calc",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "record calc failed: %s", stderr.String())

	u, err := store.GetUnit("calc")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateRecorded, u.State)

	// Transition Recorded -> Ready
	_, err = store.Transition("calc", adapterkit.StateReady, state.TransitionOptions{})
	require.NoError(t, err)

	// 6. Replay against ported binary
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
	assert.Contains(t, stdout.String(), "PASS: 100% replay matched (1/1 cases)")

	// 7. Verify front door routing and kill-switch override
	fd := clifrontdoor.New(origBin,
		clifrontdoor.WithPortedUnitCommand("calc", "calc", func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
			fmt.Fprintln(stdout, `{"status":"success","result":42,"driver":"ported_go"}`)
			return 0, nil
		}),
		clifrontdoor.WithRouteOverride(func(cmd, unitID string) bool {
			return frontdoor.IsUnitKillSwitchActive(tempDir, unitID)
		}),
	)

	// A. Native ported execution
	stdout.Reset()
	stderr.Reset()
	code, err := fd.ExecuteWithIO(context.Background(), []string{"calc"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), `"driver":"ported_go"`)

	// B. Transparent unported passthrough to original binary
	stdout.Reset()
	stderr.Reset()
	code, err = fd.ExecuteWithIO(context.Background(), []string{"legacy"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), `"unported":"legacy_value"`)

	// C. Kill switch test: routing drops back to original binary
	_ = os.Setenv("GO_ANYTHING_UNIT_CALC", "original")
	stdout.Reset()
	stderr.Reset()
	code, err = fd.ExecuteWithIO(context.Background(), []string{"calc"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.NotContains(t, stdout.String(), "driver") // Orig binary does not include driver tag!
	assert.Contains(t, stdout.String(), `"result":42`)
	_ = os.Unsetenv("GO_ANYTHING_UNIT_CALC")

	// 8. Execute 'go-anything verify': stores commit-tied evidence and advances to Verified
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"verify",
		"calc",
		"--target=" + portedBin,
		"--mode=structural_json",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "verify failed: %s\n%s", stderr.String(), stdout.String())
	assert.Contains(t, stdout.String(), `PASS: Unit "calc" successfully verified`)

	u, err = store.GetUnit("calc")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateVerified, u.State)

	// 9. Execute 'go-anything approve': requires --skip-shadow, advances to Approved
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"approve", "calc", "--skip-shadow", "--actor=lead", "--dir=" + tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `Approved unit "calc" by lead`)

	u, err = store.GetUnit("calc")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateApproved, u.State)

	// 10. Execute 'go-anything promote': tests kill switch and advances to Cutover
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"promote", "calc", "--actor=lead", "--dir=" + tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `Promoted unit "calc"`)
	assert.Contains(t, stdout.String(), "Kill switch tested and verified operational")

	u, err = store.GetUnit("calc")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCutover, u.State)

	// 11. Test that 'lift' refuses if fallback count is non-zero
	_, err = frontdoor.IncrementFallback(tempDir, "calc")
	require.NoError(t, err)

	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"lift", "--unit=calc", "--dir=" + tempDir, "--skip-build"}, &stdout, &stderr)
	assert.Error(t, err, "lift must refuse when fallback count > 0")
	assert.Contains(t, stderr.String(), "non-zero fallback count")

	u, _ = store.GetUnit("calc")
	assert.Equal(t, adapterkit.StateCutover, u.State, "state must remain Cutover if lift is blocked")

	// 12. Reset fallback count to 0 and execute 'lift'
	_ = os.Remove(frontdoor.FallbackFilePath(tempDir, "calc"))
	u.FallbackCount = 0
	require.NoError(t, store.SaveUnit(u))

	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"lift", "--unit=calc", "--dir=" + tempDir, "--skip-build"}, &stdout, &stderr)
	require.NoError(t, err, "lift failed: %s\n%s", stderr.String(), stdout.String())
	assert.Contains(t, stdout.String(), "SUCCESS: Lift complete!")

	// Prove unit is marked Completed
	u, err = store.GetUnit("calc")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCompleted, u.State)

	// 13. Prove 100% replay still passes after lift
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
	assert.Contains(t, stdout.String(), "PASS: 100% replay matched (1/1 cases)")
}

// TestE2E_M2_HTTPStranglerLifecycle validates the complete strangler lifecycle for an HTTP service:
// record -> replay -> front door & kill switch -> verify -> approve -> promote -> lift -> 100% replay proof.
func TestE2E_M2_HTTPStranglerLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-m2-http-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer

	// 1. Initialize workspace
	err = runCLI([]string{"init", "--name=user-service", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	store := state.NewStore(tempDir)

	// 2. Start mock original HTTP application server
	var reqCounter int64
	origServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		reqID := atomic.AddInt64(&reqCounter, 1)
		w.Header().Set("X-Request-ID", fmt.Sprintf("req-%d", reqID))
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

		switch r.URL.Path {
		case "/users/1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1,"name":"Alice","role":"admin"}`))
		case "/legacy":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"legacy":"unported_route"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found"}`))
		}
	}))
	defer origServer.Close()

	// 3. Register unit "users"
	unit := &adapterkit.Unit{
		SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
		ID:              "users",
		Name:            "Users Service",
		Wave:            0,
		State:           adapterkit.StatePlanned,
		EvidenceProfile: adapterkit.ProfileReplay,
		Metadata: map[string]any{
			"legacy_path": "legacy_service.py",
		},
	}
	require.NoError(t, store.SaveUnit(unit))

	// Create dummy legacy file to prove removal during lift
	dummyFile := filepath.Join(tempDir, "legacy_service.py")
	require.NoError(t, os.WriteFile(dummyFile, []byte("# legacy python code\n"), 0644))

	// 4. Record baseline cases
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"record",
		"--unit=users",
		"--url=" + origServer.URL + "/users/1",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "record users failed: %s", stderr.String())

	u, err := store.GetUnit("users")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateRecorded, u.State)

	// Transition to Ready
	_, err = store.Transition("users", adapterkit.StateReady, state.TransitionOptions{})
	require.NoError(t, err)

	// 5. Start ported Go HTTP server
	portedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-ported-42")
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

		if r.URL.Path == "/users/1" {
			w.WriteHeader(http.StatusOK)
			// Equivalent structural JSON
			_, _ = w.Write([]byte(`{"role":"admin","id":1,"name":"Alice"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	}))
	defer portedServer.Close()

	// 6. Replay against ported Go server
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"replay",
		"--unit=users",
		"--target=" + portedServer.URL,
		"--mode=structural_json",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "replay failed: %s\n%s", stderr.String(), stdout.String())
	assert.Contains(t, stdout.String(), "PASS: 100% replay matched (1/1 cases)")

	// 7. Verify HTTP front door proxy and kill switch
	targetURL, err := url.Parse(origServer.URL)
	require.NoError(t, err)

	hfd, err := httpfrontdoor.NewFrontDoor(targetURL)
	require.NoError(t, err)

	hfd.RegisterUnitRoute("users", "/users/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if frontdoor.IsUnitKillSwitchActive(tempDir, "users") {
			// Kill switch active: proxy directly to original server
			origProxy := httputil.NewSingleHostReverseProxy(targetURL)
			origProxy.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Driver", "ported-go")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"role":"admin","id":1,"name":"Alice"}`))
	}))

	proxyServer := httptest.NewServer(hfd)
	defer proxyServer.Close()

	// A. Ported route served by Go handler
	resp, err := http.Get(proxyServer.URL + "/users/1")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ported-go", resp.Header.Get("X-Driver"))
	_ = resp.Body.Close()

	// B. Unported route proxied to original server
	resp, err = http.Get(proxyServer.URL + "/legacy")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-Driver")) // Original server does not set X-Driver
	_ = resp.Body.Close()

	// C. Kill switch test: /users/1 proxied back to original server
	_ = os.Setenv("GO_ANYTHING_UNIT_USERS", "original")
	resp, err = http.Get(proxyServer.URL + "/users/1")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-Driver"))
	_ = resp.Body.Close()
	_ = os.Unsetenv("GO_ANYTHING_UNIT_USERS")

	// 8. Execute 'go-anything verify': advances to Verified
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"verify",
		"users",
		"--target=" + portedServer.URL,
		"--mode=structural_json",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `PASS: Unit "users" successfully verified`)

	u, err = store.GetUnit("users")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateVerified, u.State)

	// 9. Execute 'go-anything approve': advances to Approved
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"approve", "users", "--skip-shadow", "--actor=lead", "--dir=" + tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `Approved unit "users" by lead`)

	u, err = store.GetUnit("users")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateApproved, u.State)

	// 10. Execute 'go-anything promote': advances to Cutover
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"promote", "users", "--actor=lead", "--dir=" + tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `Promoted unit "users"`)

	u, err = store.GetUnit("users")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCutover, u.State)

	// 11. Execute 'go-anything lift': confirms zero fallbacks, removes legacy files, and marks Completed
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"lift", "--unit=users", "--dir=" + tempDir, "--skip-build"}, &stdout, &stderr)
	require.NoError(t, err, "lift failed: %s\n%s", stderr.String(), stdout.String())
	assert.Contains(t, stdout.String(), "SUCCESS: Lift complete!")

	u, err = store.GetUnit("users")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCompleted, u.State)

	// Verify dummy legacy file was safely removed
	assert.NoFileExists(t, dummyFile, "legacy original file must be removed after lift")

	// 12. Prove 100% replay pass after lift
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"replay",
		"--unit=users",
		"--target=" + portedServer.URL,
		"--mode=structural_json",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "PASS: 100% replay matched (1/1 cases)")
}
