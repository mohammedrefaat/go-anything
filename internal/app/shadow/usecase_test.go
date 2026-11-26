package shadow_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/app/shadow"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeShadowPort is a fake implementation of shadow.ShadowPort for testing.
type fakeShadowPort struct {
	startShadowFunc func(ctx context.Context, dir, unitID string, port int, sampleRate float64) (*shadow.Output, error)
	getStatusFunc   func(ctx context.Context, dir, unitID string) (*shadow.Output, error)
	stopShadowFunc  func(ctx context.Context, dir, unitID string) (*shadow.Output, error)
}

func (f *fakeShadowPort) StartShadow(ctx context.Context, dir, unitID string, port int, sampleRate float64) (*shadow.Output, error) {
	if f.startShadowFunc != nil {
		return f.startShadowFunc(ctx, dir, unitID, port, sampleRate)
	}
	return &shadow.Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "start",
		Active:           true,
		Port:             port,
		SampleRate:       sampleRate,
		RequestsTotal:    0,
		TotalRequests:    0,
		RequestsShadowed: 0,
		Shadowed:         0,
		OverflowDropped:  0,
		MismatchesCount:  0,
		Mismatches:       0,
	}, nil
}

func (f *fakeShadowPort) GetShadowStatus(ctx context.Context, dir, unitID string) (*shadow.Output, error) {
	if f.getStatusFunc != nil {
		return f.getStatusFunc(ctx, dir, unitID)
	}
	return &shadow.Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "status",
		Active:           true,
		Port:             8080,
		SampleRate:       0.5,
		RequestsTotal:    100,
		TotalRequests:    100,
		RequestsShadowed: 50,
		Shadowed:         50,
		OverflowDropped:  0,
		MismatchesCount:  0,
		Mismatches:       0,
	}, nil
}

func (f *fakeShadowPort) StopShadow(ctx context.Context, dir, unitID string) (*shadow.Output, error) {
	if f.stopShadowFunc != nil {
		return f.stopShadowFunc(ctx, dir, unitID)
	}
	return &shadow.Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "stop",
		Active:           false,
		Port:             8080,
		SampleRate:       0.5,
		RequestsTotal:    100,
		TotalRequests:    100,
		RequestsShadowed: 50,
		Shadowed:         50,
		OverflowDropped:  0,
		MismatchesCount:  0,
		Mismatches:       0,
	}, nil
}

func TestShadow_Start(t *testing.T) {
	port := &fakeShadowPort{}
	uc := shadow.New(port)

	out, err := uc.Run(context.Background(), shadow.Input{
		UnitID:     "unit-payment",
		Action:     "start",
		Port:       9090,
		SampleRate: 0.25,
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "unit-payment", out.UnitID)
	assert.Equal(t, "start", out.Action)
	assert.True(t, out.Active)
	assert.Equal(t, 9090, out.Port)
	assert.Equal(t, 0.25, out.SampleRate)

	// Validate JSON schema
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("shadow.output.schema.json", data)
	require.NoError(t, err, "shadow output JSON must satisfy v2 schema: %s", string(data))
}

func TestShadow_Status(t *testing.T) {
	port := &fakeShadowPort{}
	uc := shadow.New(port)

	out, err := uc.Run(context.Background(), shadow.Input{
		UnitID: "unit-payment",
		Action: "status",
	})
	require.NoError(t, err)
	assert.Equal(t, "status", out.Action)
	assert.True(t, out.Active)
	assert.Equal(t, int64(100), out.RequestsTotal)
	assert.Equal(t, int64(50), out.RequestsShadowed)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("shadow.output.schema.json", data)
	require.NoError(t, err)
}

func TestShadow_Stop(t *testing.T) {
	port := &fakeShadowPort{}
	uc := shadow.New(port)

	out, err := uc.Run(context.Background(), shadow.Input{
		UnitID: "unit-payment",
		Action: "stop",
	})
	require.NoError(t, err)
	assert.Equal(t, "stop", out.Action)
	assert.False(t, out.Active)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("shadow.output.schema.json", data)
	require.NoError(t, err)
}

func TestShadow_ValidationErrors(t *testing.T) {
	uc := shadow.New(&fakeShadowPort{})

	_, err := uc.Run(context.Background(), shadow.Input{})
	assert.ErrorContains(t, err, "unit ID is required")

	_, err = uc.Run(context.Background(), shadow.Input{
		UnitID: "u1",
		Action: "restart",
	})
	assert.ErrorContains(t, err, "invalid shadow action")
}
