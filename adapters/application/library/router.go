package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// UnitMode determines how an individual library unit is routed during migration.
type UnitMode string

const (
	// ModeForeign routes invocations to the original foreign library wrapper.
	ModeForeign UnitMode = "old"

	// ModeGo routes invocations to the migrated Go implementation.
	ModeGo UnitMode = "new"

	// ModeShadow routes to the foreign library while asynchronously executing the Go implementation.
	ModeShadow UnitMode = "shadow"
)

// ForeignWrapper represents a foreign library execution target (Subprocess or HTTP).
type ForeignWrapper interface {
	Invoke(ctx context.Context, unitID string, input any) (any, error)
	InvokeRaw(ctx context.Context, unitID string, payload []byte) ([]byte, error)
	Close() error
}

type unitRoute struct {
	foreignWrapper ForeignWrapper
	goInvoker      Invoker
	mode           UnitMode
	fallbackCount  int64
}

// Router orchestrates strangler migration of foreign library units to Go one unit at a time.
type Router struct {
	mu             sync.RWMutex
	name           string
	defaultForeign ForeignWrapper
	routes         map[string]*unitRoute
}

// NewRouter creates a new foreign library migration router.
func NewRouter(name string) *Router {
	if name == "" {
		name = "router"
	}
	return &Router{
		name:   name,
		routes: make(map[string]*unitRoute),
	}
}

// SetDefaultForeign sets the fallback foreign wrapper when no unit-specific wrapper is registered.
func (r *Router) SetDefaultForeign(wrapper ForeignWrapper) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultForeign = wrapper
}

func (r *Router) getOrCreateRouteLocked(unitID string) *unitRoute {
	rt, exists := r.routes[unitID]
	if !exists {
		rt = &unitRoute{
			mode: ModeForeign,
		}
		r.routes[unitID] = rt
	}
	return rt
}

// RegisterForeign registers a foreign library wrapper for the specified unit.
func (r *Router) RegisterForeign(unitID string, wrapper ForeignWrapper) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt := r.getOrCreateRouteLocked(unitID)
	rt.foreignWrapper = wrapper
}

// RegisterGo registers a ported Go invoker for the specified unit.
func (r *Router) RegisterGo(unitID string, invoker Invoker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt := r.getOrCreateRouteLocked(unitID)
	rt.goInvoker = invoker
}

// SetUnitMode sets the routing mode for a unit.
func (r *Router) SetUnitMode(unitID string, mode UnitMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt := r.getOrCreateRouteLocked(unitID)
	rt.mode = mode
}

// MigrateUnit marks a unit as migrated to Go and registers its Go implementation.
func (r *Router) MigrateUnit(unitID string, goInvoker Invoker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt := r.getOrCreateRouteLocked(unitID)
	rt.goInvoker = goInvoker
	rt.mode = ModeGo
}

// GetEffectiveMode determines the active mode for a unit, checking environment variables first.
func (r *Router) GetEffectiveMode(unitID string) UnitMode {
	normalized := strings.ToUpper(unitID)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, "/", "_")
	normalized = strings.ReplaceAll(normalized, ".", "_")

	if env := os.Getenv("GO_ANYTHING_UNIT_" + normalized); env != "" {
		switch strings.ToLower(env) {
		case "original", "foreign", "old":
			return ModeForeign
		case "go", "new":
			return ModeGo
		case "shadow":
			return ModeShadow
		}
	}

	if env := os.Getenv("GO_ANYTHING_MODE"); env != "" {
		switch strings.ToLower(env) {
		case "original", "foreign", "old":
			return ModeForeign
		case "go", "new":
			return ModeGo
		case "shadow":
			return ModeShadow
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if rt, ok := r.routes[unitID]; ok {
		return rt.mode
	}
	return ModeForeign
}

// FallbackCount returns the total number of fallback invocations for a unit.
func (r *Router) FallbackCount(unitID string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if rt, ok := r.routes[unitID]; ok {
		return atomic.LoadInt64(&rt.fallbackCount)
	}
	return 0
}

// Invoke routes an invocation based on the unit's migration state.
func (r *Router) Invoke(ctx context.Context, unitID string, input any) (any, error) {
	r.mu.RLock()
	rt, exists := r.routes[unitID]
	defaultForeign := r.defaultForeign
	r.mu.RUnlock()

	mode := r.GetEffectiveMode(unitID)

	// Check if environment requested foreign fallback while in Go mode
	if exists && rt.mode == ModeGo && mode == ModeForeign {
		atomic.AddInt64(&rt.fallbackCount, 1)
	}

	switch mode {
	case ModeGo:
		if exists && rt.goInvoker != nil {
			return rt.goInvoker(ctx, input)
		}
		// If Go invoker is not registered, fallback to foreign
		if exists {
			atomic.AddInt64(&rt.fallbackCount, 1)
		}
		fallthrough

	case ModeShadow:
		foreignWrap := defaultForeign
		if exists && rt.foreignWrapper != nil {
			foreignWrap = rt.foreignWrapper
		}
		if foreignWrap == nil {
			return nil, fmt.Errorf("%w: unit %q has no foreign wrapper for shadow mode", ErrUnitNotFound, unitID)
		}

		res, err := foreignWrap.Invoke(ctx, unitID, input)

		// Async shadow execution of Go implementation if present
		if exists && rt.goInvoker != nil {
			go func() {
				_, _ = rt.goInvoker(context.Background(), input)
			}()
		}
		return res, err

	case ModeForeign:
		fallthrough
	default:
		foreignWrap := defaultForeign
		if exists && rt.foreignWrapper != nil {
			foreignWrap = rt.foreignWrapper
		}
		if foreignWrap == nil {
			return nil, fmt.Errorf("%w: unit %q has no foreign wrapper", ErrUnitNotFound, unitID)
		}
		return foreignWrap.Invoke(ctx, unitID, input)
	}
}

// DriverName returns the replay driver identifier.
func (r *Router) DriverName() string {
	return "library-router:" + r.name
}

// ExecuteCase replays a test case against the router.
func (r *Router) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if testCase == nil {
		return nil, errors.New("library: testCase cannot be nil")
	}

	out, err := r.Invoke(ctx, testCase.UnitID, testCase.Input.Body)
	if err != nil {
		var invocErr *InvocationError
		if errors.As(err, &invocErr) {
			return &adapterkit.CaseOutput{
				ExitCode:     invocErr.ExitCode,
				Stderr:       invocErr.Stderr,
				ErrorMessage: err.Error(),
			}, nil
		}
		return &adapterkit.CaseOutput{
			ExitCode:     1,
			ErrorMessage: err.Error(),
		}, nil
	}

	caseOut := &adapterkit.CaseOutput{
		ExitCode: 0,
		Body:     out,
	}

	// Also format stdout as JSON string if map or slice
	if out != nil {
		if b, err := json.Marshal(out); err == nil {
			caseOut.Stdout = string(b)
		}
	}

	return caseOut, nil
}

// Close closes all registered foreign wrappers.
func (r *Router) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	if r.defaultForeign != nil {
		if err := r.defaultForeign.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	for _, rt := range r.routes {
		if rt.foreignWrapper != nil {
			if err := rt.foreignWrapper.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
