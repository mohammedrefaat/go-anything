// Package shadow provides live traffic shadowing, side-effect safety guards,
// in-memory secret redaction, and bounded queueing for ported units.
package shadow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mohammedrefat/go-anything/internal/redactor"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// Option configures the shadow Engine.
type Option func(*Engine)

// WithPolicy sets the complete shadow policy.
func WithPolicy(p Policy) Option {
	return func(e *Engine) {
		e.policy = p
	}
}

// WithAllowSideEffects gates non-idempotent shadowing (POST, PUT, DELETE).
func WithAllowSideEffects(allow bool) Option {
	return func(e *Engine) {
		e.policy.AllowSideEffects = allow
	}
}

// WithSampleRate sets the sample rate for shadow execution (0.0 to 1.0).
func WithSampleRate(rate float64) Option {
	return func(e *Engine) {
		if rate < 0.0 {
			rate = 0.0
		} else if rate > 1.0 {
			rate = 1.0
		}
		e.policy.SampleRate = rate
	}
}

// WithWorkerPoolSize sets the number of concurrent worker goroutines.
func WithWorkerPoolSize(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.policy.Workers = n
		}
	}
}

// WithQueueCapacity sets the maximum capacity of the pending shadow task queue.
func WithQueueCapacity(capacity int) Option {
	return func(e *Engine) {
		if capacity > 0 {
			e.policy.QueueCap = capacity
		}
	}
}

// WithReportsPath sets the file path for shadow mismatch records.
func WithReportsPath(path string) Option {
	return func(e *Engine) {
		if path != "" {
			e.policy.ReportsPath = path
		}
	}
}

// WithWarningWriter configures where the startup warning is written.
func WithWarningWriter(w io.Writer) Option {
	return func(e *Engine) {
		if w != nil {
			e.policy.WarningWriter = w
		}
	}
}

// WithUnitID sets the migration unit ID.
func WithUnitID(unitID string) Option {
	return func(e *Engine) {
		e.policy.UnitID = unitID
	}
}

// WithMode sets the initial routing mode override.
func WithMode(m Mode) Option {
	return func(e *Engine) {
		e.policy.Mode = m
		e.explicitMode = true
	}
}

// WithRedactor supplies a custom redactor instance.
func WithRedactor(r *redactor.Redactor) Option {
	return func(e *Engine) {
		e.red = r
	}
}

// WithRandomSource supplies a custom pseudo-random source for deterministic sampling in tests.
func WithRandomSource(src rand.Source) Option {
	return func(e *Engine) {
		if src != nil {
			e.policy.Rng = rand.New(src)
		}
	}
}

// Engine coordinates shadow traffic mirroring across old and new implementations.
type Engine struct {
	policy       Policy
	explicitMode bool
	red          *redactor.Redactor
	logger       *MismatchLogger
	queue        chan func(context.Context)

	totalCalls    atomic.Int64
	shadowRuns    atomic.Int64
	droppedCount  atomic.Int64
	blockedCount  atomic.Int64
	sampleSkipped atomic.Int64
	mismatches    atomic.Int64

	mu     sync.RWMutex
	rngMu  sync.Mutex
	closed atomic.Bool
	wg     sync.WaitGroup
}

// New returns a new configured shadow Engine.
func New(opts ...Option) *Engine {
	e := &Engine{
		policy: DefaultPolicy(),
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.policy.Workers <= 0 {
		e.policy.Workers = runtime.NumCPU()
	}
	if e.policy.QueueCap <= 0 {
		e.policy.QueueCap = 100
	}
	if e.red == nil {
		e.red = redactor.New()
	}

	e.logger = NewMismatchLogger(e.policy.ReportsPath, e.red)
	e.queue = make(chan func(context.Context), e.policy.QueueCap)

	// Emit startup warning if side effects are permitted
	if e.policy.AllowSideEffects {
		e.policy.EmitStartupWarning()
	}

	// Start bounded worker pool
	for i := 0; i < e.policy.Workers; i++ {
		e.wg.Add(1)
		go e.workerLoop()
	}

	return e
}

// SetMode dynamically overrides the routing mode.
func (e *Engine) SetMode(m Mode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policy.Mode = m
	e.explicitMode = true
}

// GetMode returns the current active routing mode, checking unit-specific and global env vars
// if no explicit mode was configured.
func (e *Engine) GetMode() Mode {
	e.mu.RLock()
	explicit := e.explicitMode
	configured := e.policy.Mode
	unitID := e.policy.UnitID
	e.mu.RUnlock()

	if explicit {
		return configured
	}

	if unitID != "" {
		return GetUnitMode(unitID)
	}
	return GetGlobalMode()
}

// Policy returns a copy of the active shadow policy.
func (e *Engine) Policy() Policy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.policy
}

