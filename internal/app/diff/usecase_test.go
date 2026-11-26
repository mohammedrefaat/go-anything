package diff_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/app/diff"
	"github.com/mohammedrefaat/go-anything/internal/replay"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReplayComparatorPort is a fake implementation for testing diff.UseCase.
type fakeReplayComparatorPort struct {
	loadCasesFunc   func(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error)
	loadRulesFunc   func(ctx context.Context, dir string) (*adapterkit.Rules, error)
	replayCasesFunc func(ctx context.Context, unitID string, cases []*adapterkit.Case, target string, mode string, jobs int, rules *adapterkit.Rules) (*replay.ReplayReport, error)
}

func (f *fakeReplayComparatorPort) LoadCases(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error) {
	if f.loadCasesFunc != nil {
		return f.loadCasesFunc(ctx, dir, unitID)
	}
	return []*adapterkit.Case{
		{
			CaseID: "case-001",
			UnitID: unitID,
			Kind:   adapterkit.KindHappy,
			Input: adapterkit.CaseInput{
				Argv: []string{"test"},
			},
			ExpectedOutput: adapterkit.CaseOutput{
				Stdout:   "hello\n",
				ExitCode: 0,
			},
		},
		{
			CaseID: "case-002",
			UnitID: unitID,
			Kind:   adapterkit.KindEdge,
			Input: adapterkit.CaseInput{
				Argv: []string{"test", "--edge"},
			},
			ExpectedOutput: adapterkit.CaseOutput{
				Stdout:   "edge\n",
				ExitCode: 0,
			},
		},
	}, nil
}

func (f *fakeReplayComparatorPort) LoadRules(ctx context.Context, dir string) (*adapterkit.Rules, error) {
	if f.loadRulesFunc != nil {
		return f.loadRulesFunc(ctx, dir)
	}
	return nil, nil
}

func (f *fakeReplayComparatorPort) ReplayCases(ctx context.Context, unitID string, cases []*adapterkit.Case, target string, mode string, jobs int, rules *adapterkit.Rules) (*replay.ReplayReport, error) {
	if f.replayCasesFunc != nil {
		return f.replayCasesFunc(ctx, unitID, cases, target, mode, jobs, rules)
	}
	return &replay.ReplayReport{
		SchemaVersion: "v1alpha1",
		UnitID:        unitID,
		TotalCases:    len(cases),
		PassedCases:   len(cases),
		FailedCases:   0,
		Results: []replay.CaseResult{
			{CaseID: "case-001", UnitID: unitID, Passed: true},
			{CaseID: "case-002", UnitID: unitID, Passed: true},
		},
		Summary: "PASS: all cases matched",
	}, nil
}

func TestDiff_AllMatched(t *testing.T) {
	port := &fakeReplayComparatorPort{}
	uc := diff.New(port)

	out, err := uc.Run(context.Background(), diff.Input{
		UnitID: "unit-auth",
		Target: "bin/auth",
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "unit-auth", out.UnitID)
	assert.Equal(t, "bin/auth", out.Target)
	assert.Equal(t, 2, out.TotalCases)
	assert.Equal(t, 2, out.Matched)
	assert.Equal(t, 0, out.Mismatched)
	assert.True(t, out.Passed)
	assert.Empty(t, out.Mismatches)

	// Validate JSON schema
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("diff.output.schema.json", data)
	require.NoError(t, err, "diff output JSON must satisfy v2 schema: %s", string(data))
}

func TestDiff_WithMismatches_SortedByCaseID(t *testing.T) {
	port := &fakeReplayComparatorPort{
		replayCasesFunc: func(ctx context.Context, unitID string, cases []*adapterkit.Case, target string, mode string, jobs int, rules *adapterkit.Rules) (*replay.ReplayReport, error) {
			return &replay.ReplayReport{
				UnitID:      unitID,
				TotalCases:  2,
				PassedCases: 0,
				FailedCases: 2,
				Results: []replay.CaseResult{
					// Out of order to verify sorting
					{
						CaseID: "case-002",
						UnitID: unitID,
						Passed: false,
						Mismatches: []adapterkit.Mismatch{
							{Path: "stdout", Expected: "edge\n", Actual: "different\n", Message: "string mismatch"},
						},
					},
					{
						CaseID: "case-001",
						UnitID: unitID,
						Passed: false,
						Mismatches: []adapterkit.Mismatch{
							{Path: "exit_code", Expected: 0, Actual: 1, Message: "exit code mismatch"},
						},
					},
				},
				Summary: "FAIL: 2/2 cases failed",
			}, nil
		},
	}
	uc := diff.New(port)

	out, err := uc.Run(context.Background(), diff.Input{
		UnitID: "unit-auth",
		Target: "bin/auth",
	})
	require.NoError(t, err)
	assert.False(t, out.Passed)
	assert.Equal(t, 2, out.Mismatched)
	require.Len(t, out.Mismatches, 2)

	// Rule 2 verification: must be sorted by case_id!
	assert.Equal(t, "case-001", out.Mismatches[0].CaseID)
	assert.Equal(t, "case-002", out.Mismatches[1].CaseID)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("diff.output.schema.json", data)
	require.NoError(t, err)
}

func TestDiff_CaseFilter(t *testing.T) {
	var requestedCases []*adapterkit.Case
	port := &fakeReplayComparatorPort{
		replayCasesFunc: func(ctx context.Context, unitID string, cases []*adapterkit.Case, target string, mode string, jobs int, rules *adapterkit.Rules) (*replay.ReplayReport, error) {
			requestedCases = cases
			return &replay.ReplayReport{
				UnitID:      unitID,
				TotalCases:  1,
				PassedCases: 1,
				Results:     []replay.CaseResult{{CaseID: cases[0].CaseID, Passed: true}},
			}, nil
		},
	}
	uc := diff.New(port)

	out, err := uc.Run(context.Background(), diff.Input{
		UnitID:     "unit-auth",
		Target:     "bin/auth",
		CaseFilter: "case-002",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, out.TotalCases)
	require.Len(t, requestedCases, 1)
	assert.Equal(t, "case-002", requestedCases[0].CaseID)
}

func TestDiff_ValidationErrors(t *testing.T) {
	uc := diff.New(&fakeReplayComparatorPort{})

	_, err := uc.Run(context.Background(), diff.Input{Target: "bin/target"})
	assert.ErrorContains(t, err, "unit ID is required")

	_, err = uc.Run(context.Background(), diff.Input{UnitID: "u1"})
	assert.ErrorContains(t, err, "target ported implementation must be specified")
}

func TestDiff_ReplayError(t *testing.T) {
	port := &fakeReplayComparatorPort{
		replayCasesFunc: func(ctx context.Context, unitID string, cases []*adapterkit.Case, target string, mode string, jobs int, rules *adapterkit.Rules) (*replay.ReplayReport, error) {
			return nil, errors.New("connection refused to target")
		},
	}
	uc := diff.New(port)
	_, err := uc.Run(context.Background(), diff.Input{
		UnitID: "unit-auth",
		Target: "http://localhost:9999",
	})
	assert.ErrorContains(t, err, "replay execution failed")
}
