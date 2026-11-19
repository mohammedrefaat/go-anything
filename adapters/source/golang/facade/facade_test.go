package facade

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestPayload struct {
	Value int
	Data  []string
}

func clonePayload(p TestPayload) TestPayload {
	cp := TestPayload{
		Value: p.Value,
		Data:  make([]string, len(p.Data)),
	}
	copy(cp.Data, p.Data)
	return cp
}

func TestShadow_Modes_OldShadowNew(t *testing.T) {
	var oldCalls, newCalls atomic.Int64

	oldFn := func(_ context.Context, p TestPayload) (int, error) {
		oldCalls.Add(1)
		return p.Value * 10, nil
	}

	newFn := func(_ context.Context, p TestPayload) (int, error) {
		newCalls.Add(1)
		return p.Value * 10, nil
	}

	shadow := NewShadow(oldFn, newFn, clonePayload,
		WithWorkerPoolSize[TestPayload, int](2),
		WithQueueCapacity[TestPayload, int](10),
	)
	defer shadow.Close()

	ctx := context.Background()

	// 1. ModeOld
	shadow.SetMode(ModeOld)
	res, err := shadow.Execute(ctx, TestPayload{Value: 5})
	require.NoError(t, err)
	assert.Equal(t, 50, res)
	assert.Equal(t, int64(1), oldCalls.Load())
	assert.Equal(t, int64(0), newCalls.Load())

	// 2. ModeNew
	shadow.SetMode(ModeNew)
	res, err = shadow.Execute(ctx, TestPayload{Value: 7})
	require.NoError(t, err)
	assert.Equal(t, 70, res)
	assert.Equal(t, int64(1), oldCalls.Load())
	assert.Equal(t, int64(1), newCalls.Load())

	// 3. ModeShadow
	shadow.SetMode(ModeShadow)
	res, err = shadow.Execute(ctx, TestPayload{Value: 3})
	require.NoError(t, err)
	assert.Equal(t, 30, res) // Primary returned from oldFn
	assert.Equal(t, int64(2), oldCalls.Load())

	// Wait for async shadow worker to execute
	assert.Eventually(t, func() bool {
		return newCalls.Load() == 2 && shadow.ShadowRuns() == 1
	}, 1*time.Second, 10*time.Millisecond)

	assert.Equal(t, int64(3), shadow.TotalCalls())
	assert.Equal(t, int64(0), shadow.DroppedCount())
}

func TestShadow_BoundedQueue_DropsOverflow(t *testing.T) {
	// Worker will sleep on shadow execution so queue quickly fills up
	workerBlocker := make(chan struct{})

	oldFn := func(_ context.Context, x int) (int, error) {
		return x * 2, nil
	}

	newFn := func(_ context.Context, x int) (int, error) {
		<-workerBlocker
		return x * 2, nil
	}

	// 1 worker, queue capacity of 2
	shadow := NewShadow(oldFn, newFn, nil,
		WithWorkerPoolSize[int, int](1),
		WithQueueCapacity[int, int](2),
		WithMode[int, int](ModeShadow),
	)

	ctx := context.Background()

	// Send 50 items while shadow worker is blocked
	for i := 0; i < 50; i++ {
		res, err := shadow.Execute(ctx, i)
		require.NoError(t, err)
		assert.Equal(t, i*2, res) // Primary calls never block or fail!
	}

	// Because queue capacity is 2 and worker is blocked on 1 task:
	// Exactly 1 in worker + 2 in queue = 3 accepted, 47 dropped!
	assert.Greater(t, shadow.DroppedCount(), int64(40), "overflow must be dropped and counted")

	// Release worker
	close(workerBlocker)
	shadow.Close()
}

func TestShadow_MismatchDetection(t *testing.T) {
	var mismatchDetected atomic.Bool

	oldFn := func(_ context.Context, s string) (string, error) {
		return "legacy-" + s, nil
	}

	newFn := func(_ context.Context, s string) (string, error) {
		return "modern-" + s, nil // Intentional mismatch
	}

	shadow := NewShadow(oldFn, newFn, nil,
		WithWorkerPoolSize[string, string](2),
		WithOnMismatch[string, string](func(in, oldRes, newRes string, oldErr, newErr error) {
			mismatchDetected.Store(true)
		}),
	)
	defer shadow.Close()

	res, err := shadow.Execute(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "legacy-test", res)

	assert.Eventually(t, func() bool {
		return mismatchDetected.Load() && shadow.MismatchCount() == 1
	}, 1*time.Second, 10*time.Millisecond)
}

