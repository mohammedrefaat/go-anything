package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/frontdoor"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_VerifyApprovePromote_FullWorkflow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-vap-workflow-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer

	// 1. Initialize workspace
	err = runCLI([]string{"init", "--name=service-auth", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	store := state.NewStore(tempDir)

	// 2. Register unit "auth"
	unit := &adapterkit.Unit{
		SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
		ID:              "auth",
		Name:            "Authentication Unit",
		Wave:            0,
		State:           adapterkit.StatePlanned,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(unit))

	// 3. Start mock HTTP original server and record cases
	var reqCounter int64
	origServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		reqID := atomic.AddInt64(&reqCounter, 1)
		w.Header().Set("X-Request-ID", fmt.Sprintf("req-%d", reqID))
		switch r.URL.Path {
		case "/login":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"logged_in","user_id":"u-123"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found"}`))
		}
	}))
	defer origServer.Close()

	// Record happy login case
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"record",
		"--unit=auth",
		"--url=" + origServer.URL + "/login",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	u, err := store.GetUnit("auth")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateRecorded, u.State)

	// Move to Ready
	_, err = store.Transition("auth", adapterkit.StateReady, state.TransitionOptions{})
	require.NoError(t, err)

	// Start ported Go HTTP server (returns matching behavior)
	portedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-ported-1")
		if r.URL.Path == "/login" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"user_id":"u-123","status":"logged_in"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	}))
	defer portedServer.Close()

	// 4. Test verify: requires --target
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"verify", "auth", "--dir=" + tempDir}, &stdout, &stderr)
	assert.Error(t, err, "verify without --target should fail")

	// 5. Test verify success: advances unit to Verified and stores evidence
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"verify",
		"auth",
		"--target=" + portedServer.URL,
		"--mode=structural_json",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "verify failed: %s\n%s", stderr.String(), stdout.String())
	assert.Contains(t, stdout.String(), `PASS: Unit "auth" successfully verified under profile "replay"`)

	u, err = store.GetUnit("auth")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateVerified, u.State)

	// Check evidence file
	evidenceFiles, err := store.ListEvidenceForUnit("auth")
	require.NoError(t, err)
	require.NotEmpty(t, evidenceFiles)
	assert.Equal(t, "auth", evidenceFiles[0].UnitID)
	assert.Equal(t, 0, evidenceFiles[0].MismatchesCount)

	// 6. Test approve without --skip-shadow should fail (M5 shadow engine requirement)
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"approve", "auth", "--dir=" + tempDir}, &stdout, &stderr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "use --skip-shadow to bypass shadow mode")

	u, _ = store.GetUnit("auth")
	assert.Equal(t, adapterkit.StateVerified, u.State, "state must remain Verified when approve fails")

	// 7. Test approve with --skip-shadow succeeds: advances to Approved
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"approve", "auth", "--skip-shadow", "--actor=lead-dev", "--dir=" + tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `Approved unit "auth" by lead-dev`)

	u, err = store.GetUnit("auth")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateApproved, u.State)
	assert.Equal(t, "lead-dev", u.Metadata["approved_by"])
	assert.Equal(t, true, u.Metadata["shadow_skipped"])

	// 8. Test promote: switches routing to Go, tests kill switch, and advances to Cutover
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"promote", "auth", "--actor=lead-dev", "--dir=" + tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `Promoted unit "auth": routing switched to Go implementation`)
	assert.Contains(t, stdout.String(), "Kill switch tested and verified operational")

	u, err = store.GetUnit("auth")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateCutover, u.State)
	assert.Equal(t, "go", u.Metadata["routing"])

	// 9. Increment fallback count and verify status display
	_, err = frontdoor.IncrementFallback(tempDir, "auth")
	require.NoError(t, err)
	_, err = frontdoor.IncrementFallback(tempDir, "auth")
	require.NoError(t, err)

	fbCount, err := frontdoor.GetFallbackCount(tempDir, "auth")
	require.NoError(t, err)
	assert.Equal(t, int64(2), fbCount)

	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"status", tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "auth")
	assert.Contains(t, stdout.String(), "Cutover")
	assert.Contains(t, stdout.String(), "2") // Displays fallback count 2
}

func TestCLI_Verify_RefusesOnMismatch(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-verify-mismatch-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"init", "--name=shop", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	store := state.NewStore(tempDir)
	unit := &adapterkit.Unit{
		SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
		ID:              "cart",
		Name:            "Shopping Cart",
		Wave:            0,
		State:           adapterkit.StateReady,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(unit))

	// Write recorded case
	c := &adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         "case-cart-0001",
		UnitID:         "cart",
		Kind:           adapterkit.KindHappy,
		SourceRevision: "head",
		Runtime:        "go1.22",
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Method: "GET",
			Path:   "/cart",
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: 200,
			Body:       map[string]any{"items_count": float64(5)},
		},
	}
	casesFile := filepath.Join(store.CasesDir(), "cart.jsonl")
	data, _ := json.Marshal(c)
	_ = os.WriteFile(casesFile, append(data, '\n'), 0644)

	// Server returning mismatched items_count (99 instead of 5)
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items_count":99}`))
	}))
	defer badServer.Close()

	// Verify should refuse transition due to mismatch
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{
		"verify",
		"cart",
		"--target=" + badServer.URL,
		"--mode=structural_json",
		"--dir=" + tempDir,
	}, &stdout, &stderr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "verification refused: 1/1 cases mismatched")

	// Unit state should NOT advance to Verified
	u, _ := store.GetUnit("cart")
	assert.NotEqual(t, adapterkit.StateVerified, u.State)
}

func TestCLI_Promote_RefusesIfNotApproved(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-promote-guard-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"init", "--name=orders", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	store := state.NewStore(tempDir)
	unit := &adapterkit.Unit{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		ID:            "order",
		Name:          "Order Unit",
		Wave:          0,
		State:         adapterkit.StateVerified, // Verified, but NOT Approved yet
	}
	require.NoError(t, store.SaveUnit(unit))

	// Promote must reject unapproved unit
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"promote", "order", "--dir=" + tempDir}, &stdout, &stderr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), `cannot promote unit "order": must be in 'Approved' state (currently "Verified")`)
}
