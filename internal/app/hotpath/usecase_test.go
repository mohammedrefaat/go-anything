package hotpath_test

import (
	"context"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/app/hotpath"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHotpathPort struct {
	selectedFunction string
	casesCount       int
	scaffoldFiles    []string
	isImplemented    bool
	replayPass       bool
	fuzzPass         bool
	gatePass         bool
	shadowEnabled    bool
	unit             *adapterkit.Unit
	calls            map[string]int
}

func newMockPort() *mockHotpathPort {
	return &mockHotpathPort{
		selectedFunction: "calculate_tax",
		casesCount:       5,
		scaffoldFiles:    []string{"main.go", "types.go", "handlers.go", "handlers_impl.go"},
		isImplemented:    false,
		replayPass:       true,
		fuzzPass:         true,
		gatePass:         true,
		unit: &adapterkit.Unit{
			ID:              "pkg/billing",
			Name:            "pkg/billing",
			State:           adapterkit.StatePlanned,
			EvidenceProfile: adapterkit.ProfileReplay,
			Metadata:        map[string]any{},
		},
		calls: make(map[string]int),
	}
}

func (m *mockHotpathPort) DetectOrSelectFunction(ctx context.Context, dir, unitID, requestedFn string) (string, error) {
	m.calls["DetectOrSelectFunction"]++
	if requestedFn != "" {
		return requestedFn, nil
	}
	return m.selectedFunction, nil
}

func (m *mockHotpathPort) EnsureCases(ctx context.Context, dir, unitID, function string) (int, error) {
	m.calls["EnsureCases"]++
	return m.casesCount, nil
}

func (m *mockHotpathPort) ScaffoldBridge(ctx context.Context, dir, unitID, function string) ([]string, error) {
	m.calls["ScaffoldBridge"]++
	return m.scaffoldFiles, nil
}

func (m *mockHotpathPort) CheckImplementation(ctx context.Context, dir, unitID string) (bool, string, error) {
	m.calls["CheckImplementation"]++
	if !m.isImplemented {
		return false, "handlers_impl.go still returns ErrNotPorted", nil
	}
	return true, "Go implementation detected", nil
}

func (m *mockHotpathPort) ReplayAndDiff(ctx context.Context, dir, unitID string) (bool, string, error) {
	m.calls["ReplayAndDiff"]++
	if !m.replayPass {
		return false, "1 case mismatched", nil
	}
	return true, "0 replay mismatches", nil
}

func (m *mockHotpathPort) FuzzAndBench(ctx context.Context, dir, unitID string) (bool, string, error) {
	m.calls["FuzzAndBench"]++
	if !m.fuzzPass {
		return false, "fuzz found divergence", nil
	}
	return true, "fuzz and bench clean", nil
}

func (m *mockHotpathPort) RunGate(ctx context.Context, dir, unitID string) (bool, string, error) {
	m.calls["RunGate"]++
	if !m.gatePass {
		return false, "vet failed", nil
	}
	return true, "gate passed", nil
}

func (m *mockHotpathPort) EnableShadow(ctx context.Context, dir, unitID string) error {
	m.calls["EnableShadow"]++
	m.shadowEnabled = true
	return nil
}

func (m *mockHotpathPort) GetUnit(dir, unitID string) (*adapterkit.Unit, error) {
	m.calls["GetUnit"]++
	return m.unit, nil
}

func (m *mockHotpathPort) SaveUnit(dir string, unit *adapterkit.Unit) error {
	m.calls["SaveUnit"]++
	m.unit = unit
	return nil
}

func TestHotpath_PauseWhenUnimplemented(t *testing.T) {
	port := newMockPort()
	port.isImplemented = false

	uc := hotpath.New(port)
	out, err := uc.Run(context.Background(), hotpath.Input{
		UnitID: "pkg/billing",
	})
	require.NoError(t, err)

	assert.Equal(t, hotpath.PipelineStatusPausedForImplementation, out.Status)
	assert.Equal(t, hotpath.StepImplement, out.CurrentStep)
	assert.Equal(t, "calculate_tax", out.Function)
	assert.Contains(t, out.HumanActionRequired, "Implement Go port in bridges/pkg/billing/handlers_impl.go")

	// Ensure later steps were NOT executed
	assert.Equal(t, 0, port.calls["ReplayAndDiff"])
	assert.Equal(t, 0, port.calls["RunGate"])
}

func TestHotpath_CompletesWhenImplemented_ReachesHumanApprovalBoundary(t *testing.T) {
	port := newMockPort()
	port.isImplemented = true

	uc := hotpath.New(port)
	out, err := uc.Run(context.Background(), hotpath.Input{
		UnitID:   "pkg/billing",
		Function: "calculate_tax",
	})
	require.NoError(t, err)

	assert.Equal(t, hotpath.PipelineStatusReadyForApproval, out.Status)
	assert.Equal(t, "Shadow", out.UnitState)
	assert.Equal(t, "go-anything approve --unit pkg/billing", out.HumanActionRequired)
	assert.True(t, port.shadowEnabled)

	// All steps executed
	assert.Equal(t, 1, port.calls["EnsureCases"])
	assert.Equal(t, 1, port.calls["ScaffoldBridge"])
	assert.Equal(t, 1, port.calls["CheckImplementation"])
	assert.Equal(t, 1, port.calls["ReplayAndDiff"])
	assert.Equal(t, 1, port.calls["FuzzAndBench"])
	assert.Equal(t, 1, port.calls["RunGate"])
	assert.Equal(t, 1, port.calls["EnableShadow"])
}

func TestHotpath_Resumable_SkipsCompletedSteps(t *testing.T) {
	port := newMockPort()
	port.isImplemented = true

	uc := hotpath.New(port)
	// Run 1: completes all steps
	out1, err := uc.Run(context.Background(), hotpath.Input{UnitID: "pkg/billing"})
	require.NoError(t, err)
	assert.Equal(t, hotpath.PipelineStatusReadyForApproval, out1.Status)

	// Run 2: resume should skip completed steps
	out2, err := uc.Run(context.Background(), hotpath.Input{UnitID: "pkg/billing"})
	require.NoError(t, err)
	assert.Equal(t, hotpath.PipelineStatusReadyForApproval, out2.Status)

	// Calls count shouldn't have doubled for cached steps
	assert.Equal(t, 1, port.calls["EnsureCases"])
	assert.Equal(t, 1, port.calls["ScaffoldBridge"])
	assert.Equal(t, 1, port.calls["ReplayAndDiff"])
}

func TestHotpath_DiffFailureStopsPipeline(t *testing.T) {
	port := newMockPort()
	port.isImplemented = true
	port.replayPass = false

	uc := hotpath.New(port)
	out, err := uc.Run(context.Background(), hotpath.Input{UnitID: "pkg/billing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replay and diff failed")
	assert.Equal(t, hotpath.PipelineStatusFailed, out.Status)
	assert.Equal(t, hotpath.StepReplayDiff, out.CurrentStep)
	assert.Equal(t, 0, port.calls["RunGate"])
}
