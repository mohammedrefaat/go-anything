package fuzz

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// UseCase coordinates differential fuzzing execution, divergence logging, and case promotion.
type UseCase struct {
	port FuzzPort
}

// New creates a new differential fuzz UseCase.
func New(port FuzzPort) *UseCase {
	if port == nil {
		port = NewDefaultFuzzPort()
	}
	return &UseCase{port: port}
}

// Run executes the differential fuzzing use-case.
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

	runs := in.Runs
	if runs <= 0 && in.Time <= 0 {
		runs = 100
	}

	seed := in.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	maxBytes := in.MaxInputBytes
	if maxBytes <= 0 {
		maxBytes = 4096
	}

	seedCases, err := uc.port.LoadSeedCases(ctx, absDir, in.UnitID)
	if err != nil {
		return nil, fmt.Errorf("failed to load seed cases for unit %q: %w", in.UnitID, err)
	}

	corpusPath := filepath.Join(absDir, ".go-anything", "cases", in.UnitID+".jsonl")

	start := time.Now()
	fuzzReq := FuzzExecutionRequest{
		UnitID:        in.UnitID,
		Time:          in.Time,
		Runs:          runs,
		Seed:          seed,
		MaxInputBytes: maxBytes,
		Dir:           absDir,
		Target:        in.Target,
		SeedCases:     seedCases,
	}

	result, err := uc.port.ExecuteFuzz(ctx, fuzzReq)
	if err != nil {
		return nil, fmt.Errorf("differential fuzzing failed: %w", err)
	}
	durationMS := float64(time.Since(start).Milliseconds())

	promotedCount := 0
	var promotedCases []string
	if in.PromoteFindings && len(result.DivergentCases) > 0 {
		if err := uc.port.PromoteCases(ctx, absDir, in.UnitID, result.DivergentCases); err != nil {
			return nil, fmt.Errorf("failed to promote divergent cases: %w", err)
		}
		promotedCount = len(result.DivergentCases)
		for _, c := range result.DivergentCases {
			promotedCases = append(promotedCases, c.CaseID)
		}
	}
	if promotedCases == nil {
		promotedCases = []string{}
	}

	divergences := result.Divergences
	if divergences == nil {
		divergences = []DivergenceRecord{}
	}

	passed := result.DivergencesCount == 0

	summary := fmt.Sprintf("PASS: differential fuzzing completed with 0 divergences across %d runs", result.TotalRuns)
	if result.DivergencesCount > 0 {
		summary = fmt.Sprintf("FAIL: detected %d divergence(s) across %d runs (%d promoted to corpus)",
			result.DivergencesCount, result.TotalRuns, promotedCount)
	}

	output := &Output{
		SchemaVersion:    "v2",
		UnitID:           in.UnitID,
		DurationMS:       durationMS,
		TotalRuns:        result.TotalRuns,
		DivergencesCount: result.DivergencesCount,
		Divergences:      divergences,
		PromotedCount:    promotedCount,
		PromotedCases:    promotedCases,
		CorpusPath:       corpusPath,
		Passed:           passed,
		Summary:          summary,
	}

	// Persist fuzz report
	_ = uc.port.SaveReport(ctx, absDir, in.UnitID, output)

	return output, nil
}

// Run is a convenience function.
func Run(ctx context.Context, in Input, port ...FuzzPort) (*Output, error) {
	var p FuzzPort
	if len(port) > 0 {
		p = port[0]
	}
	return New(p).Run(ctx, in)
}