// ShouldShadow evaluates whether a request with the given method is eligible for shadow execution.
// Returns false if blocked by idempotency policy or skipped by sample rate.
func (e *Engine) ShouldShadow(method string) (bool, string) {
	e.mu.RLock()
	allowSideEffects := e.policy.AllowSideEffects
	sampleRate := e.policy.SampleRate
	rng := e.policy.Rng
	e.mu.RUnlock()

	// 1. Idempotency safety: GET and HEAD only by default
	if method != "" && !IsIdempotentMethod(method) && !allowSideEffects {
		e.blockedCount.Add(1)
		return false, fmt.Sprintf("blocked: non-idempotent method %q blocked by default (allow-side-effects=false)", method)
	}

	// 2. Sample rate evaluation
	if sampleRate <= 0.0 {
		e.sampleSkipped.Add(1)
		return false, "skipped: sample rate is 0.0"
	}
	if sampleRate < 1.0 {
		var roll float64
		if rng != nil {
			e.rngMu.Lock()
			roll = rng.Float64()
			e.rngMu.Unlock()
		} else {
			roll = rand.Float64()
		}
		if roll >= sampleRate {
			e.sampleSkipped.Add(1)
			return false, fmt.Sprintf("skipped: sample roll (%.4f) >= sample rate (%.4f)", roll, sampleRate)
		}
	}

	return true, ""
}

// Submit attempts to enqueue a shadow task to the bounded pool.
// If the queue is full, drops immediately and increments DroppedCount.
func (e *Engine) Submit(task func(context.Context)) bool {
	if e.closed.Load() {
		return false
	}

	select {
	case e.queue <- task:
		return true
	default:
		e.droppedCount.Add(1)
		return false
	}
}

func (e *Engine) workerLoop() {
	defer e.wg.Done()
	for task := range e.queue {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Guard against panics in shadow handlers
					e.mismatches.Add(1)
					_ = e.logger.LogMismatch(MismatchRecord{
						UnitID:     e.policy.UnitID,
						ReportType: string(adapterkit.ReportTypeDiff),
						Status:     string(adapterkit.StatusFail),
						Mismatches: []adapterkit.Mismatch{
							{
								Path:    "panic",
								Message: fmt.Sprintf("panic in shadow execution: %v", r),
							},
						},
						Summary: fmt.Sprintf("PANIC in shadow handler: %v", r),
					})
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			task(ctx)
			cancel()
		}()
	}
}

