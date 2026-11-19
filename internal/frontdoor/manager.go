package frontdoor

import (
	"net/http"
	"sync"
)

// Manager coordinates kill switch checks, fallback execution, and metric persistence
// for both CLI and HTTP front doors.
type Manager struct {
	workspaceRoot string
	mu            sync.RWMutex
	memoryCounts  map[string]int64
}

// NewManager initializes a front door Manager for the given workspace root.
func NewManager(workspaceRoot string) *Manager {
	return &Manager{
		workspaceRoot: workspaceRoot,
		memoryCounts:  make(map[string]int64),
	}
}

// WorkspaceRoot returns the manager's configured workspace root directory.
func (m *Manager) WorkspaceRoot() string {
	return m.workspaceRoot
}

// IsKillSwitchActive returns true if the kill switch is triggered for unitID or globally.
func (m *Manager) IsKillSwitchActive(unitID string) bool {
	return IsUnitKillSwitchActive(m.workspaceRoot, unitID)
}

// RecordFallback increments and persists the fallback count for unitID.
func (m *Manager) RecordFallback(unitID string) (int64, error) {
	if unitID == "" {
		return 0, nil
	}
	cnt, err := IncrementFallback(m.workspaceRoot, unitID)
	if err == nil {
		m.mu.Lock()
		m.memoryCounts[unitID] = cnt
		m.mu.Unlock()
	}
	return cnt, err
}

// GetFallbackCount returns the fallback count for unitID.
func (m *Manager) GetFallbackCount(unitID string) int64 {
	cnt, err := GetFallbackCount(m.workspaceRoot, unitID)
	if err == nil {
		return cnt
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memoryCounts[unitID]
}

// GetAllFallbackCounts returns all known fallback counts.
func (m *Manager) GetAllFallbackCounts() map[string]int64 {
	counts, err := GetAllFallbackCounts(m.workspaceRoot)
	if err == nil && len(counts) > 0 {
		return counts
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]int64, len(m.memoryCounts))
	for k, v := range m.memoryCounts {
		res[k] = v
	}
	return res
}

// ResetFallback resets the fallback count for unitID.
func (m *Manager) ResetFallback(unitID string) error {
	m.mu.Lock()
	delete(m.memoryCounts, unitID)
	m.mu.Unlock()
	return ResetFallbackCount(m.workspaceRoot, unitID)
}

// CLIRouteOverrideHook returns a RouteOverrideFunc suitable for adapters/application/cli/frontdoor.WithRouteOverride.
// If unitID is non-empty, it checks unitID; otherwise it checks cmd.
func (m *Manager) CLIRouteOverrideHook() func(cmd, unitID string) bool {
	return func(cmd, unitID string) bool {
		target := unitID
		if target == "" {
			target = cmd
		}
		return m.IsKillSwitchActive(target)
	}
}

// CLIFallbackHook returns a FallbackHookFunc suitable for adapters/application/cli/frontdoor.WithFallbackHook.
// It records fallback counts whenever a command falls back to the original binary.
func (m *Manager) CLIFallbackHook() func(cmd, unitID string, argv []string) {
	return func(cmd, unitID string, argv []string) {
		target := unitID
		if target == "" {
			target = cmd
		}
		if target != "" {
			_, _ = m.RecordFallback(target)
		}
	}
}

// WrapHTTPHandler wraps an HTTP handler with kill-switch evaluation and fallback routing.
// If the kill switch is active for unitID, fallbackHandler is invoked and a fallback is recorded.
// If fallbackHandler is nil and kill switch is active, a 503 Service Unavailable response is returned.
func (m *Manager) WrapHTTPHandler(unitID string, portedHandler http.Handler, fallbackHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.IsKillSwitchActive(unitID) {
			_, _ = m.RecordFallback(unitID)
			if fallbackHandler != nil {
				fallbackHandler.ServeHTTP(w, r)
			} else {
				http.Error(w, "Service Unavailable: unit disabled by kill switch", http.StatusServiceUnavailable)
			}
			return
		}
		if portedHandler != nil {
			portedHandler.ServeHTTP(w, r)
		}
	})
}

// WrapHTTPHandlerFunc wraps an HTTP handler function with kill-switch evaluation and fallback routing.
func (m *Manager) WrapHTTPHandlerFunc(unitID string, portedFn http.HandlerFunc, fallbackFn http.HandlerFunc) http.HandlerFunc {
	var fallbackHandler http.Handler
	if fallbackFn != nil {
		fallbackHandler = fallbackFn
	}
	var portedHandler http.Handler
	if portedFn != nil {
		portedHandler = portedFn
	}
	wrapped := m.WrapHTTPHandler(unitID, portedHandler, fallbackHandler)
	return wrapped.ServeHTTP
}
