package frontdoor_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	clifrontdoor "github.com/mohammedrefaat/go-anything/adapters/application/cli/frontdoor"
	"github.com/mohammedrefaat/go-anything/internal/frontdoor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestKillSwitch_EnvironmentVariables(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("Global kill switch", func(t *testing.T) {
		assert.False(t, frontdoor.IsGlobalKillSwitchActive())

		for _, val := range []string{"true", "1", "original", "TRUE", "Original"} {
			t.Setenv(frontdoor.EnvDisableAll, val)
			assert.True(t, frontdoor.IsGlobalKillSwitchActive(), "expected true for %s", val)
			assert.True(t, frontdoor.IsUnitKillSwitchActive(tmpDir, "any-unit"))
		}

		t.Setenv(frontdoor.EnvDisableAll, "false")
		assert.False(t, frontdoor.IsGlobalKillSwitchActive())
	})

	t.Run("Per-unit direct env var", func(t *testing.T) {
		unitID := "unit-checkout"
		assert.False(t, frontdoor.IsUnitKillSwitchActive(tmpDir, unitID))

		t.Setenv("GO_ANYTHING_UNIT_unit-checkout", "original")
		assert.True(t, frontdoor.IsUnitKillSwitchActive(tmpDir, unitID))

		// Different unit is unaffected
		assert.False(t, frontdoor.IsUnitKillSwitchActive(tmpDir, "unit-other"))
	})

	t.Run("Per-unit normalized env var", func(t *testing.T) {
		unitID := "auth.service-v1/login"
		normKey := frontdoor.NormalizeUnitEnvKey(unitID)
		assert.Equal(t, "AUTH_SERVICE_V1_LOGIN", normKey)

		assert.False(t, frontdoor.IsUnitKillSwitchActive(tmpDir, unitID))

		t.Setenv("GO_ANYTHING_UNIT_"+normKey, "original")
		assert.True(t, frontdoor.IsUnitKillSwitchActive(tmpDir, unitID))
	})
}

func TestKillSwitch_LocalLockfiles(t *testing.T) {
	tmpDir := t.TempDir()
	localDir := filepath.Join(tmpDir, ".go-anything", "local")
	require.NoError(t, os.MkdirAll(localDir, 0755))

	unitID := "order-service"
	assert.False(t, frontdoor.IsUnitKillSwitchActive(tmpDir, unitID))

	// Create .kill file
	killFile := filepath.Join(localDir, unitID+".kill")
	require.NoError(t, os.WriteFile(killFile, []byte("disabled"), 0644))
	assert.True(t, frontdoor.IsUnitKillSwitchActive(tmpDir, unitID))

	require.NoError(t, os.Remove(killFile))
	assert.False(t, frontdoor.IsUnitKillSwitchActive(tmpDir, unitID))

	// Create .override file
	overrideFile := filepath.Join(localDir, unitID+".override")
	require.NoError(t, os.WriteFile(overrideFile, []byte("override to original"), 0644))
	assert.True(t, frontdoor.IsUnitKillSwitchActive(tmpDir, unitID))
}

func TestFallbackCounter_PersistenceAndAtomicity(t *testing.T) {
	tmpDir := t.TempDir()
	unitID := "test-unit"

	count, err := frontdoor.GetFallbackCount(tmpDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// Increment once
	n1, err := frontdoor.IncrementFallback(tmpDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n1)

	// Increment again
	n2, err := frontdoor.IncrementFallback(tmpDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n2)

	// Verify durable record
	rec, err := frontdoor.GetFallbackRecord(tmpDir, unitID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, unitID, rec.UnitID)
	assert.Equal(t, int64(2), rec.Count)
	assert.WithinDuration(t, time.Now().UTC(), rec.LastFallback, 5*time.Second)

	// Test backward compatibility: parse raw integer format
	rawFile := frontdoor.FallbackFilePath(tmpDir, "legacy-raw-unit")
	require.NoError(t, os.WriteFile(rawFile, []byte("42\n"), 0644))

	rawCount, err := frontdoor.GetFallbackCount(tmpDir, "legacy-raw-unit")
	require.NoError(t, err)
	assert.Equal(t, int64(42), rawCount)

	// Incrementing raw format migrates it to JSON
	nRaw, err := frontdoor.IncrementFallback(tmpDir, "legacy-raw-unit")
	require.NoError(t, err)
	assert.Equal(t, int64(43), nRaw)

	// Test reset
	require.NoError(t, frontdoor.ResetFallbackCount(tmpDir, unitID))
	cReset, err := frontdoor.GetFallbackCount(tmpDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), cReset)
}

