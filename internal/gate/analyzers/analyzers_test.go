package analyzers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefat/go-anything/internal/gate/analyzers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerPanicAnalyzer_FlagsUnrecoveredPanic(t *testing.T) {
	dir := t.TempDir()

	src := `package testpkg

import "net/http"

func UnsafeHandler(w http.ResponseWriter, r *http.Request) {
	panic("database exploded")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unsafe.go"), []byte(src), 0644))

	diagnostics, err := analyzers.CheckPackage(context.Background(), dir, analyzers.HandlerPanicAnalyzer)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "handlerpanic", diagnostics[0].Analyzer)
	assert.Contains(t, diagnostics[0].Message, "direct call to panic() inside HTTP handler without recover()")
}

func TestHandlerPanicAnalyzer_AllowsRecoveredHandler(t *testing.T) {
	dir := t.TempDir()

	src := `package testpkg

import "net/http"

func SafeHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			http.Error(w, "internal server error", 500)
		}
	}()

	panic("something caught")
}

func RegularNonHandlerFunction() {
	panic("regular panic outside handler is allowed")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "safe.go"), []byte(src), 0644))

	diagnostics, err := analyzers.CheckPackage(context.Background(), dir, analyzers.HandlerPanicAnalyzer)
	require.NoError(t, err)
	assert.Empty(t, diagnostics, "handlers with recover() or non-handlers must not be flagged")
}

func TestGoroutineContextAnalyzer_FlagsUncoordinatedGoroutine(t *testing.T) {
	dir := t.TempDir()

	src := `package testpkg

func LaunchLeakyBackground() {
	go func() {
		println("leaky worker")
	}()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leaky.go"), []byte(src), 0644))

	diagnostics, err := analyzers.CheckPackage(context.Background(), dir, analyzers.GoroutineContextAnalyzer)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "goroutinecontext", diagnostics[0].Analyzer)
	assert.Contains(t, diagnostics[0].Message, "goroutine launched without context.Context or coordination tracker")
}

func TestGoroutineContextAnalyzer_AllowsCoordinatedGoroutines(t *testing.T) {
	dir := t.TempDir()

	src := `package testpkg

import (
	"context"
	"sync"
)

func LaunchWithContextParam(ctx context.Context) {
	go func(ctx context.Context) {
		select {
		case <-ctx.Done():
			return
		}
	}(ctx)
}

func LaunchWithWaitGroup(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		println("coordinated with wg")
	}()
}

func LaunchWorker(ctx context.Context) {
	go runWorker(ctx)
}

func runWorker(ctx context.Context) {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coordinated.go"), []byte(src), 0644))

	diagnostics, err := analyzers.CheckPackage(context.Background(), dir, analyzers.GoroutineContextAnalyzer)
	require.NoError(t, err)
	assert.Empty(t, diagnostics, "goroutines with context or waitgroup must not be flagged")
}

func TestAllAnalyzers_CleanPackage(t *testing.T) {
	dir := t.TempDir()

	src := `package testpkg

import (
	"context"
	"net/http"
	"sync"
)

type Server struct {
	wg sync.WaitGroup
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		_ = recover()
	}()
	s.wg.Add(1)
	go func(ctx context.Context) {
		defer s.wg.Done()
		println("running")
	}(r.Context())
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.go"), []byte(src), 0644))

	diagnostics, err := analyzers.CheckPackage(context.Background(), dir, analyzers.All()...)
	require.NoError(t, err)
	assert.Empty(t, diagnostics)
}
