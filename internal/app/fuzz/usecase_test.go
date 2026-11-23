package fuzz_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/app/fuzz"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFuzzPort is a fake implementation of fuzz.FuzzPort for testing.
type fakeFuzzPort struct {
	loadSeedCasesFunc func(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error)
	executeFuzzFunc   func(ctx context.Context, req fuzz.FuzzExecutionRequest) (*fuzz.FuzzExecutionResult, error)
	promoteCasesFunc  func(ctx context.Context, dir, unitID string, cases []*adapterkit.Case) error
	saveReportFunc    func(ctx context.Context, dir, unitID string, out *fuzz.Output) error

	promotedCases []*adapterkit.Case
	savedReport   *fuzz.Output
}

func (f *fakeFuzzPort) LoadSeedCases(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error) {
	if f.loadSeedCasesFunc != nil {
		return f.loadSeedCasesFunc(ctx, dir, unitID)
	}
	return []*adapterkit.Case{
		{
			CaseID: "seed-001",
			UnitID: unitID,
			Kind:   adapterkit.KindHappy,
			Input: adapterkit.CaseInput{
				Argv: []string{"calculate", "42"},
			},
			ExpectedOutput: adapterkit.CaseOutput{
				Stdout:   "42\n",
				ExitCode: 0,
			},
		},
	}, nil
}

func (f *fakeFuzzPort) ExecuteFuzz(ctx context.Context, req fuzz.FuzzExecutionRequest) (*fuzz.FuzzExecutionResult, error) {
	if f.executeFuzzFunc != nil {
		return f.executeFuzzFunc(ctx, req)
	}
	return &fuzz.FuzzExecutionResult{
		TotalRuns:        req.Runs,
		DivergencesCount: 0,
		Divergences:      []fuzz.DivergenceRecord{},
		DivergentCases:   []*adapterkit.Case{},
	}, nil
}

func (f *fakeFuzzPort) PromoteCases(ctx context.Context, dir, unitID string, cases []*adapterkit.Case) error {
	f.promotedCases = append(f.promotedCases, cases...)
	if f.promoteCasesFunc != nil {
		return f.promoteCasesFunc(ctx, dir, unitID, cases)
	}
	return nil
}

func (f *fakeFuzzPort) SaveReport(ctx context.Context, dir, unitID string, out *fuzz.Output) error {
	f.savedReport = out
	if f.saveReportFunc != nil {
		return f.saveReportFunc(ctx, dir, unitID, out)
	}
	return nil
}

func TestFuzz_ZeroDivergences(t *testing.T) {
	port := &fakeFuzzPort{}
	uc := fuzz.New(port)

	out, err := uc.Run(context.Background(), fuzz.Input{
		UnitID: "unit-calculator",
		Runs:   50,
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "unit-calculator", out.UnitID)
	assert.Equal(t, 50, out.TotalRuns)
	assert.Equal(t, 0, out.DivergencesCount)
	assert.Empty(t, out.Divergences)
	assert.Equal(t, 0, out.PromotedCount)
	assert.Empty(t, out.PromotedCases)
	assert.True(t, out.Passed)
	assert.Contains(t, out.Summary, "PASS")

	// Validate JSON schema
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("fuzz.report.schema.json", data)
	require.NoError(t, err, "fuzz output JSON must satisfy v2 schema: %s", string(data))
}

func TestFuzz_WithDivergences_Promoted(t *testing.T) {
	divergentCase := &adapterkit.Case{
		CaseID: "fuzz-unit-calc-0001",
		UnitID: "unit-calculator",
		Kind:   adapterkit.KindFuzz,
		Input: adapterkit.CaseInput{
			Argv: []string{"calculate", "-0"},
		},
		ExpectedOutput: adapterkit.CaseOutput{
			Stdout:   "0\n",
			ExitCode: 0,
		},
	}

	port := &fakeFuzzPort{
		executeFuzzFunc: func(ctx context.Context, req fuzz.FuzzExecutionRequest) (*fuzz.FuzzExecutionResult, error) {
			return &fuzz.FuzzExecutionResult{
				TotalRuns:        req.Runs,
				DivergencesCount: 1,
				Divergences: []fuzz.DivergenceRecord{
					{
						CaseID:       divergentCase.CaseID,
						Input:        divergentCase.Input,
						LegacyOutput: divergentCase.ExpectedOutput,
						ModernOutput: adapterkit.CaseOutput{Stdout: "-0\n", ExitCode: 0},
						Diff:         "output stdout mismatch",
					},
				},
				DivergentCases: []*adapterkit.Case{divergentCase},
			}, nil
		},
	}
	uc := fuzz.New(port)

	out, err := uc.Run(context.Background(), fuzz.Input{
		UnitID:          "unit-calculator",
		Runs:            100,
		PromoteFindings: true,
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "unit-calculator", out.UnitID)
	assert.Equal(t, 100, out.TotalRuns)
	assert.Equal(t, 1, out.DivergencesCount)
	assert.Len(t, out.Divergences, 1)
	assert.Equal(t, 1, out.PromotedCount)
	assert.Equal(t, []string{"fuzz-unit-calc-0001"}, out.PromotedCases)
	assert.False(t, out.Passed)
	assert.Contains(t, out.Summary, "FAIL")

	// Verify case was promoted through port
	require.Len(t, port.promotedCases, 1)
	assert.Equal(t, "fuzz-unit-calc-0001", port.promotedCases[0].CaseID)

	// Validate JSON schema
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("fuzz.report.schema.json", data)
	require.NoError(t, err)
}

func TestFuzz_ValidationErrors(t *testing.T) {
	uc := fuzz.New(&fakeFuzzPort{})

	_, err := uc.Run(context.Background(), fuzz.Input{})
	assert.ErrorContains(t, err, "unit ID is required")
}

func TestFuzz_Timeout(t *testing.T) {
	port := &fakeFuzzPort{}
	uc := fuzz.New(port)

	out, err := uc.Run(context.Background(), fuzz.Input{
		UnitID: "unit-calculator",
		Time:   10 * time.Millisecond,
		Runs:   0,
	})
	require.NoError(t, err)
	assert.True(t, out.Passed)
}