func TestFallbackCounter_HighConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	unitID := "concurrent-unit"
	numGoroutines := 30
	incrementsPerGoroutine := 10
	expectedTotal := int64(numGoroutines * incrementsPerGoroutine)

	var eg errgroup.Group
	for i := 0; i < numGoroutines; i++ {
		eg.Go(func() error {
			for j := 0; j < incrementsPerGoroutine; j++ {
				if _, err := frontdoor.IncrementFallback(tmpDir, unitID); err != nil {
					return err
				}
			}
			return nil
		})
	}

	require.NoError(t, eg.Wait())

	finalCount, err := frontdoor.GetFallbackCount(tmpDir, unitID)
	require.NoError(t, err)
	assert.Equal(t, expectedTotal, finalCount)
}

func TestManager_GetAllFallbackCounts(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := frontdoor.NewManager(tmpDir)

	_, err := mgr.RecordFallback("unit-1")
	require.NoError(t, err)
	_, err = mgr.RecordFallback("unit-1")
	require.NoError(t, err)

	_, err = mgr.RecordFallback("unit-2")
	require.NoError(t, err)

	allCounts := mgr.GetAllFallbackCounts()
	assert.Equal(t, int64(2), allCounts["unit-1"])
	assert.Equal(t, int64(1), allCounts["unit-2"])
}

func TestManager_CLIIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := frontdoor.NewManager(tmpDir)

	portedCalled := false
	portedHandler := func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
		portedCalled = true
		_, _ = stdout.Write([]byte("ported output"))
		return 0, nil
	}

	fallbackCalled := false
	customFallback := func(ctx context.Context, origBin string, origArgs []string, argv []string, stdin io.Reader, stdout, stderr io.Writer, env []string, dir string) (int, error) {
		fallbackCalled = true
		_, _ = stdout.Write([]byte("fallback output"))
		return 0, nil
	}

	fd := clifrontdoor.New("orig_app",
		clifrontdoor.WithPortedUnitCommand("status", "unit-status", portedHandler),
		clifrontdoor.WithFallbackExecutor(customFallback),
		clifrontdoor.WithRouteOverride(mgr.CLIRouteOverrideHook()),
		clifrontdoor.WithFallbackHook(mgr.CLIFallbackHook()),
	)

	// 1. Without kill switch: ported executes
	exitCode, err := fd.Execute(context.Background(), []string{"status"})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.True(t, portedCalled)
	assert.False(t, fallbackCalled)
	assert.Equal(t, int64(0), mgr.GetFallbackCount("unit-status"))

	// 2. Enable kill switch for unit-status
	t.Setenv("GO_ANYTHING_UNIT_unit-status", "original")
	portedCalled = false
	fallbackCalled = false

	exitCode, err = fd.Execute(context.Background(), []string{"status"})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.False(t, portedCalled, "ported handler should not be called when kill switch is on")
	assert.True(t, fallbackCalled, "fallback executor must be called")
	assert.Equal(t, int64(1), mgr.GetFallbackCount("unit-status"))
}

func TestManager_HTTPIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := frontdoor.NewManager(tmpDir)
	unitID := "http-user-service"

	portedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ported-handler-response"))
	})

	fallbackHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("legacy-fallback-response"))
	})

	wrapped := mgr.WrapHTTPHandler(unitID, portedHandler, fallbackHandler)

	// 1. Normal state: ported serves request
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ported-handler-response", rec.Body.String())
	assert.Equal(t, int64(0), mgr.GetFallbackCount(unitID))

	// 2. Kill switch active: fallback serves request and increments counter
	t.Setenv("GO_ANYTHING_UNIT_HTTP_USER_SERVICE", "original")

	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "legacy-fallback-response", rec2.Body.String())
	assert.Equal(t, int64(1), mgr.GetFallbackCount(unitID))

	// 3. Fallback handler nil with kill switch active -> returns 503
	wrappedNilFallback := mgr.WrapHTTPHandler(unitID, portedHandler, nil)
	rec3 := httptest.NewRecorder()
	wrappedNilFallback.ServeHTTP(rec3, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec3.Code)
	assert.Equal(t, int64(2), mgr.GetFallbackCount(unitID))
}
