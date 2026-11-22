package shadow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/adapters/source/golang/facade"
	"github.com/mohammedrefat/go-anything/internal/shadow"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileReportsSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	schemaPath := filepath.Join("..", "..", "schemas", "v1alpha1", "reports.schema.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020

	f, err := os.Open(schemaPath)
	require.NoError(t, err, "failed to open reports schema file")
	defer f.Close()

	err = compiler.AddResource("reports.schema.json", f)
	require.NoError(t, err)

	schema, err := compiler.Compile("reports.schema.json")
	require.NoError(t, err)
	return schema
}

func TestModeResolution(t *testing.T) {
	// 1. ParseMode
	assert.Equal(t, shadow.ModeNew, shadow.ParseMode("new"))
	assert.Equal(t, shadow.ModeNew, shadow.ParseMode("NEW"))
	assert.Equal(t, shadow.ModeShadow, shadow.ParseMode("shadow"))
	assert.Equal(t, shadow.ModeShadow, shadow.ParseMode("  SHADOW  "))
	assert.Equal(t, shadow.ModeOld, shadow.ParseMode("old"))
	assert.Equal(t, shadow.ModeOld, shadow.ParseMode("invalid"))
	assert.Equal(t, shadow.ModeOld, shadow.ParseMode(""))

	// 2. NormalizeUnitID
	assert.Equal(t, "AUTH", shadow.NormalizeUnitID("auth"))
	assert.Equal(t, "USER_SERVICE", shadow.NormalizeUnitID("user-service"))
	assert.Equal(t, "PKG_API_V1", shadow.NormalizeUnitID("pkg/api.v1"))

	// 3. Global mode
	t.Setenv("GO_ANYTHING_MODE", "shadow")
	assert.Equal(t, shadow.ModeShadow, shadow.GetGlobalMode())

	t.Setenv("GO_ANYTHING_MODE", "new")
	assert.Equal(t, shadow.ModeNew, shadow.GetGlobalMode())

	t.Setenv("GO_ANYTHING_MODE", "")
	assert.Equal(t, shadow.ModeOld, shadow.GetGlobalMode())

	// 4. Per-unit mode: GO_ANYTHING_MODE_<unitID>
	t.Setenv("GO_ANYTHING_MODE", "old")
	t.Setenv("GO_ANYTHING_MODE_AUTH", "new")
	assert.Equal(t, shadow.ModeNew, shadow.GetUnitMode("auth"))
	assert.Equal(t, shadow.ModeOld, shadow.GetUnitMode("billing"))

	// 5. Per-unit mode: GO_ANYTHING_UNIT_<unitID>
	t.Setenv("GO_ANYTHING_UNIT_PAYMENTS", "shadow")
	assert.Equal(t, shadow.ModeShadow, shadow.GetUnitMode("payments"))

	// 6. Unit-specific overrides global
	t.Setenv("GO_ANYTHING_MODE", "old")
	t.Setenv("GO_ANYTHING_MODE_SEARCH", "shadow")
	assert.Equal(t, shadow.ModeShadow, shadow.GetUnitMode("search"))
}

