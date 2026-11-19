// Package shadow provides live traffic shadowing, side-effect safety guards,
// and mismatch logging for ported units.
package shadow

// Engine coordinates shadow traffic mirroring.
type Engine struct{}

// New returns a new Engine instance.
func New() *Engine {
	return &Engine{}
}
