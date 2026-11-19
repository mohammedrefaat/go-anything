package lift

import (
	"context"
	"errors"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

var (
	// ErrLiftBlocked is returned when lift cannot proceed due to pre-flight or verification blockers.
	ErrLiftBlocked = errors.New("lift operation blocked")
)

// ReplayRunnerFunc defines a pluggable verification profile runner executed inside the isolated copy.
type ReplayRunnerFunc func(ctx context.Context, copyDir string, units []*adapterkit.Unit) error

// BuildRunnerFunc defines a pluggable compiler/builder runner executed inside the isolated copy.
type BuildRunnerFunc func(ctx context.Context, copyDir string) (string, error)

// Options configures the Lift engine.
type Options struct {
	WorkspaceRoot string                     // Root path of the repository being lifted.
	CommitSHA     string                     // Target commit SHA for evidence matching (auto-detected if empty).
	TargetUnits   []string                   // Optional slice of unit IDs to lift (lifts all units in manifest if empty).
	Profile       adapterkit.EvidenceProfile // Required evidence profile (defaults to ProfileReplay).
	Actor         string                     // Operator/identity performing the lift.
	BridgePaths   []string                   // Relative paths or files of front door bridges to remove.
	OriginalPaths []string                   // Relative paths or files of original application to remove.
	SkipBuild     bool                       // If true, skips the compilation check (useful for non-Go or testing).
	BuildCommand  []string                   // Custom build command (defaults to ["go", "build", "./..."]).
	ReplayRunner  ReplayRunnerFunc           // Pluggable verification runner for isolated copy.
	BuildRunner   BuildRunnerFunc            // Pluggable build runner for isolated copy.
}

// Result summarizes the outcome of a lift operation.
type Result struct {
	Success        bool     `json:"success"`
	Blockers       []string `json:"blockers,omitempty"`
	CompletedUnits []string `json:"completed_units,omitempty"`
	RemovedFiles   []string `json:"removed_files,omitempty"`
	CopyDirectory  string   `json:"copy_directory,omitempty"`
}