func TestIdempotencyPolicy_BlockedByDefault(t *testing.T) {
	// Idempotent methods: GET, HEAD
	assert.True(t, shadow.IsIdempotentMethod("GET"))
	assert.True(t, shadow.IsIdempotentMethod("HEAD"))
	assert.True(t, shadow.IsIdempotentMethod("get"))
	assert.True(t, shadow.IsIdempotentMethod("head"))

	// Non-idempotent methods: POST, PUT, DELETE, PATCH, etc.
	assert.False(t, shadow.IsIdempotentMethod("POST"))
	assert.False(t, shadow.IsIdempotentMethod("PUT"))
	assert.False(t, shadow.IsIdempotentMethod("DELETE"))
	assert.False(t, shadow.IsIdempotentMethod("PATCH"))
	assert.False(t, shadow.IsIdempotentMethod("OPTIONS"))

	engine := shadow.New(
		shadow.WithAllowSideEffects(false),
		shadow.WithWorkerPoolSize(2),
		shadow.WithQueueCapacity(10),
	)
	defer engine.Close()

	// GET and HEAD allowed
	allowed, _ := engine.ShouldShadow("GET")
	assert.True(t, allowed)
	allowed, _ = engine.ShouldShadow("HEAD")
	assert.True(t, allowed)
	assert.Equal(t, int64(0), engine.BlockedCount())

	// POST, PUT, DELETE blocked by default
	blockedMethods := []string{"POST", "PUT", "DELETE", "PATCH"}
	for i, method := range blockedMethods {
		allowed, reason := engine.ShouldShadow(method)
		assert.False(t, allowed, "method %s must be blocked by default", method)
		assert.Contains(t, reason, "blocked")
		assert.Equal(t, int64(i+1), engine.BlockedCount())
	}
}

func TestAllowSideEffects_WarningAndAllowed(t *testing.T) {
	var warnBuf bytes.Buffer

	engine := shadow.New(
		shadow.WithAllowSideEffects(true),
		shadow.WithWarningWriter(&warnBuf),
		shadow.WithWorkerPoolSize(2),
		shadow.WithQueueCapacity(10),
	)
	defer engine.Close()

	// 1. Verify startup warning exact text
	expectedWarning := "Shadow execution runs the Go handler on real requests. Non-idempotent handlers may repeat writes or external calls. Run against isolated data or stubs. You are responsible for the consequences.\n"
	assert.Equal(t, expectedWarning, warnBuf.String(), "startup warning must match v3 plan exactly")

	// 2. Non-idempotent methods are permitted when AllowSideEffects=true
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "GET", "HEAD"} {
		allowed, reason := engine.ShouldShadow(method)
		assert.True(t, allowed, "method %s should be allowed with AllowSideEffects=true: %s", method, reason)
	}
	assert.Equal(t, int64(0), engine.BlockedCount())
}

func TestSampleRate_Filtering(t *testing.T) {
	// 1. Sample rate 0.0: all requests skipped
	e0 := shadow.New(
		shadow.WithSampleRate(0.0),
		shadow.WithWorkerPoolSize(1),
	)
	defer e0.Close()

	for i := 0; i < 20; i++ {
		allowed, _ := e0.ShouldShadow("GET")
		assert.False(t, allowed)
	}
	assert.Equal(t, int64(20), e0.SampleSkippedCount())

	// 2. Sample rate 1.0: all eligible requests accepted
	e1 := shadow.New(
		shadow.WithSampleRate(1.0),
		shadow.WithWorkerPoolSize(1),
	)
	defer e1.Close()

	for i := 0; i < 20; i++ {
		allowed, _ := e1.ShouldShadow("GET")
		assert.True(t, allowed)
	}
	assert.Equal(t, int64(0), e1.SampleSkippedCount())

	// 3. Sample rate 0.5: statistically samples ~50%
	e50 := shadow.New(
		shadow.WithSampleRate(0.5),
		shadow.WithRandomSource(rand.NewSource(12345)),
		shadow.WithWorkerPoolSize(1),
	)
	defer e50.Close()

	sampled := 0
	total := 1000
	for i := 0; i < total; i++ {
		if ok, _ := e50.ShouldShadow("GET"); ok {
			sampled++
		}
	}
	assert.InDelta(t, 500, sampled, 50, "sample rate 0.5 should sample ~50%% of requests")
	assert.Equal(t, int64(total-sampled), e50.SampleSkippedCount())
}

