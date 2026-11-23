package diff

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// UseCase coordinates loading recorded cases and comparing them against the target implementation.
type UseCase struct {
	port ReplayComparatorPort
}

// New creates a new diff UseCase.
func New(port ReplayComparatorPort) *UseCase {
	if port == nil {
		port = NewDefaultReplayComparatorPort()
	}
	return &UseCase{port: port}
}

// Run executes the diff use-case.
func (uc *UseCase) Run(ctx context.Context, in Input) (*Output, error) {
	if in.UnitID == "" {
		return nil, fmt.Errorf("unit ID is required")
	}
	if in.Target == "" {
		return nil, fmt.Errorf("target ported implementation must be specified")
	}

	dir := in.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}

	jobs := in.Jobs
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}

	cases, err := uc.port.LoadCases(ctx, absDir, in.UnitID)
	if err != nil {
		return nil, fmt.Errorf("failed to load cases for unit %q: %w", in.UnitID, err)
	}

	if in.CaseFilter != "" {
		var filtered []*adapterkit.Case
		for _, c := range cases {
			if c.CaseID == in.CaseFilter {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("case %q not found for unit %q", in.CaseFilter, in.UnitID)
		}
		cases = filtered
	}

	rules, _ := uc.port.LoadRules(ctx, absDir)

	report, err := uc.port.ReplayCases(ctx, in.UnitID, cases, in.Target, in.Mode, jobs, rules)
	if err != nil {
		return nil, fmt.Errorf("replay execution failed: %w", err)
	}

	caseMap := make(map[string]*adapterkit.Case, len(cases))
	for _, c := range cases {
		caseMap[c.CaseID] = c
	}

	var mismatches []MismatchDetail
	for _, res := range report.Results {
		if !res.Passed {
			var inp, out any
			if c, ok := caseMap[res.CaseID]; ok {
				inp = c.Input
				out = c.ExpectedOutput
			}
			msg := res.Error
			if msg == "" && len(res.Mismatches) > 0 {
				msg = fmt.Sprintf("%d mismatch(es) detected", len(res.Mismatches))
			}
			diffs := res.Mismatches
			if diffs == nil {
				diffs = []adapterkit.Mismatch{}
			}
			mismatches = append(mismatches, MismatchDetail{
				CaseID:  res.CaseID,
				Diffs:   diffs,
				Inputs:  inp,
				Outputs: out,
				Message: msg,
				Error:   res.Error,
			})
		}
	}

	if mismatches == nil {
		mismatches = []MismatchDetail{}
	}

	// Rule 2: Strictly sort by CaseID
	sort.Slice(mismatches, func(i, j int) bool {
		return mismatches[i].CaseID < mismatches[j].CaseID
	})

	passed := report.FailedCases == 0 && report.TotalCases > 0
	if report.TotalCases == 0 {
		passed = true
	}

	summary := fmt.Sprintf("PASS: 100%% replay matched (%d/%d cases)", report.PassedCases, report.TotalCases)
	if report.FailedCases > 0 {
		summary = fmt.Sprintf("FAIL: %d/%d cases failed verification", report.FailedCases, report.TotalCases)
	}

	return &Output{
		SchemaVersion:   "v2",
		UnitID:          in.UnitID,
		Target:          in.Target,
		Mode:            in.Mode,
		TotalCases:      report.TotalCases,
		MatchedCases:    report.PassedCases,
		Matched:         report.PassedCases,
		MismatchedCases: report.FailedCases,
		Mismatched:      report.FailedCases,
		Passed:          passed,
		Mismatches:      mismatches,
		MismatchList:    mismatches,
		Summary:         summary,
	}, nil
}

// Run is a convenience function.
func Run(ctx context.Context, in Input, port ...ReplayComparatorPort) (*Output, error) {
	var p ReplayComparatorPort
	if len(port) > 0 {
		p = port[0]
	}
	return New(p).Run(ctx, in)
}
