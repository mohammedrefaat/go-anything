package gate_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/app/gate"
	internalgate "github.com/mohammedrefaat/go-anything/internal/gate"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGatePort is a fake implementation of gate.GatePort for testing.
type fakeGatePort struct {
	runGateFunc           func(ctx context.Context, cfg internalgate.Config) (*internalgate.GateReport, error)
	generateScorecardFunc func(ctx context.Context, dir string, unitID string, report *internalgate.GateReport) (string, error)
	saveEvidenceFunc      func(ctx context.Context, dir string, ev *adapterkit.Evidence) error
	getCommitSHAFunc      func(ctx context.Context, dir string) (string, error)
	savedEvidence         *adapterkit.Evidence
}

func (f *fakeGatePort) RunGate(ctx context.Context, cfg internalgate.Config) (*internalgate.GateReport, error) {
	if f.runGateFunc != nil {
		return f.runGateFunc(ctx, cfg)
	}
	checks := make([]internalgate.CheckReport, 0, len(cfg.Checks))
	for _, name := range cfg.Checks {
		checks = append(checks, internalgate.CheckReport{
			Name:     name,
			Status:   internalgate.StatusPass,
			Duration: 10 * time.Millisecond,
		})
	}
	return &internalgate.GateReport{
		Passed:      true,
		PassedCount: len(checks),
		FailedCount: 0,
		Checks:      checks,
	}, nil
}

func (f *fakeGatePort) GenerateScorecard(ctx context.Context, dir string, unitID string, report *internalgate.GateReport) (string, error) {
	if f.generateScorecardFunc != nil {
		return f.generateScorecardFunc(ctx, dir, unitID, report)
	}
	return "/path/to/.go-anything/reports/scorecard-" + unitID + ".md", nil
}

func (f *fakeGatePort) SaveEvidence(ctx context.Context, dir string, ev *adapterkit.Evidence) error {
	f.savedEvidence = ev
	if f.saveEvidenceFunc != nil {
		return f.saveEvidenceFunc(ctx, dir, ev)
	}
	return nil
}

func (f *fakeGatePort) GetCurrentCommitSHA(ctx context.Context, dir string) (string, error) {
	if f.getCommitSHAFunc != nil {
		return f.getCommitSHAFunc(ctx, dir)
	}
	return "0123456789abcdef0123456789abcdef01234567", nil
}

func TestGate_Success_FullProfile(t *testing.T) {
	port := &fakeGatePort{}
	uc := gate.New(port)

	out, err := uc.Run(context.Background(), gate.Input{
		UnitID:    "unit-core",
		Profile:   "full",
		Scorecard: true,
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "unit-core", out.UnitID)
	assert.Equal(t, "full", out.Profile)
	assert.True(t, out.Passed)
	assert.True(t, out.EvidenceSaved)
	assert.NotEmpty(t, out.ScorecardPath)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", out.CommitSHA)
	assert.Len(t, out.Checks, len(internalgate.FullProfileChecks()))
	assert.Equal(t, "api_compat", out.Checks[0].Name)
	assert.Equal(t, "build", out.Checks[1].Name)

	// Validate JSON schema
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("gate.output.schema.json", data)
	require.NoError(t, err, "gate output JSON must satisfy v2 schema: %s", string(data))
}

func TestGate_SkippedCheck_NoEvidenceSaved(t *testing.T) {
	port := &fakeGatePort{
		runGateFunc: func(ctx context.Context, cfg internalgate.Config) (*internalgate.GateReport, error) {
			checks := make([]internalgate.CheckReport, 0, len(cfg.Checks))
			passedCount := 0
			skippedCount := 0
			for _, name := range cfg.Checks {
				status := internalgate.StatusPass
				if name == internalgate.CheckStaticcheck {
					status = internalgate.StatusSkipped
					skippedCount++
				} else {
					passedCount++
				}
				checks = append(checks, internalgate.CheckReport{Name: name, Status: status})
			}
			return &internalgate.GateReport{
				Passed:       true,
				PassedCount:  passedCount,
				SkippedCount: skippedCount,
				Checks:       checks,
			}, nil
		},
	}
	uc := gate.New(port)

	out, err := uc.Run(context.Background(), gate.Input{
		UnitID:  "unit-core",
		Profile: "full",
	})
	require.NoError(t, err)
	assert.False(t, out.Passed)
	assert.False(t, out.EvidenceSaved)
	assert.Nil(t, port.savedEvidence)
}

func TestGate_FailedCheck_NoEvidenceSaved(t *testing.T) {
	port := &fakeGatePort{
		runGateFunc: func(ctx context.Context, cfg internalgate.Config) (*internalgate.GateReport, error) {
			return &internalgate.GateReport{
				Passed:      false,
				PassedCount: 1,
				FailedCount: 1,
				Checks: []internalgate.CheckReport{
					{Name: "build", Status: internalgate.StatusPass, Duration: 20 * time.Millisecond},
					{Name: "vet", Status: internalgate.StatusFail, Duration: 15 * time.Millisecond, Issues: []string{"vet error found"}},
				},
			}, nil
		},
	}
	uc := gate.New(port)

	out, err := uc.Run(context.Background(), gate.Input{
		UnitID:  "unit-core",
		Profile: "full",
	})
	require.NoError(t, err)
	assert.False(t, out.Passed)
	assert.False(t, out.EvidenceSaved)
	assert.Nil(t, port.savedEvidence)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("gate.output.schema.json", data)
	require.NoError(t, err)
}

func TestGate_ReplayProfile(t *testing.T) {
	var executedChecks []string
	port := &fakeGatePort{
		runGateFunc: func(ctx context.Context, cfg internalgate.Config) (*internalgate.GateReport, error) {
			executedChecks = cfg.Checks
			return &internalgate.GateReport{
				Passed:      true,
				PassedCount: 1,
				Checks: []internalgate.CheckReport{
					{Name: "replay", Status: internalgate.StatusPass},
				},
			}, nil
		},
	}
	uc := gate.New(port)

	out, err := uc.Run(context.Background(), gate.Input{
		UnitID:  "unit-core",
		Profile: "replay",
	})
	require.NoError(t, err)
	assert.True(t, out.Passed)
	assert.Equal(t, []string{"replay"}, executedChecks)
}

func TestGate_ValidationErrors(t *testing.T) {
	uc := gate.New(&fakeGatePort{})

	_, err := uc.Run(context.Background(), gate.Input{})
	assert.ErrorContains(t, err, "unit ID is required")

	_, err = uc.Run(context.Background(), gate.Input{UnitID: "u1", Profile: "invalid-profile"})
	assert.ErrorContains(t, err, "unsupported verification profile")
}
