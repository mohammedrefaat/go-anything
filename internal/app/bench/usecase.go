package bench

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"time"
)

// UseCase coordinates comparative latency benchmarking, memory profiling, and speedup analysis.
type UseCase struct {
	port BenchPort
}

// New creates a new benchmark UseCase.
func New(port BenchPort) *UseCase {
	if port == nil {
		port = NewDefaultBenchPort()
	}
	return &UseCase{port: port}
}

// Run executes the comparative benchmarking pipeline.
func (uc *UseCase) Run(ctx context.Context, in Input) (*Output, error) {
	if in.UnitID == "" {
		return nil, fmt.Errorf("unit ID is required")
	}

	dir := in.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}

	iterations := in.Iterations
	if iterations <= 0 {
		iterations = 100
	}
	warmup := in.Warmup
	if warmup <= 0 {
		warmup = 10
	}

	cases, err := uc.port.LoadWorkloadCases(ctx, absDir, in.UnitID)
	if err != nil {
		return nil, fmt.Errorf("failed to load benchmark workload cases: %w", err)
	}

	req := BenchExecutionRequest{
		UnitID:     in.UnitID,
		Iterations: iterations,
		Warmup:     warmup,
		Target:     in.Target,
		Dir:        absDir,
		Cases:      cases,
	}

	res, err := uc.port.ExecuteBenchmark(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("benchmark execution failed: %w", err)
	}

	// Calculate speedup ratio: Original p50 / Go p50 (or mean if p50 == 0)
	speedupRatio := 1.0
	if res.Go.P50NS > 0 && res.Original.P50NS > 0 {
		speedupRatio = res.Original.P50NS / res.Go.P50NS
	} else if res.Go.MeanNS > 0 && res.Original.MeanNS > 0 {
		speedupRatio = res.Original.MeanNS / res.Go.MeanNS
	}
	// Round to 2 decimal places
	speedupRatio = math.Round(speedupRatio*100) / 100

	// Rule: Always attach Environment and Iterations to speedup ratio for reproducibility
	summary := fmt.Sprintf("Go is %.2fx faster than original (p50: %.0fns vs %.0fns) [%d iterations, %d warmup | %s, %s]",
		speedupRatio, res.Go.P50NS, res.Original.P50NS, iterations, warmup, res.Environment.CPUModel, res.Environment.OS)

	timestamp := time.Now().UTC().Format(time.RFC3339)

	out := &Output{
		SchemaVersion: "v2",
		UnitID:        in.UnitID,
		Original:      res.Original,
		Go:            res.Go,
		SpeedupRatio:  speedupRatio,
		Iterations:    iterations,
		Warmup:        warmup,
		Environment:   res.Environment,
		Summary:       summary,
		Timestamp:     timestamp,
	}

	// Persist report under .go-anything/reports/bench_<unit>.json
	_ = uc.port.SaveReport(ctx, absDir, in.UnitID, out)

	return out, nil
}

// Run is a convenience function.
func Run(ctx context.Context, in Input, port ...BenchPort) (*Output, error) {
	var p BenchPort
	if len(port) > 0 {
		p = port[0]
	}
	return New(p).Run(ctx, in)
}
