package conformance

import (
	"fmt"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CapabilityGuard enforces declared capability boundaries on adapter operations.
// The Core refuses any action an adapter did not declare, returning
// an error wrapping adapterkit.ErrUndeclaredCapability.
type CapabilityGuard struct {
	declared map[adapterkit.Capability]struct{}
}

// NewCapabilityGuard constructs a guard initialized with the adapter's declared capabilities.
func NewCapabilityGuard(caps []adapterkit.Capability) *CapabilityGuard {
	m := make(map[adapterkit.Capability]struct{}, len(caps))
	for _, c := range caps {
		m[c] = struct{}{}
	}
	return &CapabilityGuard{declared: m}
}

// Has checks whether a specific capability was declared.
func (g *CapabilityGuard) Has(c adapterkit.Capability) bool {
	_, ok := g.declared[c]
	return ok
}

// CheckDeclared verifies that the required capability is declared.
// If undeclared, it returns an error wrapping adapterkit.ErrUndeclaredCapability.
func (g *CapabilityGuard) CheckDeclared(required adapterkit.Capability) error {
	if !g.Has(required) {
		return fmt.Errorf("%w: required capability %q is not declared", adapterkit.ErrUndeclaredCapability, required)
	}
	return nil
}

// Execute enforces that the required capability is declared before invoking action.
// If the required capability is not declared, the action is never invoked and
// an error wrapping adapterkit.ErrUndeclaredCapability is returned.
func (g *CapabilityGuard) Execute(required adapterkit.Capability, action func() error) error {
	if err := g.CheckDeclared(required); err != nil {
		return err
	}
	if action != nil {
		return action()
	}
	return nil
}
