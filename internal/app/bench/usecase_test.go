package bench_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/app/bench"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBenchPort is a fake implementation of bench.BenchPort for testing.
type fakeBenchPort struct {
	getEnvironmentFunc    func(ctx context.Context, dir string, unitID string) (bench.EnvironmentInfo, error)
	loadWorkloadCasesFunc func(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error)
	executeBenchmarkFunc  func(ctx context.Context, req bench.BenchExecutionRequest) (*bench.BenchExecutionResult, error)
	saveReportFunc        func(ctx context.Context, dir, unitID string, out *bench.Output) error

	savedReport *bench.Output
}

func (f *fakeBenchPort) GetEnvironment(ctx context.Context, dir string, unitID string) (bench.EnvironmentInfo, error) {
	if f.getEnvironmentFunc != nil {
		return f.getEnvironmentFunc(ctx, dir, unitID)
	}
	return bench.EnvironmentInfo{
		CPUModel:        "AMD Ryzen 9 5900X 12-Core Processor",
		OS:              "windows/amd64",
		GoVersion:       "go1.26.0",
		OriginalRuntime: "Python 3.12.2",
		Cores:           24,
	}, nil
}

func (f *fakeBenchPort) LoadWorkloadCases(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error) {
	if f.loadWorkloadCasesFunc != nil {
		return f.loadWorkloadCasesFunc(ctx, dir, unitID)
	}
	return []*adapterkit.Case{
		{
			CaseID: "workload-01",
			UnitID: unitID,
			Kind:   adapterkit.KindHappy,
			Input: adapterkit.CaseInput{
				Argv: []string{unitID, "run"},
			},
		},
	}, nil
}

func (f *fakeBenchPort) ExecuteBenchmark(ctx context.Context, req bench.BenchExecutionRequest) (*bench.BenchExecutionResult, error) {
	if f.executeBenchmarkFunc != nil {
		return f.executeBenchmarkFunc(ctx, req)
	}
	env, _ := f.GetEnvironment(ctx, req.Dir, req.UnitID)
	return &bench.BenchExecutionResult{
		Original: bench.LatencyPercentiles{
			P50NS:            50000,
			P95NS:            75000,
			P99NS:            90000,
			ThroughputOpsSec: 20000,
			MeanNS:           52000,
		},
		Go: bench.GoBenchmarkMetrics{
			P50NS:            20000,
			P95NS:            30000,
			P99NS:            35000,
			ThroughputOpsSec: 50000,
			MeanNS:           21000,
			AllocsPerOp:      3,
			BytesPerOp:       256,
		},
		Environment: env,
	}, nil
}

func (f *fakeBenchPort) SaveReport(ctx context.Context, dir, unitID string, out *bench.Output) error {
	f.savedReport = out
	if f.saveReportFunc != nil {
		return f.saveReportFunc(ctx, dir, unitID, out)
	}
	return nil
}

func TestBench_Success(t *testing.T) {
	port := &fakeBenchPort{}
	uc := bench.New(port)

	out, err := uc.Run(context.Background(), bench.Input{
		UnitID:     "unit-crypto",
		Iterations: 100,
		Warmup:     10,
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, "unit-crypto", out.UnitID)
	assert.Equal(t, 100, out.Iterations)
	assert.Equal(t, 10, out.Warmup)
	assert.Equal(t, 2.5, out.SpeedupRatio) // 50000 / 20000 = 2.5x

	// Verify Go metrics
	assert.Equal(t, 20000.0, out.Go.P50NS)
	assert.Equal(t, uint64(3), out.Go.AllocsPerOp)
	assert.Equal(t, uint64(256), out.Go.BytesPerOp)

	// Verify Original metrics
	assert.Equal(t, 50000.0, out.Original.P50NS)

	// Verify Environment attached (Mandatory Rule)
	assert.Equal(t, "AMD Ryzen 9 5900X 12-Core Processor", out.Environment.CPUModel)
	assert.Equal(t, "windows/amd64", out.Environment.OS)
	assert.Equal(t, 24, out.Environment.Cores)
	assert.NotEmpty(t, out.Timestamp)
	assert.Contains(t, out.Summary, "2.50x faster")
	assert.Contains(t, out.Summary, "100 iterations")

	// Validate against schemas/v2/bench.report.schema.json
	data, err := json.Marshal(out)
	require.NoError(t, err)
	err = schemas.ValidateV2("bench.report.schema.json", data)
	require.NoError(t, err, "bench output JSON must satisfy v2 schema: %s", string(data))
}

func TestBench_ValidationErrors(t *testing.T) {
	uc := bench.New(&fakeBenchPort{})

	_, err := uc.Run(context.Background(), bench.Input{})
	assert.ErrorContains(t, err, "unit ID is required")
}

func TestBench_DefaultValues(t *testing.T) {
	var capturedReq bench.BenchExecutionRequest
	port := &fakeBenchPort{
		executeBenchmarkFunc: func(ctx context.Context, req bench.BenchExecutionRequest) (*bench.BenchExecutionResult, error) {
			capturedReq = req
			return &bench.BenchExecutionResult{
				Original: bench.LatencyPercentiles{P50NS: 100, P95NS: 200, P99NS: 300, ThroughputOpsSec: 10, MeanNS: 150},
				Go:       bench.GoBenchmarkMetrics{P50NS: 50, P95NS: 100, P99NS: 150, ThroughputOpsSec: 20, MeanNS: 75, AllocsPerOp: 1, BytesPerOp: 64},
				Environment: bench.EnvironmentInfo{
					CPUModel:        "Test CPU",
					OS:              "linux/amd64",
					GoVersion:       "go1.26.0",
					OriginalRuntime: "go",
					Cores:           4,
				},
			}, nil
		},
	}
	uc := bench.New(port)

	out, err := uc.Run(context.Background(), bench.Input{
		UnitID: "unit-auth",
	})
	require.NoError(t, err)
	assert.Equal(t, 100, capturedReq.Iterations) // default 100
	assert.Equal(t, 10, capturedReq.Warmup)      // default 10
	assert.Equal(t, 2.0, out.SpeedupRatio)
}