func TestFailClosedInMemRedaction_NoSecretsInMismatchesLog(t *testing.T) {
	tempDir := t.TempDir()
	reportsPath := filepath.Join(tempDir, "shadow_mismatches.jsonl")

	engine := shadow.New(
		shadow.WithUnitID("auth-service"),
		shadow.WithReportsPath(reportsPath),
		shadow.WithWorkerPoolSize(2),
		shadow.WithQueueCapacity(20),
	)
	defer engine.Close()

	// High-confidence real secrets
	ghToken := "ghp_0123456789abcdef0123456789abcdef0123"
	awsKey := "AKIAIOSFODNN7EXAMPLE"
	stripeKey := "sk_test_" + "0123456789abcdef0123456789abcdef01234567"
	slackToken := "xoxb-" + "test-token-1234567890-abcdef"
	secretEmail := "chief_security_officer@megacorp.internal"
	secretPassword := "superSecretMasterPassword123!"
	secretAPIKey := "sec_live_key_998877665544332211"
	creditCard := "4532015112830366" // Valid Luhn visa candidate

	// Log a mismatch containing secrets across path, expected, actual, and details
	err := engine.LogMismatch(shadow.MismatchRecord{
		UnitID:     "auth-service",
		ReportType: string(adapterkit.ReportTypeDiff),
		Status:     string(adapterkit.StatusFail),
		Summary:    fmt.Sprintf("Mismatch with token %s and aws %s", ghToken, awsKey),
		Mismatches: []adapterkit.Mismatch{
			{
				Path:     "credentials.token",
				Expected: ghToken,
				Actual:   slackToken,
				Message:  fmt.Sprintf("token mismatch: expected %s got %s", ghToken, slackToken),
			},
			{
				Path:     "payment.stripe_key",
				Expected: stripeKey,
				Actual:   "sk_test_" + "replaced",
				Message:  "stripe key mismatch",
			},
			{
				Path:     "user.card",
				Expected: creditCard,
				Actual:   "4532015112830366",
				Message:  "credit card mismatch",
			},
		},
		Details: map[string]any{
			"password": secretPassword,
			"api_key":  secretAPIKey,
			"email":    secretEmail,
			"auth_hdr": "Bearer " + ghToken,
		},
	})
	require.NoError(t, err)

	// Read raw content from shadow_mismatches.jsonl
	contentBytes, err := os.ReadFile(reportsPath)
	require.NoError(t, err)
	content := string(contentBytes)

	// Verify that NO raw secret exists in the file
	assert.NotContains(t, content, ghToken, "GitHub token must be redacted before disk write")
	assert.NotContains(t, content, awsKey, "AWS key must be redacted before disk write")
	assert.NotContains(t, content, stripeKey, "Stripe key must be redacted before disk write")
	assert.NotContains(t, content, slackToken, "Slack token must be redacted before disk write")
	assert.NotContains(t, content, secretEmail, "Email address must be redacted before disk write")
	assert.NotContains(t, content, secretPassword, "Password field must be redacted before disk write")
	assert.NotContains(t, content, secretAPIKey, "API key field must be redacted before disk write")
	assert.NotContains(t, content, creditCard, "Credit card must be redacted before disk write")

	// Verify file is valid JSON conforming to schemas/v1alpha1/reports.schema.json
	lines := strings.Split(strings.TrimSpace(content), "\n")
	require.Len(t, lines, 1)

	schema := compileReportsSchema(t)
	var parsed any
	err = json.Unmarshal([]byte(lines[0]), &parsed)
	require.NoError(t, err, "logged line must be valid JSON")

	err = schema.Validate(parsed)
	require.NoError(t, err, "logged mismatch must conform to reports.schema.json")
}

