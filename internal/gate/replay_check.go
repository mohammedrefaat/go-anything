package gate

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/mohammedrefat/go-anything/internal/replay"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// ReplayChecker executes deterministic case replay and enforces zero mismatches.
type ReplayChecker struct {
	UnitID    string
	Cases     []*adapterkit.Case
	CasesFile string
	Driver    adapterkit.Replay
	Rules     *adapterkit.Rules
	Jobs      int
	Mode      string
}

// Check replays cases and validates that all pass with zero mismatches.
func (rc *ReplayChecker) Check(ctx context.Context) (output string, issues []string, passed bool, err error) {
	cases := rc.Cases
	if len(cases) == 0 && rc.CasesFile != "" {
		loaded, loadErr := loadCasesFromJSONL(rc.CasesFile)
		if loadErr != nil {
			return "", []string{fmt.Sprintf("failed to load cases: %v", loadErr)}, false, loadErr
		}
		cases = loaded
	}

	if len(cases) == 0 {
		return "replay check failed: no test cases available", []string{"no test cases provided for replay"}, false, nil
	}

	if rc.Driver == nil {
		return "replay check failed: target replay driver is nil", []string{"missing target replay driver"}, false, nil
	}

	var opts []replay.Option
	if rc.Jobs > 0 {
		opts = append(opts, replay.WithJobs(rc.Jobs))
	}
	if rc.Rules != nil {
		opts = append(opts, replay.WithRules(rc.Rules))
	}
	if rc.Mode != "" {
		opts = append(opts, replay.WithMode(rc.Mode))
	}

	engine := replay.New(opts...)
	report, err := engine.ReplayCases(ctx, rc.UnitID, cases, rc.Driver)
	if err != nil {
		return "", []string{fmt.Sprintf("replay engine failure: %v", err)}, false, err
	}

	if report.FailedCases > 0 {
		var mismatchIssues []string
		for _, res := range report.Results {
			if !res.Passed {
				if len(res.Mismatches) > 0 {
					mismatchIssues = append(mismatchIssues,
						fmt.Sprintf("case %s: %d mismatch(es) on kind %s", res.CaseID, len(res.Mismatches), res.Kind))
				} else if res.Error != "" {
					mismatchIssues = append(mismatchIssues,
						fmt.Sprintf("case %s: error %s", res.CaseID, res.Error))
				} else {
					mismatchIssues = append(mismatchIssues,
						fmt.Sprintf("case %s: failed on kind %s", res.CaseID, res.Kind))
				}
			}
		}
		sort.Strings(mismatchIssues)
		return report.ToMarkdown(), mismatchIssues, false, nil
	}

	return fmt.Sprintf("100%% replay matched (%d/%d cases, 0 mismatches)", report.PassedCases, report.TotalCases),
		nil, true, nil
}

// CheckReplayExecutor creates a CheckExecutor that runs deterministic replay.
func CheckReplayExecutor(unitID string, cases []*adapterkit.Case, driver adapterkit.Replay, rules *adapterkit.Rules, jobs int) CheckExecutor {
	checker := &ReplayChecker{
		UnitID: unitID,
		Cases:  cases,
		Driver: driver,
		Rules:  rules,
		Jobs:   jobs,
	}
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		return checker.Check(ctx)
	}
}

// CheckReplayFromDirExecutor creates a CheckExecutor that automatically loads cases from .go-anything/cases/<unit>.jsonl.
func CheckReplayFromDirExecutor(workspaceDir, unitID string, driver adapterkit.Replay, rules *adapterkit.Rules, jobs int) CheckExecutor {
	casePath := filepath.Join(workspaceDir, ".go-anything", "cases", filepath.FromSlash(unitID)+".jsonl")
	checker := &ReplayChecker{
		UnitID:    unitID,
		CasesFile: casePath,
		Driver:    driver,
		Rules:     rules,
		Jobs:      jobs,
	}
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		return checker.Check(ctx)
	}
}
