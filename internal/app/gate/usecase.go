package gate

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/gate"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// UseCase coordinates quality gate check execution, scorecard generation, and evidence storage.
type UseCase struct {
	port GatePort
}

// New creates a new gate UseCase.
func New(port GatePort) *UseCase {
	if port == nil {
		port = NewDefaultGatePort()
	}
	return &UseCase{port: port}
}

// Run executes the quality gate pipeline for the given unit.
func (uc *UseCase) Run(ctx context.Context, in Input) (*Output, error) {
	if in.UnitID == "" {
		return nil, fmt.Errorf("unit ID is required")
	}

	if defPort, ok := uc.port.(*DefaultGatePort); ok && defPort.UnitID == "" {
		defPort.UnitID = in.UnitID
	}

	dir := in.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}

	profile := in.Profile
	if profile == "" {
		profile = string(adapterkit.ProfileFull)
	}

	var checks []string
	switch adapterkit.EvidenceProfile(profile) {
	case adapterkit.ProfileReplay:
		checks = []string{gate.CheckReplay}
	case adapterkit.ProfileFull:
		checks = gate.FullProfileChecks()
	default:
		return nil, fmt.Errorf("unsupported verification profile: %s", profile)
	}

	cfg := gate.Config{
		Dir:               absDir,
		Checks:            checks,
		AllowMissingTools: in.AllowMissingTools,
	}

	start := time.Now()
	report, err := uc.port.RunGate(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("gate execution failed: %w", err)
	}
	durationMS := float64(time.Since(start).Milliseconds())

	var checkResults []CheckResult
	for _, c := range report.Checks {
		statusStr := "pass"
		switch c.Status {
		case gate.StatusPass:
			statusStr = "pass"
		case gate.StatusFail:
			statusStr = "fail"
		case gate.StatusSkipped:
			statusStr = "skipped"
		}

		issues := c.Issues
		if issues == nil {
			issues = []string{}
		}

		checkResults = append(checkResults, CheckResult{
			Name:       c.Name,
			Status:     statusStr,
			DurationMS: c.Duration.Seconds() * 1000,
			Issues:     issues,
			Output:     c.Output,
		})
	}

	// Rule 2: Strictly sort check results by check name
	sort.Slice(checkResults, func(i, j int) bool {
		return checkResults[i].Name < checkResults[j].Name
	})

	var scorecardPath string
	if in.Scorecard {
		scPath, err := uc.port.GenerateScorecard(ctx, absDir, in.UnitID, report)
		if err == nil {
			scorecardPath = scPath
		}
	}

	commitSHA, _ := uc.port.GetCurrentCommitSHA(ctx, absDir)
	if commitSHA == "" {
		commitSHA = "0000000000000000000000000000000000000000"
	}

	evidenceSaved := false
	if report.Passed {
		ev := &adapterkit.Evidence{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			EvidenceID:    fmt.Sprintf("ev-%s-%d", in.UnitID, time.Now().UnixNano()),
			UnitID:        in.UnitID,
			CommitSHA:     commitSHA,
			Profile:       adapterkit.EvidenceProfile(profile),
			VerifiedAt:    time.Now().UTC().Format(time.RFC3339),
			VerifiedBy:    "agent",
			Checks:        report.ToGateChecks(),
		}
		if err := uc.port.SaveEvidence(ctx, absDir, ev); err == nil {
			evidenceSaved = true
		}
	}

	summary := "PASS: all quality gate checks satisfied"
	if !report.Passed {
		summary = fmt.Sprintf("FAIL: %d checks failed out of %d", report.FailedCount, len(report.Checks))
	}

	return &Output{
		SchemaVersion: "v2",
		UnitID:        in.UnitID,
		Profile:       profile,
		Passed:        report.Passed,
		CommitSHA:     commitSHA,
		DurationMS:    durationMS,
		Checks:        checkResults,
		ScorecardPath: scorecardPath,
		EvidenceSaved: evidenceSaved,
		Summary:       summary,
	}, nil
}

// Run is a convenience function.
func Run(ctx context.Context, in Input, port ...GatePort) (*Output, error) {
	var p GatePort
	if len(port) > 0 {
		p = port[0]
	}
	return New(p).Run(ctx, in)
}
