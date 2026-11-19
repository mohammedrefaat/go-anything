package facade

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
)

// RouteConfig holds old and new handlers along with route-specific mode.
type RouteConfig struct {
	OldHandler   http.Handler
	NewHandler   http.Handler
	Mode         Mode
	DroppedCount atomic.Int64
	ShadowRuns   atomic.Int64
	Mismatches   atomic.Int64
}

// HTTPRegistry manages per-route handler switching across old, shadow, and new modes.
type HTTPRegistry struct {
	mu          sync.RWMutex
	routes      map[string]*RouteConfig
	defaultMode Mode
	shadowQueue chan httpShadowTask
	workers     int
	wg          sync.WaitGroup
	closed      atomic.Bool
	dropped     atomic.Int64
}

type httpShadowTask struct {
	req         *http.Request
	routeConfig *RouteConfig
	oldStatus   int
	oldBody     []byte
}

// NewHTTPRegistry creates an HTTP registry with bounded shadow workers.
func NewHTTPRegistry(workers int, queueCap int) *HTTPRegistry {
	if workers <= 0 {
		workers = 4
	}
	if queueCap <= 0 {
		queueCap = 100
	}

	reg := &HTTPRegistry{
		routes:      make(map[string]*RouteConfig),
		defaultMode: ModeOld,
		shadowQueue: make(chan httpShadowTask, queueCap),
		workers:     workers,
	}

	for i := 0; i < reg.workers; i++ {
		reg.wg.Add(1)
		go reg.workerLoop()
	}

	return reg
}

// RegisterRoute registers old and new handlers for a URL pattern with an optional mode.
func (r *HTTPRegistry) RegisterRoute(pattern string, oldH, newH http.Handler, mode ...Mode) {
	r.mu.Lock()
	defer r.mu.Unlock()

	m := r.defaultMode
	if len(mode) > 0 {
		m = mode[0]
	}

	r.routes[pattern] = &RouteConfig{
		OldHandler: oldH,
		NewHandler: newH,
		Mode:       m,
	}
}

// SetRouteMode dynamically changes the mode for a specific pattern.
func (r *HTTPRegistry) SetRouteMode(pattern string, m Mode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rc, exists := r.routes[pattern]; exists {
		rc.Mode = m
	}
}

// SetDefaultMode updates the default mode for unregistered routes.
func (r *HTTPRegistry) SetDefaultMode(m Mode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultMode = m
}

// ServeHTTP implements http.Handler, routing each request according to the route's mode.
func (r *HTTPRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	rc, exists := r.routes[req.URL.Path]
	r.mu.RUnlock()

	if !exists {
		http.NotFound(w, req)
		return
	}

	mode := rc.Mode
	switch mode {
	case ModeNew:
		rc.NewHandler.ServeHTTP(w, req)
	case ModeOld:
		rc.OldHandler.ServeHTTP(w, req)
	case ModeShadow:
		// Read and buffer body so both old and new handlers can consume it
		var bodyBytes []byte
		if req.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(req.Body)
			if err == nil {
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		// Capture old handler output via recorder
		rec := httptest.NewRecorder()
		rc.OldHandler.ServeHTTP(rec, req)

		// Send old response to client
		for k, vv := range rec.Header() {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())

		// Clone request for async shadow evaluation
		if !r.closed.Load() {
			clonedReq := req.Clone(context.Background())
			if bodyBytes != nil {
				clonedReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			task := httpShadowTask{
				req:         clonedReq,
				routeConfig: rc,
				oldStatus:   rec.Code,
				oldBody:     rec.Body.Bytes(),
			}

			select {
			case r.shadowQueue <- task:
			default:
				// Overflow: drop and record counter immediately!
				r.dropped.Add(1)
				rc.DroppedCount.Add(1)
			}
		}
	default:
		rc.OldHandler.ServeHTTP(w, req)
	}
}

func (r *HTTPRegistry) workerLoop() {
	defer r.wg.Done()
	for task := range r.shadowQueue {
		task.routeConfig.ShadowRuns.Add(1)
		shadowRec := httptest.NewRecorder()
		task.routeConfig.NewHandler.ServeHTTP(shadowRec, task.req)

		if shadowRec.Code != task.oldStatus || !bytes.Equal(shadowRec.Body.Bytes(), task.oldBody) {
			task.routeConfig.Mismatches.Add(1)
		}
	}
}

// DroppedCount returns total dropped shadow requests across all routes.
func (r *HTTPRegistry) DroppedCount() int64 {
	return r.dropped.Load()
}

// RouteStats returns shadow evaluation statistics for a route pattern.
func (r *HTTPRegistry) RouteStats(pattern string) (runs, dropped, mismatches int64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if rc, exists := r.routes[pattern]; exists {
		return rc.ShadowRuns.Load(), rc.DroppedCount.Load(), rc.Mismatches.Load()
	}
	return 0, 0, 0
}

// Close gracefully closes the shadow queue and waits for workers.
func (r *HTTPRegistry) Close() {
	if r.closed.CompareAndSwap(false, true) {
		close(r.shadowQueue)
		r.wg.Wait()
	}
}
