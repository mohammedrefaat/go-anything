package cutover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mohammedrefat/go-anything/internal/frontdoor"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CutoverPort abstracts unit inspection, kill switch management, and fallback counts.
// Rule R-1: CutoverPort NEVER performs or exposes lift!
type CutoverPort interface {
	GetUnit(ctx context.Context, dir, unitID string) (*adapterkit.Unit, error)
	IsKillSwitchActive(ctx context.Context, dir, unitID string) bool
	ActivateKillSwitch(ctx context.Context, dir, unitID string) error
	GetFallbackCount(ctx context.Context, dir, unitID string) (int64, error)
}

// DefaultCutoverPort implements CutoverPort using internal/frontdoor and internal/state.
type DefaultCutoverPort struct{}

// NewDefaultCutoverPort creates a new DefaultCutoverPort.
func NewDefaultCutoverPort() *DefaultCutoverPort {
	return &DefaultCutoverPort{}
}

// GetUnit loads unit state from the workspace store.
func (p *DefaultCutoverPort) GetUnit(ctx context.Context, dir, unitID string) (*adapterkit.Unit, error) {
	store := state.NewStore(dir)
	return store.GetUnit(unitID)
}

// IsKillSwitchActive checks if the unit or global kill switch is active.
func (p *DefaultCutoverPort) IsKillSwitchActive(ctx context.Context, dir, unitID string) bool {
	return frontdoor.IsUnitKillSwitchActive(dir, unitID)
}

// ActivateKillSwitch creates the emergency .go-anything/local/<unit>.kill file.
func (p *DefaultCutoverPort) ActivateKillSwitch(ctx context.Context, dir, unitID string) error {
	killFile := filepath.Join(dir, ".go-anything", "local", unitID+".kill")
	if err := os.MkdirAll(filepath.Dir(killFile), 0755); err != nil {
		return fmt.Errorf("failed to create local dir: %w", err)
	}
	if err := os.WriteFile(killFile, []byte("activated\n"), 0644); err != nil {
		return fmt.Errorf("failed to create kill switch file %s: %w", killFile, err)
	}
	return nil
}

// GetFallbackCount returns the count of automatic fallbacks to legacy original.
func (p *DefaultCutoverPort) GetFallbackCount(ctx context.Context, dir, unitID string) (int64, error) {
	return frontdoor.GetFallbackCount(dir, unitID)
}
