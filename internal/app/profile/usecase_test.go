package profile_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/app/profile"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProfilePort is a fake implementation of profile.ProfilePort for testing.
type fakeProfilePort struct {
	detectLanguageFunc func(ctx context.Context, dir, unitID string) (string, error)
	profileUnitFunc    func(ctx context.Context, dir, unitID, language string, top int) (*profile.ProfileExecutionResult, error)
	saveReportFunc     func(ctx context.Context, dir, unitID string, out *profile.Output) error

	savedReport *profile.Output
}

func (f *fakeProfilePort) DetectLanguage(ctx context.Context, dir, unitID string) (string, error) {
	if f.detectLanguageFunc != nil {
		return f.detectLanguageFunc(ctx, dir, unitID)
	}
	return "python", nil
}

func (f *fakeProfilePort) ProfileUnit(ctx context.Context, dir, unitID, language string, top int) (*profile.ProfileExecutionResult, error) {
	if f.profileUnitFunc != nil {
		return f.profileUnitFunc(ctx, dir, unitID, language, top)
	}
	return &profile.ProfileExecutionResult{
		Language:        "python",
		Profiler:        "cProfile",
		TotalDurationMS: 125.5,
		Candidates: []profile.Candidate{
			{
				FunctionName:      "validate_token",
				File:              "auth/token.py",
				Line:              34,
				SelfPercent:       18.5,
				CumulativePercent: 22.0,
				CallCount:         500,
				IsPureHeuristic:   true,
				PurityReason:      "Algorithmic token computation; no external I/O (heuristic)",
			},
			{
				FunctionName:      "compute_heavy_hash",
				File:              "auth/crypto.py",
				Line:              112,
				SelfPercent:       55.0,
				CumulativePercent: 65.0,
				CallCount:         1200,
				IsPureHeuristic:   true,
				PurityReason:      "CPU-bound cryptographic math without system calls (heuristic)",
			},
			{
				FunctionName:      "fetch_remote_jwks",
				File:              "auth/remote.py",
				Line:              80,
				SelfPercent:       10.0,
				CumulativePercent: 30.0,
				CallCount:         5,
				IsPureHeuristic:   false,
				PurityReason:      "Contains external network I/O calls to remote server (heuristic)",
			},
		},
	}, nil
}

func (f *fakeProfilePort) SaveReport(ctx context.Context, dir, unitID string, out *profile.Output) error {
	f.savedReport = out
	if f.saveReportFunc != nil {
		return f.saveReportFunc(ctx, dir, unitID, out)
	}
	return nil
}

func TestProfile_Success(t *testing.T) {
	port := &fakeProfilePort{}
	uc := profile.New(port)

	out, err := uc.Run(context.Background(), profile.Input{
		UnitID: "unit-auth",
		Top:    2,
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "unit-auth", out.UnitID)
	assert.Equal(t, "python", out.Language)
	assert.Equal(t, "cProfile", out.Profiler)
	assert.Equal(t, 2, out.Top)
	require.Len(t, out.Candidates, 2)

	// Check ranking: compute_heavy_hash (55%) must be Rank 1, validate_token (18.5%) must be Rank 2
	assert.Equal(t, 1, out.Candidates[0].Rank)
	assert.Equal(t, "compute_heavy_hash", out.Candidates[0].FunctionName)
	assert.Equal(t, 55.0, out.Candidates[0].SelfPercent)
	assert.True(t, out.Candidates[0].IsPureHeuristic)
	assert.Contains(t, out.Candidates[0].PurityReason, "heuristic")

	assert.Equal(t, 2, out.Candidates[1].Rank)
	assert.Equal(t, "validate_token", out.Candidates[1].FunctionName)
	assert.Equal(t, 18.5, out.Candidates[1].SelfPercent)

	assert.NotEmpty(t, out.Summary)
	assert.NotEmpty(t, out.Timestamp)

	// Validate against schemas/v2/profile.report.schema.json
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("profile.report.schema.json", data)
	require.NoError(t, err, "profile output JSON must satisfy v2 schema: %s", string(data))
}

func TestProfile_PurityHeuristic(t *testing.T) {
	port := &fakeProfilePort{}
	uc := profile.New(port)

	out, err := uc.Run(context.Background(), profile.Input{
		UnitID: "unit-auth",
		Top:    5,
	})
	require.NoError(t, err)
	require.Len(t, out.Candidates, 3)

	// Find impure candidate
	var impureCandidate *profile.Candidate
	for i := range out.Candidates {
		if !out.Candidates[i].IsPureHeuristic {
			impureCandidate = &out.Candidates[i]
			break
		}
	}
	require.NotNil(t, impureCandidate)
	assert.Equal(t, "fetch_remote_jwks", impureCandidate.FunctionName)
	assert.False(t, impureCandidate.IsPureHeuristic)
	assert.Contains(t, impureCandidate.PurityReason, "heuristic")
}

func TestProfile_ValidationErrors(t *testing.T) {
	uc := profile.New(&fakeProfilePort{})

	_, err := uc.Run(context.Background(), profile.Input{})
	assert.ErrorContains(t, err, "unit ID is required")
}
