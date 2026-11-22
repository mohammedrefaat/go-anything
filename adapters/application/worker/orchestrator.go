package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

type unitWorkerRoute struct {
	legacyClient  SidecarClient
	goHandler     WorkerHandler
	mode          UnitMode
	fallbackCount int64
}

// Sidecar manages background worker and service units as Go sidecars,
// coordinating strangler migration one unit at a time.
type Sidecar struct {
	mu            sync.RWMutex
	name          string
	defaultLegacy SidecarClient
	routes        map[string]*unitWorkerRoute

	inFlight   sync.WaitGroup
	running    bool
	closed     bool
	shutdownCh chan struct{}
}

// NewSidecar creates a new Go worker sidecar orchestrator.
func NewSidecar(name string) *Sidecar {
	if name == "" {
		name = "worker-sidecar"
	}
	return &Sidecar{
		name:       name,
		routes:     make(map[string]*unitWorkerRoute),
		shutdownCh: make(chan struct{}),
	}
}

// SetDefaultLegacy sets the default legacy sidecar client for unconfigured units.
func (s *Sidecar) SetDefaultLegacy(client SidecarClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultLegacy = client
}

func (s *Sidecar) getOrCreateRouteLocked(unitID string) *unitWorkerRoute {
	rt, exists := s.routes[unitID]
	if !exists {
		rt = &unitWorkerRoute{
			mode: ModeLegacy,
		}
		s.routes[unitID] = rt
	}
	return rt
}

// RegisterLegacy registers a legacy worker sidecar client for a unit.
func (s *Sidecar) RegisterLegacy(unitID string, client SidecarClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.getOrCreateRouteLocked(unitID)
	rt.legacyClient = client
}

// RegisterGo registers a migrated Go handler for a unit.
func (s *Sidecar) RegisterGo(unitID string, handler WorkerHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.getOrCreateRouteLocked(unitID)
	rt.goHandler = handler
}

// SetUnitMode sets the routing mode for a unit.
func (s *Sidecar) SetUnitMode(unitID string, mode UnitMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.getOrCreateRouteLocked(unitID)
	rt.mode = mode
}

// MigrateUnit marks a worker unit as migrated to Go and registers its Go implementation.
func (s *Sidecar) MigrateUnit(unitID string, handler WorkerHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.getOrCreateRouteLocked(unitID)
	rt.goHandler = handler
	rt.mode = ModeGo
}

// GetEffectiveMode determines active mode for a unit, checking env overrides first.
func (s *Sidecar) GetEffectiveMode(unitID string) UnitMode {
	normalized := strings.ToUpper(unitID)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, "/", "_")
	normalized = strings.ReplaceAll(normalized, ".", "_")

	if env := os.Getenv("GO_ANYTHING_UNIT_" + normalized); env != "" {
		switch strings.ToLower(env) {
		case "original", "foreign", "legacy", "old":
			return ModeLegacy
		case "go", "modern", "new":
			return ModeGo
		case "shadow":
			return ModeShadow
		}
	}

	if env := os.Getenv("GO_ANYTHING_MODE"); env != "" {
		switch strings.ToLower(env) {
		case "original", "foreign", "legacy", "old":
			return ModeLegacy
		case "go", "modern", "new":
			return ModeGo
		case "shadow":
			return ModeShadow
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if rt, ok := s.routes[unitID]; ok {
		return rt.mode
	}
	return ModeLegacy
}

// FallbackCount returns the total number of fallback invocations for a unit.
func (s *Sidecar) FallbackCount(unitID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rt, ok := s.routes[unitID]; ok {
		return atomic.LoadInt64(&rt.fallbackCount)
	}
	return 0
}

// Start boots the sidecar service and marks it ready.
func (s *Sidecar) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSidecarClosed
	}
	s.running = true
	return nil
}

