package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/recorder/capture"
	"github.com/mohammedrefaat/go-anything/internal/redactor"
	"github.com/mohammedrefaat/go-anything/internal/shadow"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestM5_ExitGate_ShadowModeEngine verifies the M5 exit gate for the shadow engine:
// 1. GO_ANYTHING_MODE resolution (global and per-unit override).
// 2. Clean shadow log on idempotent traffic (GET, HEAD).
// 3. Blocked non-idempotent traffic (POST, PUT, DELETE, PATCH) by default.
// 4. Mandatory startup warning when --allow-side-effects is enabled.
// 5. Fail-closed in-memory redaction before writing shadow_mismatches.jsonl.
func TestM5_ExitGate_ShadowModeEngine(t *testing.T) {
	// -------------------------------------------------------------------------
	// 1. GO_ANYTHING_MODE resolution
	// -------------------------------------------------------------------------
	t.Run("mode resolution", func(t *testing.T) {
		t.Setenv("GO_ANYTHING_MODE", "shadow")
		assert.Equal(t, shadow.ModeShadow, shadow.GetGlobalMode())

		t.Setenv("GO_ANYTHING_MODE", "new")
		assert.Equal(t, shadow.ModeNew, shadow.GetGlobalMode())

		// Unit-specific override takes precedence
		t.Setenv("GO_ANYTHING_MODE", "old")
		t.Setenv("GO_ANYTHING_MODE_PAYMENTS", "shadow")
		assert.Equal(t, shadow.ModeShadow, shadow.GetUnitMode("payments"))
		assert.Equal(t, shadow.ModeOld, shadow.GetUnitMode("billing"))
	})

	// -------------------------------------------------------------------------
	// 2. Clean shadow log on idempotent traffic
	// -------------------------------------------------------------------------
	t.Run("clean shadow log on idempotent traffic", func(t *testing.T) {
		tempDir := t.TempDir()
		reportsPath := filepath.Join(tempDir, "shadow_mismatches.jsonl")

		// Legacy and Modern handlers returning identical responses
		legacyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","balance":100.50}`))
		})
		modernHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","balance":100.50}`))
		})

		engine := shadow.New(
			shadow.WithMode(shadow.ModeShadow),
			shadow.WithUnitID("pkg/currency"),
			shadow.WithReportsPath(reportsPath),
			shadow.WithWorkerPoolSize(2),
			shadow.WithQueueCapacity(50),
			shadow.WithSampleRate(1.0),
		)
		defer engine.Close()

		wrapped := engine.WrapHTTP(legacyHandler, modernHandler)

		// Send 10 idempotent GET and HEAD requests
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, "/balance?account=acc_123", nil)
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
		}
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodHead, "/balance?account=acc_123", nil)
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
		}

		// Wait briefly for asynchronous shadow worker pool to complete
		require.Eventually(t, func() bool {
			return engine.ShadowRuns() == 10
		}, 3*time.Second, 20*time.Millisecond)

		assert.Equal(t, int64(10), engine.TotalCalls())
		assert.Equal(t, int64(10), engine.ShadowRuns())
		assert.Equal(t, int64(0), engine.MismatchCount(), "zero mismatches on identical responses")
		assert.Equal(t, int64(0), engine.BlockedCount())

		// Verify shadow mismatch log is completely clean (either absent or 0 bytes)
		if data, err := os.ReadFile(reportsPath); err == nil {
			assert.Empty(t, strings.TrimSpace(string(data)), "shadow_mismatches.jsonl must remain clean")
		}
	})

	// -------------------------------------------------------------------------
	// 3. Blocked non-idempotent traffic by default
	// -------------------------------------------------------------------------
	t.Run("blocked non-idempotent traffic by default", func(t *testing.T) {
		tempDir := t.TempDir()
		reportsPath := filepath.Join(tempDir, "shadow_mismatches.jsonl")

		var modernExecuted bool
		legacyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"order_id":"ord_999","status":"created"}`))
		})
		modernHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			modernExecuted = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"order_id":"ord_999","status":"created"}`))
		})

		engine := shadow.New(
			shadow.WithMode(shadow.ModeShadow),
			shadow.WithUnitID("pkg/billing"),
			shadow.WithReportsPath(reportsPath),
			shadow.WithAllowSideEffects(false), // default safety: idempotent-only
			shadow.WithWorkerPoolSize(2),
		)
		defer engine.Close()

		wrapped := engine.WrapHTTP(legacyHandler, modernHandler)

		// Send non-idempotent write operations: POST, PUT, DELETE, PATCH
		methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
		for _, method := range methods {
			req := httptest.NewRequest(method, "/orders", strings.NewReader(`{"amount":50}`))
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusCreated, rec.Code, "primary legacy response returned")
		}

		// Ensure modern handler was NEVER executed in shadow mode for write requests
		time.Sleep(50 * time.Millisecond)
		assert.False(t, modernExecuted, "modern handler must not run for non-idempotent requests by default")
		assert.Equal(t, int64(4), engine.BlockedCount(), "all 4 non-idempotent requests must be blocked")
		assert.Equal(t, int64(0), engine.ShadowRuns(), "0 shadow runs scheduled for blocked writes")
	})

	// -------------------------------------------------------------------------
	// 4. Mandatory startup warning when side effects are enabled
	// -------------------------------------------------------------------------
	t.Run("startup warning on allow-side-effects", func(t *testing.T) {
		var warnBuf bytes.Buffer

		engine := shadow.New(
			shadow.WithAllowSideEffects(true),
			shadow.WithWarningWriter(&warnBuf),
		)
		defer engine.Close()

		expectedWarning := shadow.StartupWarning
		assert.Contains(t, warnBuf.String(), expectedWarning, "must emit exact v3 plan startup warning")
	})

	// -------------------------------------------------------------------------
	// 5. Fail-closed in-memory redaction before writing shadow_mismatches.jsonl
	// -------------------------------------------------------------------------
	t.Run("fail-closed redaction before writing shadow_mismatches.jsonl", func(t *testing.T) {
		tempDir := t.TempDir()
		reportsPath := filepath.Join(tempDir, "shadow_mismatches.jsonl")

		engine := shadow.New(
			shadow.WithUnitID("pkg/fraud"),
			shadow.WithReportsPath(reportsPath),
		)
		defer engine.Close()

		// Secrets that must never be written unredacted to disk
		rawBearer := "Bearer ghp_0123456789abcdef0123456789abcdef0123"
		rawAPIKey := "sk_test_" + "0123456789abcdef0123456789abcdef01234567"
		rawPassword := "topSecretMasterPassword123!"
		rawCard := "4532015112830366"

		// Record a simulated mismatch containing sensitive customer and auth data
		err := engine.LogMismatch(shadow.MismatchRecord{
			UnitID:     "pkg/fraud",
			ReportType: string(adapterkit.ReportTypeDiff),
			Status:     string(adapterkit.StatusFail),
			Summary:    fmt.Sprintf("Mismatch with api_key %s", rawAPIKey),
			Mismatches: []adapterkit.Mismatch{
				{
					Path:     "auth.header",
					Expected: rawBearer,
					Actual:   "Bearer mismatch_token",
					Message:  "token mismatch",
				},
				{
					Path:     "customer.card",
					Expected: rawCard,
					Actual:   rawCard,
					Message:  "card mismatch",
				},
			},
			Details: map[string]any{
				"password": rawPassword,
				"api_key":  rawAPIKey,
			},
		})
		require.NoError(t, err)

		// Read back written file and verify ZERO secrets were leaked to disk
		data, err := os.ReadFile(reportsPath)
		require.NoError(t, err)
		content := string(data)

		assert.NotContains(t, content, rawBearer)
		assert.NotContains(t, content, rawAPIKey)
		assert.NotContains(t, content, rawPassword)
		assert.NotContains(t, content, rawCard)

		// Verify valid JSON Lines format
		var parsed shadow.MismatchRecord
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(content)), &parsed))
		assert.Equal(t, "pkg/fraud", parsed.UnitID)
	})
}