func TestBoundedWorkerPool_LoadTest_BoundedMemoryUnderOverflow(t *testing.T) {
	// Worker blocks until released, queue capacity of 5
	workerBlocker := make(chan struct{})

	engine := shadow.New(
		shadow.WithWorkerPoolSize(2),
		shadow.WithQueueCapacity(5),
		shadow.WithAllowSideEffects(true),
	)

	// Enqueue tasks that block the 2 workers
	for i := 0; i < 2; i++ {
		engine.Submit(func(ctx context.Context) {
			<-workerBlocker
		})
	}

	// Wait briefly for workers to pick up the 2 blocking tasks
	time.Sleep(20 * time.Millisecond)

	// Take baseline memory reading
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Flood with 50,000 tasks while queue capacity is only 5
	totalTasks := 50000
	for i := 0; i < totalTasks; i++ {
		engine.Submit(func(ctx context.Context) {
			// dummy task
		})
	}

	// Memory reading immediately after massive flood
	var memDuring runtime.MemStats
	runtime.ReadMemStats(&memDuring)

	// Bounded queue: heap allocation must NOT explode
	// Alloc delta should remain tightly bounded (less than 20MB under 50k tasks)
	allocDeltaMB := float64(memDuring.HeapAlloc-memBefore.HeapAlloc) / (1024 * 1024)
	t.Logf("HeapAlloc delta under 50k overflow tasks: %.2f MB", allocDeltaMB)
	assert.Less(t, allocDeltaMB, 20.0, "memory must remain strictly bounded under overflow")

	// Dropped count must account for all overflow (50,000 submitted - max 5 queued)
	dropped := engine.DroppedCount()
	assert.Greater(t, dropped, int64(totalTasks-10), "overflow tasks must be dropped and counted")

	// Release workers and close cleanly
	close(workerBlocker)
	engine.Close()
}

func TestHTTPExecution_ModesAndIdempotency(t *testing.T) {
	tempDir := t.TempDir()
	reportsPath := filepath.Join(tempDir, "shadow_mismatches.jsonl")

	var oldCalls, newCalls atomic.Int64

	oldHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Backend", "old")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("legacy: " + string(body)))
	})

	newHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		newCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Backend", "new")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("modern: " + string(body))) // intentional mismatch
	})

	engine := shadow.New(
		shadow.WithUnitID("test-http"),
		shadow.WithReportsPath(reportsPath),
		shadow.WithWorkerPoolSize(2),
		shadow.WithQueueCapacity(20),
		shadow.WithMode(shadow.ModeShadow),
		shadow.WithAllowSideEffects(false), // POST blocked by default!
	)
	defer engine.Close()

	handler := engine.WrapHTTP(oldHandler, newHandler)

	// 1. GET request: idempotent, shadowed!
	reqGet := httptest.NewRequest(http.MethodGet, "/resource", nil)
	recGet := httptest.NewRecorder()
	handler.ServeHTTP(recGet, reqGet)

	assert.Equal(t, http.StatusOK, recGet.Code)
	assert.Equal(t, "old", recGet.Header().Get("X-Backend"))
	assert.Equal(t, "legacy: ", recGet.Body.String())

	// Wait for shadow evaluation
	assert.Eventually(t, func() bool {
		return engine.ShadowRuns() == 1 && engine.MismatchCount() == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. POST request: non-idempotent, BLOCKED by default!
	reqPost := httptest.NewRequest(http.MethodPost, "/resource", bytes.NewBufferString("payload"))
	recPost := httptest.NewRecorder()
	handler.ServeHTTP(recPost, reqPost)

	assert.Equal(t, http.StatusOK, recPost.Code)
	assert.Equal(t, "old", recPost.Header().Get("X-Backend"))
	assert.Equal(t, "legacy: payload", recPost.Body.String())

	// Post must be blocked: BlockedCount incremented, ShadowRuns unchanged!
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), engine.BlockedCount())
	assert.Equal(t, int64(1), engine.ShadowRuns(), "POST must not trigger shadow execution")

	// 3. Verify mismatch log was written for GET mismatch
	content, err := os.ReadFile(reportsPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Shadow mismatch on GET /resource")
}

