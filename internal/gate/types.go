package gate

import (
	"context"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// Standard gate check identifiers.
const (
	CheckBuild        = "build"
	CheckFormat       = "format"
	CheckVet          = "vet"
	CheckStaticcheck  = "staticcheck"
	CheckGolangciLint = "golangci-lint"
	CheckRace         = "race"
	CheckGovulncheck  = "govulncheck"
	CheckReplay       = "replay"
	CheckErrorPaths   = "error_paths"
	CheckAPICompat    = "api_compat"
)

// AllChecks returns the standard quality gate checks in execution order.
func AllChecks() []string {
	return []string{
		CheckBuild,
		CheckFormat,
		CheckVet,
		CheckStaticcheck,
		CheckGolangciLint,
		CheckRace,
		CheckGovulncheck,
	}
}

// FullProfileChecks returns all checks required to satisfy the 'full' verification profile.
func FullProfileChecks() []string {
	return []string{
		CheckBuild,
		CheckFormat,
		CheckVet,
		CheckRace,
		CheckGovulncheck,
		CheckReplay,
		CheckErrorPaths,
		CheckAPICompat,
	}
}

// CheckStatus represents the outcome of an individual gate check.
type CheckStatus string

const (
	StatusPass    CheckStatus = "pass"
	StatusFail    CheckStatus = "fail"
	StatusSkipped CheckStatus = "skipped"
)

// CheckReport details the outcome and diagnostics of a single gate check.
type CheckReport struct {
	Name       string        `json:"name"`
	Passed     bool          `json:"passed"`
	Status     CheckStatus   `json:"status"`
	Duration   time.Duration `json:"duration"`
	DurationMS float64       `json:"duration_ms"`
	Output     string        `json:"output,omitempty"`
	Issues     []string      `json:"issues,omitempty"`
}

// GateReport aggregates the outcomes of all checks in a quality gate run.
type GateReport struct {
	Passed       bool          `json:"passed"`
	Duration     time.Duration `json:"duration"`
	DurationMS   float64       `json:"duration_ms"`
	Checks       []CheckReport `json:"checks"`
	PassedCount  int           `json:"passed_count"`
	FailedCount  int           `json:"failed_count"`
	SkippedCount int           `json:"skipped_count"`
}

// ToGateChecks converts check reports to adapterkit.GateCheck slice for state/evidence persistence.
func (gr *GateReport) ToGateChecks() []adapterkit.GateCheck {
	out := make([]adapterkit.GateCheck, len(gr.Checks))
	for i, c := range gr.Checks {
		var st adapterkit.ReportStatus
		switch c.Status {
		case StatusPass:
			st = adapterkit.StatusPass
		case StatusFail:
			st = adapterkit.StatusFail
		case StatusSkipped:
			st = adapterkit.StatusSkipped
		default:
			if c.Passed {
				st = adapterkit.StatusPass
			} else {
				st = adapterkit.StatusFail
			}
		}

		out[i] = adapterkit.GateCheck{
			Name:       c.Name,
			Status:     st,
			Output:     c.Output,
			DurationMS: c.DurationMS,
		}
	}
	return out
}

// CheckExecutor defines the function signature for executing a single gate check.
type CheckExecutor func(ctx context.Context, dir string, env []string) (output string, issues []string, passed bool, err error)

// Config configures the quality gate runner.
type Config struct {
	Dir               string                   // Directory to run checks against (defaults to current dir).
	Env               []string                 // Environment variables passed to toolchain processes.
	Timeout           time.Duration            // Per-check timeout (defaults to 60s).
	Checks            []string                 // Checks to run (defaults to AllChecks()).
	Executors         map[string]CheckExecutor // Optional custom or mock executors per check name.
	AllowMissingTools bool                     // If true, missing optional tools (like staticcheck) are marked skipped instead of failed.
}