// ExecuteHTTP routes an incoming HTTP request across old and new handlers.
// ModeOld: executes oldHandler only.
// ModeNew: executes newHandler only.
// ModeShadow: executes oldHandler synchronously as primary response, and shadows
// newHandler in background if allowed by idempotency and sample rate.
func (e *Engine) ExecuteHTTP(w http.ResponseWriter, req *http.Request, oldH, newH http.Handler) {
	e.totalCalls.Add(1)
	mode := e.GetMode()

	switch mode {
	case ModeNew:
		newH.ServeHTTP(w, req)
	case ModeOld:
		oldH.ServeHTTP(w, req)
	case ModeShadow:
		// Read and buffer request body so both old and new handlers can read it
		var bodyBytes []byte
		if req.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(req.Body)
			if err == nil {
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		// 1. Run legacy handler synchronously as primary response
		oldRec := httptest.NewRecorder()
		oldH.ServeHTTP(oldRec, req)

		// Forward primary response to client
		for k, vv := range oldRec.Header() {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(oldRec.Code)
		_, _ = w.Write(oldRec.Body.Bytes())

		// 2. Check if shadow execution is permitted
		allowed, _ := e.ShouldShadow(req.Method)
		if !allowed {
			return
		}

		// 3. Clone request for async shadow evaluation
		if !e.closed.Load() {
			clonedReq := req.Clone(context.Background())
			if bodyBytes != nil {
				clonedReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			oldStatus := oldRec.Code
			oldBody := oldRec.Body.Bytes()
			oldHeaders := oldRec.Header().Clone()

			e.Submit(func(ctx context.Context) {
				e.shadowRuns.Add(1)
				shadowRec := httptest.NewRecorder()
				newH.ServeHTTP(shadowRec, clonedReq.WithContext(ctx))

				// Compare responses
				var mismatches []adapterkit.Mismatch
				if shadowRec.Code != oldStatus {
					mismatches = append(mismatches, adapterkit.Mismatch{
						Path:     "status_code",
						Expected: oldStatus,
						Actual:   shadowRec.Code,
						Message:  fmt.Sprintf("status code mismatch: %d != %d", oldStatus, shadowRec.Code),
					})
				}
				if !bytes.Equal(shadowRec.Body.Bytes(), oldBody) {
					mismatches = append(mismatches, adapterkit.Mismatch{
						Path:     "body",
						Expected: string(oldBody),
						Actual:   shadowRec.Body.String(),
						Message:  "response body mismatch",
					})
				}
				allKeys := make(map[string]bool)
				for k := range oldHeaders {
					allKeys[http.CanonicalHeaderKey(k)] = true
				}
				for k := range shadowRec.Header() {
					allKeys[http.CanonicalHeaderKey(k)] = true
				}
				for k := range allKeys {
					oldVals := oldHeaders.Values(k)
					newVals := shadowRec.Header().Values(k)
					if !slices.Equal(oldVals, newVals) {
						mismatches = append(mismatches, adapterkit.Mismatch{
							Path:     "headers." + k,
							Expected: oldVals,
							Actual:   newVals,
							Message:  "header mismatch for " + k,
						})
					}
				}

				if len(mismatches) > 0 {
					e.mismatches.Add(1)
					_ = e.logger.LogMismatch(MismatchRecord{
						SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
						UnitID:        e.policy.UnitID,
						ReportType:    string(adapterkit.ReportTypeDiff),
						Mode:          "http_contract",
						Status:        string(adapterkit.StatusFail),
						Mismatches:    mismatches,
						Summary:       fmt.Sprintf("Shadow mismatch on %s %s: %d differences", req.Method, req.URL.Path, len(mismatches)),
						Details: map[string]any{
							"method":     req.Method,
							"path":       req.URL.Path,
							"old_status": oldStatus,
							"new_status": shadowRec.Code,
							"old_body":   string(oldBody),
							"new_body":   shadowRec.Body.String(),
						},
					})
				}
			})
		}
	default:
		oldH.ServeHTTP(w, req)
	}
}

// WrapHTTP wraps old and new HTTP handlers into an http.Handler managed by the shadow Engine.
func (e *Engine) WrapHTTP(oldH, newH http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		e.ExecuteHTTP(w, req, oldH, newH)
	})
}

// LogMismatch logs a shadow mismatch report after passing it through fail-closed in-memory redaction.
func (e *Engine) LogMismatch(rec MismatchRecord) error {
	return e.logger.LogMismatch(rec)
}

// TotalCalls returns the total number of calls received.
func (e *Engine) TotalCalls() int64 { return e.totalCalls.Load() }

// ShadowRuns returns the total number of completed shadow evaluations.
func (e *Engine) ShadowRuns() int64 { return e.shadowRuns.Load() }

// DroppedCount returns the count of shadow tasks dropped due to worker pool overflow.
func (e *Engine) DroppedCount() int64 { return e.droppedCount.Load() }

// BlockedCount returns the count of requests blocked from shadowing due to non-idempotency.
func (e *Engine) BlockedCount() int64 { return e.blockedCount.Load() }

// SampleSkippedCount returns the count of requests skipped from shadowing due to sample rate.
func (e *Engine) SampleSkippedCount() int64 { return e.sampleSkipped.Load() }

// MismatchCount returns the count of observed behavioral mismatches.
func (e *Engine) MismatchCount() int64 { return e.mismatches.Load() }

// Close stops the worker pool and flushes pending tasks.
func (e *Engine) Close() {
	if e.closed.CompareAndSwap(false, true) {
		close(e.queue)
		e.wg.Wait()
	}
}