func TestHTTPExecution_AllowSideEffects_PermitsPost(t *testing.T) {
	tempDir := t.TempDir()
	reportsPath := filepath.Join(tempDir, "shadow_mismatches.jsonl")

	var newRuns atomic.Int64

	oldH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	newH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		newRuns.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	engine := shadow.New(
		shadow.WithReportsPath(reportsPath),
		shadow.WithWorkerPoolSize(2),
		shadow.WithMode(shadow.ModeShadow),
		shadow.WithAllowSideEffects(true), // Side effects enabled!
	)
	defer engine.Close()

	handler := engine.WrapHTTP(oldH, newH)

	// POST request is now permitted to shadow
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewBufferString("data"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	assert.Eventually(t, func() bool {
		return engine.ShadowRuns() == 1 && newRuns.Load() == 1
	}, 1*time.Second, 10*time.Millisecond)
	assert.Equal(t, int64(0), engine.BlockedCount())
}

func TestFacadeShadow_Integration(t *testing.T) {
	tempDir := t.TempDir()
	reportsPath := filepath.Join(tempDir, "shadow_mismatches.jsonl")

	type HTTPRequest struct {
		Method string
		Body   string
	}

	var newCalls atomic.Int64

	oldFn := func(_ context.Context, r HTTPRequest) (string, error) {
		return "old-" + r.Body, nil
	}

	newFn := func(_ context.Context, r HTTPRequest) (string, error) {
		newCalls.Add(1)
		return "new-" + r.Body, nil // Mismatch!
	}

	shadowHelper := facade.NewShadow(oldFn, newFn, nil,
		facade.WithWorkerPoolSize[HTTPRequest, string](2),
		facade.WithQueueCapacity[HTTPRequest, string](10),
		facade.WithReportsPath[HTTPRequest, string](reportsPath),
		facade.WithAllowSideEffects[HTTPRequest, string](false), // POST blocked
		facade.WithMethodExtractor[HTTPRequest, string](func(r HTTPRequest) string {
			return r.Method
		}),
	)
	defer shadowHelper.Close()

	ctx := context.Background()

	// 1. POST is blocked by default
	res, err := shadowHelper.Execute(ctx, HTTPRequest{Method: "POST", Body: "item1"})
	require.NoError(t, err)
	assert.Equal(t, "old-item1", res)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), shadowHelper.BlockedCount(), "POST must be blocked by facade shadow")
	assert.Equal(t, int64(0), shadowHelper.ShadowRuns())

	// 2. GET is shadowed
	res, err = shadowHelper.Execute(ctx, HTTPRequest{Method: "GET", Body: "item2"})
	require.NoError(t, err)
	assert.Equal(t, "old-item2", res)

	assert.Eventually(t, func() bool {
		return shadowHelper.ShadowRuns() == 1 && shadowHelper.MismatchCount() == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 3. Mismatch log was written
	content, err := os.ReadFile(reportsPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "shadow output mismatch")
}

func TestHighConcurrency_RaceFree(t *testing.T) {
	tempDir := t.TempDir()
	reportsPath := filepath.Join(tempDir, "shadow_mismatches.jsonl")

	engine := shadow.New(
		shadow.WithReportsPath(reportsPath),
		shadow.WithWorkerPoolSize(8),
		shadow.WithQueueCapacity(100),
		shadow.WithMode(shadow.ModeShadow),
		shadow.WithAllowSideEffects(true),
		shadow.WithSampleRate(0.8),
	)
	defer engine.Close()

	oldH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("resp"))
	})
	newH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("resp"))
	})

	handler := engine.WrapHTTP(oldH, newH)

	var wg sync.WaitGroup
	workers := 20
	iterations := 50

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				method := "GET"
				if i%2 == 0 {
					method = "POST"
				}
				req := httptest.NewRequest(method, fmt.Sprintf("/res/%d", i), nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Errorf("expected 200, got %d", rec.Code)
				}
			}
		}(w)
	}

	wg.Wait()
	assert.Equal(t, int64(workers*iterations), engine.TotalCalls())
}
