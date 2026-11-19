// Package state manages workspace state persistence, unit lifecycle state
// machines, atomic I/O under file locks, and evidence profiles.
package state

// New creates a new Store pointing to the given workspace root.
func New(workspaceRoot string) *Store {
	return NewStore(workspaceRoot)
}