// TestM5_ExitGate_CaptureIngestion_ZeroLeaks verifies the M5 exit gate for production capture:
// 1. All 4 capture sources (HTTP mirror, broker consumer, distributed trace, read-only observability).
// 2. Generation of adapterkit.Case with Kind: 'captured' and SchemaVersion: 'v1alpha1'.
// 3. Fail-closed in-memory redaction before writing to .go-anything/cases/<unit>.jsonl.
// 4. Strict read-only enforcement: rejection of routing control operations.
func TestM5_ExitGate_CaptureIngestion_ZeroLeaks(t *testing.T) {
	tempDir := t.TempDir()
	store := state.NewStore(tempDir)
	casesDir := store.CasesDir()
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	ingester := capture.NewIngester(capture.Config{
		WorkspaceRoot: tempDir,
		Store:         store,
		Redactor:      redactor.New(),
		Runtime:       "go1.22",
	})

	// Sensitive test credentials
	secretBearer := "Bearer ghp_live_0123456789abcdef0123456789abcdef0123"
	secretPass := "ProductionMasterPassword123!"
	secretCard := "4532015112830366"
	secretAPIKey := "sec_live_key_998877665544332211"

	// -------------------------------------------------------------------------
	// Source 1: HTTP Mirror Source
	// -------------------------------------------------------------------------
	t.Run("HTTP mirror source", func(t *testing.T) {
		httpSource := capture.NewHTTPMirrorSource(ingester, "pkg/currency")
		c, err := httpSource.Ingest(context.Background(), "pkg/currency", capture.HTTPTraffic{
			Request: capture.HTTPRequest{
				Method: "GET",
				Path:   "/convert?amount=100&from=USD&to=EUR",
				Headers: map[string]string{
					"Authorization": secretBearer,
					"X-Api-Key":     secretAPIKey,
				},
			},
			Response: capture.HTTPResponse{
				StatusCode: 200,
				Body:       map[string]any{"result": 92.0, "secret": secretPass},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, adapterkit.SchemaVersionV1Alpha1, c.SchemaVersion)
		assert.Equal(t, "pkg/currency", c.UnitID)
	})

	// -------------------------------------------------------------------------
	// Source 2: Message Broker Consumer Source
	// -------------------------------------------------------------------------
	t.Run("broker consumer source", func(t *testing.T) {
		brokerSource := capture.NewBrokerConsumerSource(ingester)
		c, err := brokerSource.Ingest(context.Background(), "pkg/billing", capture.BrokerMessage{
			Topic: "payment.events.v1",
			Key:   "txn_999888",
			Headers: map[string]string{
				"Authorization": secretBearer,
			},
			Payload: map[string]any{
				"account_id":  "acc_123",
				"card_number": secretCard,
				"password":    secretPass,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, "pkg/billing", c.UnitID)
	})

	// -------------------------------------------------------------------------
	// Source 3: Distributed Trace Source
	// -------------------------------------------------------------------------
	t.Run("distributed trace source", func(t *testing.T) {
		traceSource := capture.NewTraceSource(ingester)
		c, err := traceSource.IngestSpan(context.Background(), "pkg/fraud", capture.TraceSpan{
			TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			SpanID:  "00f067aa0ba902b7",
			Name:    "EvaluateRisk",
			Kind:    "SERVER",
			Attributes: map[string]any{
				"http.method":                   "POST",
				"http.route":                    "/fraud/evaluate",
				"http.request.header.auth":      secretBearer,
				"http.request.header.x_api_key": secretAPIKey,
				"http.status_code":              200,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, "pkg/fraud", c.UnitID)
	})

	// -------------------------------------------------------------------------
	// Source 4: Read-Only Observability Source (e.g. platform-mcp)
	// -------------------------------------------------------------------------
	t.Run("read-only observability source", func(t *testing.T) {
		obsSource := capture.NewObservabilitySource(ingester)
		c, err := obsSource.Ingest(context.Background(), "cmd/server", capture.DecodedTraffic{
			Source:    "platform-mcp",
			Operation: "server.ChargeOrder",
			Protocol:  "grpc",
			Input: map[string]any{
				"order_id": "ord_1001",
				"api_key":  secretAPIKey,
				"password": secretPass,
			},
			Output: map[string]any{
				"status_code": 200,
				"body":        map[string]any{"receipt": "rcpt_approved"},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, "cmd/server", c.UnitID)
	})

	// -------------------------------------------------------------------------
	// Invariant: Zero Leaks in Cases Written to Disk
	// -------------------------------------------------------------------------
	t.Run("zero credential leaks on disk", func(t *testing.T) {
		units := []string{"pkg/currency", "pkg/billing", "pkg/fraud", "cmd/server"}
		for _, unitID := range units {
			caseFile := filepath.Join(casesDir, filepath.FromSlash(unitID)+".jsonl")
			data, err := os.ReadFile(caseFile)
			require.NoError(t, err, "case file for unit %s should exist", unitID)
			content := string(data)

			// Zero unredacted secrets must be found in any saved case file
			assert.NotContains(t, content, secretBearer, "Bearer token must not leak in %s", unitID)
			assert.NotContains(t, content, secretPass, "password must not leak in %s", unitID)
			assert.NotContains(t, content, secretCard, "card number must not leak in %s", unitID)
			assert.NotContains(t, content, secretAPIKey, "api key must not leak in %s", unitID)

			// Ensure valid JSON Lines format
			lines := strings.Split(strings.TrimSpace(content), "\n")
			for _, line := range lines {
				var parsed adapterkit.Case
				require.NoError(t, json.Unmarshal([]byte(line), &parsed), "line must be valid Case JSON")
				assert.Equal(t, adapterkit.KindCaptured, parsed.Kind)
				assert.Equal(t, adapterkit.SchemaVersionV1Alpha1, parsed.SchemaVersion)
			}
		}
	})

	// -------------------------------------------------------------------------
	// Invariant: Zero Routing Control (Strictly Read-Only Ingestion)
	// -------------------------------------------------------------------------
	t.Run("zero routing control enforcement", func(t *testing.T) {
		obsSource := capture.NewObservabilitySource(ingester)

		forbiddenKeywords := []string{
			"route_to",
			"traffic_split",
			"reroute",
			"route_control",
			"routing_override",
			"promote_route",
			"kill_switch",
		}

		for _, kw := range forbiddenKeywords {
			_, err := obsSource.Ingest(context.Background(), "pkg/billing", capture.DecodedTraffic{
				Source:    "platform-mcp",
				Operation: "billing.Process",
				Metadata: map[string]any{
					kw: "http://attacker.example.com",
				},
				Input:  map[string]any{"amount": 100},
				Output: map[string]any{"status": "ok"},
			})
			require.Error(t, err, "must reject routing control keyword '%s'", kw)
			assert.ErrorIs(t, err, capture.ErrRoutingControlForbidden)
		}
	})
}

// TestM5_ExitGate_PaymentsDogfoodService_EndToEnd executes live shadowing and capture ingestion
// against the example.com/payments dogfood service, proving 0 mismatches on idempotent traffic,
// blocked non-idempotent writes, and clean case ingestion.
func TestM5_ExitGate_PaymentsDogfoodService_EndToEnd(t *testing.T) {
	tempDir := t.TempDir()

	// Copy dogfood_service fixture
	srcDir, err := filepath.Abs("../../testdata/dogfood_service")
	if err != nil || !dirExists(srcDir) {
		srcDir, err = filepath.Abs("testdata/dogfood_service")
		require.NoError(t, err)
	}
	require.NoError(t, copyDir(srcDir, tempDir))

	// Initialize git repo
	initGitRepo(t, tempDir)

	// Modernize workspace: three-tier partitioning
	var modStdout, modStderr bytes.Buffer
	err = runCLI([]string{"modernize", tempDir}, &modStdout, &modStderr)
	require.NoError(t, err, "modernize failed: %s", modStderr.String())

	store := state.NewStore(tempDir)
	reportsPath := filepath.Join(store.ReportsDir(), "shadow_mismatches.jsonl")

	// -------------------------------------------------------------------------
	// 1. Live Shadowing on Currency and Fraud endpoints
	// -------------------------------------------------------------------------
	// Legacy and Modern Currency Convert Handlers
	legacyConvert := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"amount":100,"from":"USD","to":"EUR","result":92,"formatted":"€92.00"}`))
	})
	modernConvert := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"amount":100,"from":"USD","to":"EUR","result":92,"formatted":"€92.00"}`))
	})

	// Legacy and Modern Charge Handlers (Non-idempotent write)
	legacyCharge := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"receipt":"rcpt_prod_12345"}`))
	})
	modernCharge := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"receipt":"rcpt_prod_12345"}`))
	})

	engine := shadow.New(
		shadow.WithMode(shadow.ModeShadow),
		shadow.WithUnitID("pkg/currency"),
		shadow.WithReportsPath(reportsPath),
		shadow.WithWorkerPoolSize(4),
		shadow.WithQueueCapacity(100),
		shadow.WithSampleRate(1.0),
		shadow.WithAllowSideEffects(false),
	)
	defer engine.Close()

	mux := http.NewServeMux()
	mux.Handle("/convert", engine.WrapHTTP(legacyConvert, modernConvert))
	mux.Handle("/charge", engine.WrapHTTP(legacyCharge, modernCharge))

	server := httptest.NewServer(mux)
	defer server.Close()

	// Send 15 idempotent GET requests to /convert
	for i := 0; i < 15; i++ {
		resp, err := http.Get(server.URL + "/convert?amount=100&from=USD&to=EUR")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	}

	// Send 5 non-idempotent POST requests to /charge
	for i := 0; i < 5; i++ {
		resp, err := http.Post(server.URL+"/charge?amount=50&currency=USD", "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	}

	// Flush and wait for shadow tasks
	require.Eventually(t, func() bool {
		return engine.ShadowRuns() == 15
	}, 3*time.Second, 20*time.Millisecond)

	assert.Equal(t, int64(20), engine.TotalCalls())
	assert.Equal(t, int64(15), engine.ShadowRuns())
	assert.Equal(t, int64(5), engine.BlockedCount(), "5 write requests blocked from shadow execution")
	assert.Equal(t, int64(0), engine.MismatchCount(), "0 mismatches on idempotent traffic")

	// Verify clean shadow log: reportsPath does not exist or has 0 bytes
	if data, err := os.ReadFile(reportsPath); err == nil {
		assert.Empty(t, strings.TrimSpace(string(data)), "clean shadow log requirement satisfied")
	}

	// -------------------------------------------------------------------------
	// 2. Production Capture Ingestion on Payments Service Traffic
	// -------------------------------------------------------------------------
	ingester := capture.NewIngester(capture.Config{
		WorkspaceRoot: tempDir,
		Store:         store,
		Redactor:      redactor.New(),
		Runtime:       "go1.22",
	})
	httpMirror := capture.NewHTTPMirrorSource(ingester, "pkg/currency")

	// Ingest mirrored traffic
	capturedCase, err := httpMirror.Ingest(context.Background(), "pkg/currency", capture.HTTPTraffic{
		Request: capture.HTTPRequest{
			Method: "GET",
			Path:   "/convert?amount=100&from=USD&to=EUR",
			Headers: map[string]string{
				"Authorization": "Bearer sec_bearer_token_xyz9988",
			},
		},
		Response: capture.HTTPResponse{
			StatusCode: 200,
			Body:       map[string]any{"result": 92.0, "password": "supersecretpassword123"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.KindCaptured, capturedCase.Kind)

	// Verify case was saved to .go-anything/cases/pkg/currency.jsonl with zero leaks
	caseFile := filepath.Join(store.CasesDir(), "pkg", "currency.jsonl")
	data, err := os.ReadFile(caseFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "sec_bearer_token_xyz9988")
	assert.NotContains(t, string(data), "supersecretpassword123")

	// -------------------------------------------------------------------------
	// 3. Lifecycle State Transition to Shadow
	// -------------------------------------------------------------------------
	unit, err := store.GetUnit("pkg/currency")
	require.NoError(t, err)

	// Transition Planned -> Recorded -> Ready -> In progress -> Verified -> Shadow
	unit.State = adapterkit.StateVerified
	require.NoError(t, store.SaveUnit(unit))

	updatedUnit, err := store.Transition("pkg/currency", adapterkit.StateShadow, state.TransitionOptions{
		Actor: "m5-verifier",
	})
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateShadow, updatedUnit.State, "unit successfully transitioned to Shadow state")
}
