package facade

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mohammedrefaat/go-anything/internal/shadow"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// ShadowOption configures a Shadow instance.
type ShadowOption[T any, R any] func(*Shadow[T, R])

// WithWorkerPoolSize sets the number of concurrent shadow worker goroutines.
func WithWorkerPoolSize[T any, R any](n int) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		if n > 0 {
			s.workers = n
		}
	}
}

// WithQueueCapacity sets the maximum capacity of the shadow task queue before dropping.
func WithQueueCapacity[T any, R any](capacity int) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		if capacity > 0 {
			s.queueCap = capacity
		}
	}
}

// WithMode sets the initial mode for the Shadow instance.
func WithMode[T any, R any](m Mode) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		s.mode = m
		s.modeExplicit = true
	}
}

// WithOnMismatch sets a callback invoked when a shadow diff is observed.
func WithOnMismatch[T any, R any](cb func(input T, oldRes, newRes R, oldErr, newErr error)) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		s.onMismatch = cb
	}
}

// WithComparator sets a custom comparison function for outputs.
func WithComparator[T any, R any](cmpFn func(a, b R) bool) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		s.customCmp = cmpFn
	}
}

// WithAllowSideEffects gates shadowing of non-idempotent requests.
func WithAllowSideEffects[T any, R any](allow bool) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		s.allowSideEffects = allow
	}
}

// WithSampleRate sets the sample rate for shadow requests (0.0 to 1.0).
func WithSampleRate[T any, R any](rate float64) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		if rate < 0.0 {
			rate = 0.0
		} else if rate > 1.0 {
			rate = 1.0
		}
		s.sampleRate = rate
	}
}

// WithUnitID sets the migration unit ID for per-unit mode resolution and reporting.
func WithUnitID[T any, R any](unitID string) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		s.unitID = unitID
	}
}

// WithReportsPath specifies the file path where shadow mismatch logs are appended.
func WithReportsPath[T any, R any](path string) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		s.reportsPath = path
	}
}

// WithMethodExtractor configures a custom function to determine the HTTP/action method from input.
func WithMethodExtractor[T any, R any](fn func(T) string) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		s.methodExtractor = fn
	}
}

// WithPolicy applies an existing shadow.Policy to the Shadow instance.
func WithPolicy[T any, R any](p shadow.Policy) ShadowOption[T, R] {
	return func(s *Shadow[T, R]) {
		s.allowSideEffects = p.AllowSideEffects
		s.sampleRate = p.SampleRate
		if p.Workers > 0 {
			s.workers = p.Workers
		}
		if p.QueueCap > 0 {
			s.queueCap = p.QueueCap
		}
		if p.ReportsPath != "" {
			s.reportsPath = p.ReportsPath
		}
		if p.UnitID != "" {
			s.unitID = p.UnitID
		}
		if p.Mode != "" {
			s.mode = Mode(p.Mode)
			s.modeExplicit = true
		}
	}
}

type shadowTask[T any, R any] struct {
	input  T
	oldRes R
	oldErr error
	method string
}

// Shadow routes function execution across old and new implementations with in-process
// bounded shadow execution.
type Shadow[T any, R any] struct {
	oldFn            func(context.Context, T) (R, error)
	newFn            func(context.Context, T) (R, error)
	cloneFn          func(T) T
	mode             Mode
	modeExplicit     bool
	workers          int
	queueCap         int
	queue            chan shadowTask[T, R]
	onMismatch       func(input T, oldRes, newRes R, oldErr, newErr error)
	customCmp        func(a, b R) bool
	allowSideEffects bool
	sampleRate       float64
	unitID           string
	reportsPath      string
	methodExtractor  func(T) string
	engine           *shadow.Engine

	totalCalls atomic.Int64
	shadowRuns atomic.Int64
	dropped    atomic.Int64
	mismatches atomic.Int64

	mu     sync.RWMutex
	closed atomic.Bool
	wg     sync.WaitGroup
}

// NewShadow creates a new generic in-process Shadow helper.
func NewShadow[T any, R any](
	oldFn func(context.Context, T) (R, error),
	newFn func(context.Context, T) (R, error),
	cloneFn func(T) T,
	opts ...ShadowOption[T, R],
) *Shadow[T, R] {
	s := &Shadow[T, R]{
		oldFn:      oldFn,
		newFn:      newFn,
		cloneFn:    cloneFn,
		mode:       ModeShadow,
		workers:    runtime.NumCPU(),
		queueCap:   100,
		sampleRate: 1.0,
	}

	for _, opt := range opts {
		opt(s)
	}

	engineOpts := []shadow.Option{
		shadow.WithWorkerPoolSize(1), // minimal internal engine pool; shadow manages its own worker pool
		shadow.WithQueueCapacity(s.queueCap),
		shadow.WithAllowSideEffects(s.allowSideEffects),
		shadow.WithSampleRate(s.sampleRate),
		shadow.WithUnitID(s.unitID),
		shadow.WithReportsPath(s.reportsPath),
	}
	if s.modeExplicit {
		engineOpts = append(engineOpts, shadow.WithMode(shadow.Mode(s.mode)))
	}
	s.engine = shadow.New(engineOpts...)

	s.queue = make(chan shadowTask[T, R], s.queueCap)

	// Start bounded worker pool
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.workerLoop()
	}

	return s
}

// SetMode dynamically changes the routing mode.
func (s *Shadow[T, R]) SetMode(m Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = m
	s.modeExplicit = true
	if s.engine != nil {
		s.engine.SetMode(shadow.Mode(m))
	}
}

