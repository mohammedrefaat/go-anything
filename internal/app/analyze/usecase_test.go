package analyze_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/analyzer"
	"github.com/mohammedrefaat/go-anything/internal/app/analyze"
	"github.com/mohammedrefaat/go-anything/internal/planner"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAnalyzerPort is a fake implementation of analyze.AnalyzerPort for testing.
type fakeAnalyzerPort struct {
	analyzeFunc func(ctx context.Context, dir, source string) (*analyzer.Result, error)
	planFunc    func(ctx context.Context, res *analyzer.Result) (*planner.PlanResult, error)
}

func (f *fakeAnalyzerPort) Analyze(ctx context.Context, dir, source string) (*analyzer.Result, error) {
	if f.analyzeFunc != nil {
		return f.analyzeFunc(ctx, dir, source)
	}
	return &analyzer.Result{
		ModulePath:    "example.com/testmod",
		RootDirectory: dir,
		Packages: map[string]*analyzer.PackageInfo{
			"example.com/testmod/pkg/a": {
				ID:      "pkg-a",
				Name:    "a",
				PkgPath: "example.com/testmod/pkg/a",
			},
			"example.com/testmod/pkg/b": {
				ID:      "pkg-b",
				Name:    "b",
				PkgPath: "example.com/testmod/pkg/b",
			},
		},
		InternalGraph: map[string][]string{
			"example.com/testmod/pkg/b": {"example.com/testmod/pkg/a"},
			"example.com/testmod/pkg/a": {},
		},
	}, nil
}

func (f *fakeAnalyzerPort) Plan(ctx context.Context, res *analyzer.Result) (*planner.PlanResult, error) {
	if f.planFunc != nil {
		return f.planFunc(ctx, res)
	}
	unitA := &adapterkit.Unit{
		ID:           "pkg-a",
		Name:         "a",
		State:        adapterkit.StatePlanned,
		Wave:         0,
		Dependencies: []string{},
	}
	unitB := &adapterkit.Unit{
		ID:           "pkg-b",
		Name:         "b",
		State:        adapterkit.StatePlanned,
		Wave:         1,
		Dependencies: []string{"pkg-a"},
	}
	return &planner.PlanResult{
		TotalUnits: 2,
		MaxWave:    1,
		Waves: map[int][]*adapterkit.Unit{
			0: {unitA},
			1: {unitB},
		},
		Units:  []*adapterkit.Unit{unitA, unitB},
		Cycles: nil,
	}, nil
}

func TestAnalyze_Success(t *testing.T) {
	fakePort := &fakeAnalyzerPort{}
	uc := analyze.New(fakePort)

	out, err := uc.Run(context.Background(), analyze.Input{
		Dir:    ".",
		Source: "go",
		JSON:   true,
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "example.com/testmod", out.Project)
	assert.Equal(t, "go", out.SourceLanguage)
	assert.Len(t, out.Units, 2)
	assert.Len(t, out.Waves, 2)
	assert.Equal(t, 0, out.Waves[0].Wave)
	assert.Equal(t, []string{"pkg-a"}, out.Waves[0].UnitIDs)
	assert.Equal(t, 1, out.Waves[1].Wave)
	assert.Equal(t, []string{"pkg-b"}, out.Waves[1].UnitIDs)
	assert.Empty(t, out.Cycles)
	assert.Len(t, out.Graph, 2)

	// Validate against schemas/v2/analyze.output.schema.json
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("analyze.output.schema.json", data)
	require.NoError(t, err, "output JSON must satisfy v2 schema: %s", string(data))
}

func TestAnalyze_ConvenienceRun(t *testing.T) {
	fakePort := &fakeAnalyzerPort{}
	out, err := analyze.Run(context.Background(), analyze.Input{Dir: "."}, fakePort)
	require.NoError(t, err)
	assert.Equal(t, "example.com/testmod", out.Project)
}

func TestAnalyze_AnalyzerError(t *testing.T) {
	fakePort := &fakeAnalyzerPort{
		analyzeFunc: func(ctx context.Context, dir, source string) (*analyzer.Result, error) {
			return nil, errors.New("failed to load package files")
		},
	}
	uc := analyze.New(fakePort)
	_, err := uc.Run(context.Background(), analyze.Input{Dir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "analysis failed")
}

func TestAnalyze_PlannerError(t *testing.T) {
	fakePort := &fakeAnalyzerPort{
		planFunc: func(ctx context.Context, res *analyzer.Result) (*planner.PlanResult, error) {
			return nil, errors.New("cycle resolution overflow")
		},
	}
	uc := analyze.New(fakePort)
	_, err := uc.Run(context.Background(), analyze.Input{Dir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "planning failed")
}

func TestAnalyze_WithCycles(t *testing.T) {
	fakePort := &fakeAnalyzerPort{
		planFunc: func(ctx context.Context, res *analyzer.Result) (*planner.PlanResult, error) {
			return &planner.PlanResult{
				TotalUnits: 2,
				MaxWave:    0,
				Waves: map[int][]*adapterkit.Unit{
					0: {
						{ID: "cycle-a", State: adapterkit.StatePlanned},
						{ID: "cycle-b", State: adapterkit.StatePlanned},
					},
				},
				Units: []*adapterkit.Unit{
					{ID: "cycle-a", State: adapterkit.StatePlanned},
					{ID: "cycle-b", State: adapterkit.StatePlanned},
				},
				Cycles: [][]string{{"cycle-a", "cycle-b"}},
			}, nil
		},
	}
	uc := analyze.New(fakePort)
	out, err := uc.Run(context.Background(), analyze.Input{Dir: "."})
	require.NoError(t, err)
	assert.Len(t, out.Cycles, 1)
	assert.Equal(t, []string{"cycle-a", "cycle-b"}, out.Cycles[0])

	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("analyze.output.schema.json", data)
	require.NoError(t, err)
}