// Forward delivers a worker task to the appropriate destination (Go or legacy sidecar).
func (s *Sidecar) Forward(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, ErrSidecarClosed
	}
	rt, exists := s.routes[task.UnitID]
	defaultLegacy := s.defaultLegacy
	s.mu.RUnlock()

	s.inFlight.Add(1)
	defer s.inFlight.Done()

	mode := s.GetEffectiveMode(task.UnitID)

	// If unit is in Go mode but environment overrode to legacy, count fallback
	if exists && rt.mode == ModeGo && mode == ModeLegacy {
		atomic.AddInt64(&rt.fallbackCount, 1)
	}

	switch mode {
	case ModeGo:
		if exists && rt.goHandler != nil {
			start := time.Now()
			res, err := rt.goHandler(ctx, task)
			if err != nil {
				return &WorkerResult{
					TaskID:       task.ID,
					UnitID:       task.UnitID,
					Status:       "failed",
					ErrorMessage: err.Error(),
					DurationMS:   time.Since(start).Seconds() * 1000,
				}, nil
			}
			if res != nil && res.DurationMS == 0 {
				res.DurationMS = time.Since(start).Seconds() * 1000
			}
			return res, nil
		}
		// If Go handler not set, fallback to legacy
		if exists {
			atomic.AddInt64(&rt.fallbackCount, 1)
		}
		fallthrough

	case ModeShadow:
		legacy := defaultLegacy
		if exists && rt.legacyClient != nil {
			legacy = rt.legacyClient
		}
		if legacy == nil {
			return nil, fmt.Errorf("%w: unit %q has no legacy sidecar for shadow mode", ErrUnitNotConfigured, task.UnitID)
		}

		res, err := legacy.Forward(ctx, task)

		// Async shadow execution of Go worker
		if exists && rt.goHandler != nil {
			s.inFlight.Add(1)
			go func() {
				defer s.inFlight.Done()
				_, _ = rt.goHandler(context.Background(), task)
			}()
		}
		return res, err

	case ModeLegacy:
		fallthrough
	default:
		legacy := defaultLegacy
		if exists && rt.legacyClient != nil {
			legacy = rt.legacyClient
		}
		if legacy == nil {
			return nil, fmt.Errorf("%w: unit %q has no legacy sidecar", ErrUnitNotConfigured, task.UnitID)
		}
		return legacy.Forward(ctx, task)
	}
}

// HealthCheck executes health checks across all registered sidecars.
func (s *Sidecar) HealthCheck(ctx context.Context) map[string]*HealthReport {
	s.mu.RLock()
	defer s.mu.RUnlock()

	reports := make(map[string]*HealthReport)

	if s.defaultLegacy != nil {
		rep, err := s.defaultLegacy.HealthCheck(ctx)
		if rep == nil {
			rep = &HealthReport{
				Status:    HealthStateUnhealthy,
				Transport: s.defaultLegacy.Transport(),
				Message:   fmt.Sprint(err),
			}
		}
		reports["default"] = rep
	}

	for unitID, rt := range s.routes {
		if rt.legacyClient != nil {
			rep, err := rt.legacyClient.HealthCheck(ctx)
			if rep == nil {
				rep = &HealthReport{
					Status:    HealthStateUnhealthy,
					Transport: rt.legacyClient.Transport(),
					Message:   fmt.Sprint(err),
				}
			}
			reports[unitID] = rep
		} else if rt.goHandler != nil {
			reports[unitID] = &HealthReport{
				Status:    HealthStateHealthy,
				Transport: "go_native",
				Message:   "Go native worker handler ready",
			}
		}
	}

	return reports
}

// Shutdown gracefully shuts down the sidecar, waiting for in-flight tasks to complete.
func (s *Sidecar) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.running = false
	close(s.shutdownCh)
	s.mu.Unlock()

	// Wait for in-flight jobs to drain or context timeout
	done := make(chan struct{})
	go func() {
		s.inFlight.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		// Context expired while draining
	}

	return s.closeClients()
}

func (s *Sidecar) closeClients() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	if s.defaultLegacy != nil {
		if err := s.defaultLegacy.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, rt := range s.routes {
		if rt.legacyClient != nil {
			if err := rt.legacyClient.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Close closes the sidecar immediately without waiting for in-flight jobs.
func (s *Sidecar) Close() error {
	return s.Shutdown(context.Background())
}

// DriverName returns the replay driver identifier.
func (s *Sidecar) DriverName() string {
	return "worker-sidecar:" + s.name
}

// ExecuteCase replays a test case against the worker sidecar.
func (s *Sidecar) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if testCase == nil {
		return nil, errors.New("worker: testCase cannot be nil")
	}

	task := WorkerTask{
		ID:        testCase.CaseID,
		UnitID:    testCase.UnitID,
		Payload:   testCase.Input.Body,
		Headers:   testCase.Input.Headers,
		CreatedAt: time.Now(),
	}

	res, err := s.Forward(ctx, task)
	if err != nil {
		return &adapterkit.CaseOutput{
			ExitCode:     1,
			ErrorMessage: err.Error(),
		}, nil
	}

	caseOut := &adapterkit.CaseOutput{
		Body: res.Output,
	}

	if res.Status == "failed" {
		caseOut.ExitCode = 1
		caseOut.ErrorMessage = res.ErrorMessage
	} else {
		caseOut.ExitCode = 0
	}

	if res.Output != nil {
		if b, err := json.Marshal(res.Output); err == nil {
			caseOut.Stdout = string(b)
		}
	}

	return caseOut, nil
}