func TestHTTPRegistry_RoutesAndModes(t *testing.T) {
	reg := NewHTTPRegistry(2, 20)
	defer reg.Close()

	oldHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Backend", "old")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("old: " + string(body)))
	})

	newHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Backend", "new")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("new: " + string(body)))
	})

	reg.RegisterRoute("/api/test", oldHandler, newHandler, ModeOld)

	// 1. ModeOld
	req := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewBufferString("hello"))
	rec := httptest.NewRecorder()
	reg.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "old", rec.Header().Get("X-Backend"))
	assert.Equal(t, "old: hello", rec.Body.String())

	// 2. ModeNew
	reg.SetRouteMode("/api/test", ModeNew)
	req2 := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewBufferString("world"))
	rec2 := httptest.NewRecorder()
	reg.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "new", rec2.Header().Get("X-Backend"))
	assert.Equal(t, "new: world", rec2.Body.String())

	// 3. ModeShadow
	reg.SetRouteMode("/api/test", ModeShadow)
	req3 := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewBufferString("shadow-payload"))
	rec3 := httptest.NewRecorder()
	reg.ServeHTTP(rec3, req3)

	// Primary response is from oldHandler
	assert.Equal(t, http.StatusOK, rec3.Code)
	assert.Equal(t, "old", rec3.Header().Get("X-Backend"))
	assert.Equal(t, "old: shadow-payload", rec3.Body.String())

	// Wait for shadow evaluation
	assert.Eventually(t, func() bool {
		runs, _, _ := reg.RouteStats("/api/test")
		return runs >= 1
	}, 1*time.Second, 10*time.Millisecond)

	// Route that does not exist returns 404
	req404 := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec404 := httptest.NewRecorder()
	reg.ServeHTTP(rec404, req404)
	assert.Equal(t, http.StatusNotFound, rec404.Code)
}

func TestHTTPRegistry_DropsOverflow(t *testing.T) {
	blocker := make(chan struct{})

	oldH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	newH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocker
		w.WriteHeader(http.StatusOK)
	})

	// Registry with 1 worker and 2 queue capacity
	reg := NewHTTPRegistry(1, 2)
	reg.RegisterRoute("/flood", oldH, newH, ModeShadow)

	// Flood with 50 requests
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodGet, "/flood", nil)
		rec := httptest.NewRecorder()
		reg.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	assert.Greater(t, reg.DroppedCount(), int64(40), "overflow must be dropped and counted")

	close(blocker)
	reg.Close()
}

func TestGenerateFacadeSource(t *testing.T) {
	funcs := []FunctionSpec{
		{
			Name:       "Add",
			Params:     "x, y int",
			ParamNames: "x, y",
			Returns:    "int",
		},
		{
			Name:       "Greet",
			Params:     "name string",
			ParamNames: "name",
			Returns:    "string",
		},
	}

	source := GenerateFacadeSource("calc", "pkg/calc", "example.com/app/legacy/pkg/calc", "example.com/app/modern/pkg/calc", funcs)
	assert.Contains(t, source, "package calc")
	assert.Contains(t, source, `legacy "example.com/app/legacy/pkg/calc"`)
	assert.Contains(t, source, `modern "example.com/app/modern/pkg/calc"`)
	assert.Contains(t, source, `func Add(x, y int) int`)
	assert.Contains(t, source, `return modern.Add(x, y)`)
	assert.Contains(t, source, `return legacy.Add(x, y)`)
	assert.Contains(t, source, `func Greet(name string) string`)
}

func TestFacade_HighConcurrency(t *testing.T) {
	var count atomic.Int64
	oldFn := func(_ context.Context, v int) (int, error) {
		count.Add(1)
		return v, nil
	}
	newFn := func(_ context.Context, v int) (int, error) {
		return v, nil
	}

	shadow := NewShadow(oldFn, newFn, nil,
		WithWorkerPoolSize[int, int](8),
		WithQueueCapacity[int, int](50),
		WithMode[int, int](ModeShadow),
	)
	defer shadow.Close()

	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, err := shadow.Execute(context.Background(), gID*100+i)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		}(g)
	}
	wg.Wait()

	assert.Equal(t, int64(1000), count.Load())
	assert.Equal(t, int64(1000), shadow.TotalCalls())
}
