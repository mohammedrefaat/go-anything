package conformance

import (
	"context"
	"fmt"
	"strings"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CheckName identifies a specific category of conformance verification.
type CheckName string

const (
	CheckMetadata               CheckName = "metadata"
	CheckDetectionAndEnv        CheckName = "detection_and_environment"
	CheckCapabilities           CheckName = "capabilities"
	CheckFieldSemantics         CheckName = "field_semantics"
	CheckAnalysisAndDiscovery   CheckName = "analysis_and_unit_discovery"
	CheckCaseSchema             CheckName = "case_recording_schema"
	CheckReplayAcrossTransports CheckName = "replay_across_transports"
	CheckCoverageLimits         CheckName = "coverage_limits"
)

// Detector is an optional interface that language adapters may implement
// to explicitly check whether a repository matches their source language.
type Detector interface {
	Detect(ctx context.Context, repoPath string) (bool, error)
}

// EnvironmentChecker is an optional interface that language adapters may implement
// to verify their host toolchains, compilers, and required dependencies.
type EnvironmentChecker interface {
	CheckEnvironment(ctx context.Context) error
}

// CaseRecorder is an optional interface for language adapters capable of recording
// or generating behavioral test cases.
type CaseRecorder interface {
	RecordCases(ctx context.Context, unitID string, inputs []adapterkit.CaseInput) ([]*adapterkit.Case, error)
}

// CoverageLimits defines the coverage thresholds and constraints declared by an adapter.
type CoverageLimits struct {
	// MinStatementCoverage specifies the minimum statement/branch coverage required (0.0 to 1.0).
	MinStatementCoverage float64 `json:"min_statement_coverage,omitempty"`
	// MaxUnreplayableRatio specifies the maximum allowable ratio of unreplayable fields (0.0 to 1.0).
	MaxUnreplayableRatio float64 `json:"max_unreplayable_ratio,omitempty"`
	// SupportedCaseKinds specifies the subset of CaseKinds supported by the adapter.
	SupportedCaseKinds []adapterkit.CaseKind `json:"supported_case_kinds,omitempty"`
}

// CoverageLimiter is an optional interface for language adapters that declare
// explicit coverage constraints.
type CoverageLimiter interface {
	CoverageLimits() CoverageLimits
}

// MultiTransportReplayProvider is an optional interface for adapters or test fixtures
// that supply Replay drivers across multiple transports to verify cross-transport consistency.
type MultiTransportReplayProvider interface {
	CreateReplayDrivers(ctx context.Context, unitID string) (map[adapterkit.TransportType]adapterkit.Replay, error)
}

// Violation represents an individual conformance failure with descriptive context.
type Violation struct {
	Check       CheckName `json:"check"`
	Description string    `json:"description"`
}

func (v Violation) String() string {
	return fmt.Sprintf("[%s] %s", v.Check, v.Description)
}

// Report aggregates results from all conformance checks.
type Report struct {
	Passed     bool        `json:"passed"`
	Violations []Violation `json:"violations,omitempty"`
}

// AddViolation appends a new descriptive violation to the report.
func (r *Report) AddViolation(check CheckName, format string, args ...any) {
	r.Passed = false
	r.Violations = append(r.Violations, Violation{
		Check:       check,
		Description: fmt.Sprintf(format, args...),
	})
}

// HasCheckViolation returns true if there is at least one violation for the specified check.
func (r *Report) HasCheckViolation(check CheckName) bool {
	for _, v := range r.Violations {
		if v.Check == check {
			return true
		}
	}
	return false
}

// Summary returns a human-readable summary of the conformance report.
func (r *Report) Summary() string {
	if r.Passed {
		return "PASS: all conformance checks passed successfully"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("FAIL: %d conformance violation(s) detected:\n", len(r.Violations)))
	for i, v := range r.Violations {
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, v.Check, v.Description))
	}
	return sb.String()
}

// Config defines execution parameters and options for the conformance test suite.
type Config struct {
	// RepoPath specifies the path to a valid repository fixture for the adapter.
	// If empty, the suite will generate a minimal repository fixture if possible.
	RepoPath string

	// InvalidRepoPath specifies an invalid or non-matching repository path to test detection rejection.
	// If empty, a non-existent path is used.
	InvalidRepoPath string

	// TargetUnitID specifies the unit to exercise during replay checks.
	TargetUnitID string

	// SampleCases provides cases for schema validation and cross-transport replay checks.
	SampleCases []*adapterkit.Case

	// StrictReplay requires CreateReplayDriver to return a non-nil driver.
	StrictReplay bool

	// DeclaredLimits specifies declared coverage limits if the adapter does not implement CoverageLimiter.
	DeclaredLimits *CoverageLimits

	// ProbeAction allows an adapter or test fixture to attempt an action for capability enforcement checks.
	ProbeAction func(action string) error
}