// GetMode returns the current routing mode.
func (s *Shadow[T, R]) GetMode() Mode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.modeExplicit {
		return s.mode
	}
	if s.unitID != "" {
		return Mode(shadow.GetUnitMode(s.unitID))
	}
	return s.mode
}

// Execute evaluates the input through the configured mode.
func (s *Shadow[T, R]) Execute(ctx context.Context, input T) (R, error) {
	s.totalCalls.Add(1)
	mode := s.GetMode()

	switch mode {
	case ModeNew:
		return s.newFn(ctx, input)
	case ModeOld:
		return s.oldFn(ctx, input)
	case ModeShadow:
		// 1. Run legacy synchronously as primary response
		oldRes, oldErr := s.oldFn(ctx, input)

		// 2. Check policy (idempotency, sample rate)
		var method string
		if s.methodExtractor != nil {
			method = s.methodExtractor(input)
		} else if req, ok := any(input).(*http.Request); ok {
			method = req.Method
		} else if m, ok := any(input).(interface{ Method() string }); ok {
			method = m.Method()
		} else if m, ok := any(input).(interface{ HTTPMethod() string }); ok {
			method = m.HTTPMethod()
		}

		if s.engine != nil {
			allowed, _ := s.engine.ShouldShadow(method)
			if !allowed {
				return oldRes, oldErr
			}
		}

		// 3. Clone input for decoupled shadow evaluation
		var clonedInput T
		if s.cloneFn != nil {
			clonedInput = s.cloneFn(input)
		} else {
			clonedInput = input
		}

		// 4. Attempt non-blocking enqueue into bounded shadow pool
		if !s.closed.Load() {
			task := shadowTask[T, R]{
				input:  clonedInput,
				oldRes: oldRes,
				oldErr: oldErr,
				method: method,
			}
			select {
			case s.queue <- task:
				// Enqueued successfully
			default:
				// Queue full: drop and increment overflow counter immediately!
				s.dropped.Add(1)
			}
		}

		return oldRes, oldErr
	default:
		return s.oldFn(ctx, input)
	}
}

func (s *Shadow[T, R]) workerLoop() {
	defer s.wg.Done()
	for task := range s.queue {
		s.evaluateShadow(task)
	}
}

func (s *Shadow[T, R]) evaluateShadow(task shadowTask[T, R]) {
	s.shadowRuns.Add(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	newRes, newErr := s.newFn(ctx, task.input)

	// Compare outputs
	mismatch := false
	if (task.oldErr == nil) != (newErr == nil) {
		mismatch = true
	} else if task.oldErr != nil && newErr != nil && task.oldErr.Error() != newErr.Error() {
		mismatch = true
	} else if task.oldErr == nil {
		if s.customCmp != nil {
			if !s.customCmp(task.oldRes, newRes) {
				mismatch = true
			}
		} else if !cmp.Equal(task.oldRes, newRes) {
			mismatch = true
		}
	}

	if mismatch {
		s.mismatches.Add(1)
		if s.onMismatch != nil {
			s.onMismatch(task.input, task.oldRes, newRes, task.oldErr, newErr)
		}

		// Fail-closed in-memory redacted logging to shadow_mismatches.jsonl
		if s.engine != nil {
			_ = s.engine.LogMismatch(shadow.MismatchRecord{
				UnitID:     s.unitID,
				ReportType: string(adapterkit.ReportTypeDiff),
				Status:     string(adapterkit.StatusFail),
				Mismatches: []adapterkit.Mismatch{
					{
						Path:     "output",
						Expected: fmt.Sprintf("%v", task.oldRes),
						Actual:   fmt.Sprintf("%v", newRes),
						Message:  fmt.Sprintf("shadow output mismatch (oldErr=%v, newErr=%v)", task.oldErr, newErr),
					},
				},
				Details: map[string]any{
					"method":     task.method,
					"old_result": fmt.Sprintf("%v", task.oldRes),
					"new_result": fmt.Sprintf("%v", newRes),
					"old_error":  fmt.Sprintf("%v", task.oldErr),
					"new_error":  fmt.Sprintf("%v", newErr),
				},
			})
		}
	}
}

// TotalCalls returns the count of all calls through Execute.
func (s *Shadow[T, R]) TotalCalls() int64 { return s.totalCalls.Load() }

// ShadowRuns returns the count of completed shadow evaluations.
func (s *Shadow[T, R]) ShadowRuns() int64 { return s.shadowRuns.Load() }

// DroppedCount returns the count of dropped calls due to worker pool overflow.
func (s *Shadow[T, R]) DroppedCount() int64 { return s.dropped.Load() }

// BlockedCount returns the count of requests blocked from shadowing due to non-idempotency.
func (s *Shadow[T, R]) BlockedCount() int64 {
	if s.engine != nil {
		return s.engine.BlockedCount()
	}
	return 0
}

// SampleSkippedCount returns the count of requests skipped from shadowing due to sample rate.
func (s *Shadow[T, R]) SampleSkippedCount() int64 {
	if s.engine != nil {
		return s.engine.SampleSkippedCount()
	}
	return 0
}

// MismatchCount returns the count of detected behavioral differences.
func (s *Shadow[T, R]) MismatchCount() int64 { return s.mismatches.Load() }

// Close stops the worker pool and flushes pending tasks.
func (s *Shadow[T, R]) Close() {
	if s.closed.CompareAndSwap(false, true) {
		close(s.queue)
		s.wg.Wait()
		if s.engine != nil {
			s.engine.Close()
		}
	}
}
