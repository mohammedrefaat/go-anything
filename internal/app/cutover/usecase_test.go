package cutover_test

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/app/cutover"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCutoverPort is a fake implementation of cutover.CutoverPort for testing.
type fakeCutoverPort struct {
	getUnitFunc            func(ctx context.Context, dir, unitID string) (*adapterkit.Unit, error)
	isKillSwitchActiveFunc func(ctx context.Context, dir, unitID string) bool
	activateKillSwitchFunc func(ctx context.Context, dir, unitID string) error
	getFallbackCountFunc   func(ctx context.Context, dir, unitID string) (int64, error)
	killSwitchTriggered    bool
}

func (f *fakeCutoverPort) GetUnit(ctx context.Context, dir, unitID string) (*adapterkit.Unit, error) {
	if f.getUnitFunc != nil {
		return f.getUnitFunc(ctx, dir, unitID)
	}
	return &adapterkit.Unit{
		ID:    unitID,
		Name:  "Test Unit",
		State: adapterkit.StateApproved,
		Metadata: map[string]any{
			"target": "bin/test-target",
		},
	}, nil
}

func (f *fakeCutoverPort) IsKillSwitchActive(ctx context.Context, dir, unitID string) bool {
	if f.isKillSwitchActiveFunc != nil {
		return f.isKillSwitchActiveFunc(ctx, dir, unitID)
	}
	return f.killSwitchTriggered
}

func (f *fakeCutoverPort) ActivateKillSwitch(ctx context.Context, dir, unitID string) error {
	f.killSwitchTriggered = true
	if f.activateKillSwitchFunc != nil {
		return f.activateKillSwitchFunc(ctx, dir, unitID)
	}
	return nil
}

func (f *fakeCutoverPort) GetFallbackCount(ctx context.Context, dir, unitID string) (int64, error) {
	if f.getFallbackCountFunc != nil {
		return f.getFallbackCountFunc(ctx, dir, unitID)
	}
	return 0, nil
}

func TestCutover_Plan(t *testing.T) {
	port := &fakeCutoverPort{}
	uc := cutover.New(port)

	out, err := uc.Run(context.Background(), cutover.Input{
		UnitID: "unit-billing",
		Action: "plan",
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "unit-billing", out.UnitID)
	assert.Equal(t, "plan", out.Action)
	assert.Equal(t, "modern", out.RouteState)
	assert.False(t, out.KillSwitchActive)
	assert.Equal(t, "bin/test-target", out.TargetService)
	assert.Empty(t, out.Warnings)

	// Validate JSON schema
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("cutover.output.schema.json", data)
	require.NoError(t, err, "cutover output JSON must satisfy v2 schema: %s", string(data))
}

func TestCutover_Plan_WithWarnings(t *testing.T) {
	port := &fakeCutoverPort{
		getUnitFunc: func(ctx context.Context, dir, unitID string) (*adapterkit.Unit, error) {
			return &adapterkit.Unit{
				ID:    unitID,
				State: adapterkit.StatePlanned, // Not approved or verified
			}, nil
		},
		getFallbackCountFunc: func(ctx context.Context, dir, unitID string) (int64, error) {
			return 5, nil
		},
	}
	uc := cutover.New(port)

	out, err := uc.Run(context.Background(), cutover.Input{
		UnitID: "unit-billing",
		Action: "plan",
	})
	require.NoError(t, err)
	assert.Equal(t, "original", out.RouteState)
	assert.NotEmpty(t, out.Warnings)
	assert.Equal(t, int64(5), out.FallbackCount)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("cutover.output.schema.json", data)
	require.NoError(t, err)
}

func TestCutover_Status(t *testing.T) {
	port := &fakeCutoverPort{
		getUnitFunc: func(ctx context.Context, dir, unitID string) (*adapterkit.Unit, error) {
			return &adapterkit.Unit{
				ID:    unitID,
				State: adapterkit.StateShadow,
			}, nil
		},
	}
	uc := cutover.New(port)

	out, err := uc.Run(context.Background(), cutover.Input{
		UnitID: "unit-billing",
		Action: "status",
	})
	require.NoError(t, err)
	assert.Equal(t, "status", out.Action)
	assert.Equal(t, "shadow", out.RouteState)
	assert.False(t, out.KillSwitchActive)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("cutover.output.schema.json", data)
	require.NoError(t, err)
}

func TestCutover_Kill(t *testing.T) {
	port := &fakeCutoverPort{}
	uc := cutover.New(port)

	out, err := uc.Run(context.Background(), cutover.Input{
		UnitID: "unit-billing",
		Action: "kill",
	})
	require.NoError(t, err)
	assert.Equal(t, "kill", out.Action)
	assert.True(t, out.KillSwitchActive)
	assert.Equal(t, "original", out.RouteState)
	assert.True(t, port.killSwitchTriggered)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("cutover.output.schema.json", data)
	require.NoError(t, err)
}

// TestCutover_RuleR1_NeverPerformsLift strictly enforces Rule R-1 via AST inspection:
// No file under internal/app/cutover may import or call internal/lift!
func TestCutover_RuleR1_NeverPerformsLift(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	fset := token.NewFileSet()
	for _, file := range files {
		content, err := os.ReadFile(file)
		require.NoError(t, err)

		node, err := parser.ParseFile(fset, file, content, parser.ImportsOnly)
		require.NoError(t, err)

		for _, imp := range node.Imports {
			assert.NotContains(t, imp.Path.Value, "internal/lift",
				"Rule R-1 Violation: file %s imports lift package: %s", file, imp.Path.Value)
		}
	}
}

func TestCutover_ValidationErrors(t *testing.T) {
	uc := cutover.New(&fakeCutoverPort{})

	_, err := uc.Run(context.Background(), cutover.Input{})
	assert.ErrorContains(t, err, "unit ID is required")

	_, err = uc.Run(context.Background(), cutover.Input{
		UnitID: "u1",
		Action: "invalid-action",
	})
	assert.ErrorContains(t, err, "invalid cutover action")
}
