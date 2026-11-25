package replay

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/comparator"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"golang.org/x/sync/errgroup"
)

// Option configures the replay Engine.
type Option func(*Engine)

// WithJobs configures the maximum concurrent workers.
func WithJobs(jobs int) Option {
	return func(e *Engine) {
		if jobs > 0 {
			e.jobs = jobs
		}
	}
}

// WithRules passes wildcard and normalizer rules to the comparator.
func WithRules(rules *adapterkit.Rules) Option {
	return func(e *Engine) {
		e.rules = rules
	}
}

// WithMode configures the default comparison mode.
func WithMode(mode string) Option {
	return func(e *Engine) {
		if mode != "" {
			e.mode = mode
		}
	}
}

// WithTimeout configures the per-case execution timeout.
func WithTimeout(d time.Duration) Option {
	return func(e *Engine) {
		if d > 0 {
			e.timeout = d
		}
	}
}

// Engine coordinates concurrent, deterministic replay of test cases.
type Engine struct {
	jobs    int
	rules   *adapterkit.Rules
	mode    string
	timeout time.Duration
}

// New returns a configured replay Engine.
func New(opts ...Option) *Engine {
	e := &Engine{
		jobs:    runtime.NumCPU(),
		mode:    comparator.ModeHTTPContract,
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ReplayCases executes recorded test cases concurrently using errgroup with SetLimit.
// Results are strictly sorted by case_id before return (Rule 2).
func (e *Engine) ReplayCases(ctx context.Context, unitID string, cases []*adapterkit.Case, driver adapterkit.Replay) (*ReplayReport, error) {
	if len(cases) == 0 {
		return &ReplayReport{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			UnitID:        unitID,
			TotalCases:    0,
			PassedCases:   0,
			FailedCases:   0,
			Results:       []CaseResult{},
			Summary:       "PASS: no cases to evaluate",
		}, nil
	}

	comp := comparator.New(comparator.WithRules(e.rules))

	var g errgroup.Group
	g.SetLimit(e.jobs)

	var mu sync.Mutex
	results := make([]CaseResult, 0, len(cases))

	for _, c := range cases {
		c := c
		g.Go(func() error {
			res := e.executeSingleCase(ctx, comp, driver, c)
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()

	// Mandatory Rule 2: Strictly sort all results by case_id, NEVER by completion time!
	sort.Slice(results, func(i, j int) bool {
		return results[i].CaseID < results[j].CaseID
	})

	passedCount := 0
	failedCount := 0
	for _, r := range results {
		if r.Passed {
			passedCount++
		} else {
			failedCount++
		}
	}

	summary := "PASS: all cases matched recorded baseline behavior"
	if failedCount > 0 {
		summary = fmt.Sprintf("FAIL: %d/%d cases failed verification", failedCount, len(cases))
	}

	return &ReplayReport{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		UnitID:        unitID,
		TotalCases:    len(cases),
		PassedCases:   passedCount,
		FailedCases:   failedCount,
		Results:       results,
		Summary:       summary,
	}, nil
}

func (e *Engine) executeSingleCase(ctx context.Context, comp *comparator.Comparator, driver adapterkit.Replay, c *adapterkit.Case) CaseResult {
	start := time.Now()

	// Ephemeral isolated working directory per case
	tempDir, err := os.MkdirTemp("", "ga-replay-"+c.CaseID+"-*")
	if err == nil {
		defer os.RemoveAll(tempDir)
	}

	caseCtx := ctx
	if e.timeout > 0 {
		var cancel context.CancelFunc
		caseCtx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	actOutput, err := driver.ExecuteCase(caseCtx, c)
	duration := float64(time.Since(start).Microseconds()) / 1000.0

	if err != nil {
		return CaseResult{
			CaseID:     c.CaseID,
			UnitID:     c.UnitID,
			Kind:       c.Kind,
			Passed:     false,
			DurationMS: duration,
			Error:      fmt.Sprintf("replay driver execution failed: %v", err),
		}
	}

	compMode := e.mode
	if c.Kind == adapterkit.KindTrap && c.TrapType != "" {
		compMode = comparator.ModeStructuralJSON
	}

	cmpResult, err := comp.CompareCaseOutput(compMode, &c.ExpectedOutput, actOutput)
	if err != nil {
		return CaseResult{
			CaseID:     c.CaseID,
			UnitID:     c.UnitID,
			Kind:       c.Kind,
			Passed:     false,
			DurationMS: duration,
			Error:      fmt.Sprintf("comparator error: %v", err),
		}
	}

	return CaseResult{
		CaseID:     c.CaseID,
		UnitID:     c.UnitID,
		Kind:       c.Kind,
		Passed:     cmpResult.Passed,
		DurationMS: duration,
		Mismatches: cmpResult.Mismatches,
	}
}

// ServerFactory creates an ephemeral server instance bound to an isolated port and directory.
type ServerFactory func(ctx context.Context, port int, workDir string) (adapterkit.Replay, error)

// ReplaySequences executes stateful HTTP sequences.
// Steps within a sequence are executed serially against a fresh server instance.
// Independent sequences are executed concurrently in parallel up to jobs.
func (e *Engine) ReplaySequences(ctx context.Context, unitID string, sequences [][]*adapterkit.Case, factory ServerFactory) (*ReplayReport, error) {
	totalCases := 0
	for _, seq := range sequences {
		totalCases += len(seq)
	}

	if totalCases == 0 {
		return &ReplayReport{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			UnitID:        unitID,
			TotalCases:    0,
			PassedCases:   0,
			FailedCases:   0,
			Results:       []CaseResult{},
			Summary:       "PASS: no sequence cases to evaluate",
		}, nil
	}

	comp := comparator.New(comparator.WithRules(e.rules))

	var g errgroup.Group
	g.SetLimit(e.jobs)

	var mu sync.Mutex
	results := make([]CaseResult, 0, totalCases)

	for seqIdx, seq := range sequences {
		seq := seq
		seqIdx := seqIdx
		g.Go(func() error {
			// Ephemeral port and directory per stateful sequence
			port, err := GetFreePort()
			if err != nil {
				mu.Lock()
				for _, c := range seq {
					results = append(results, CaseResult{
						CaseID: c.CaseID,
						UnitID: c.UnitID,
						Kind:   c.Kind,
						Passed: false,
						Error:  fmt.Sprintf("failed to allocate port: %v", err),
					})
				}
				mu.Unlock()
				return nil
			}

			seqDir, err := os.MkdirTemp("", fmt.Sprintf("ga-seq-%d-*", seqIdx))
			if err == nil {
				defer os.RemoveAll(seqDir)
			}

			driver, err := factory(ctx, port, seqDir)
			if err != nil {
				mu.Lock()
				for _, c := range seq {
					results = append(results, CaseResult{
						CaseID: c.CaseID,
						UnitID: c.UnitID,
						Kind:   c.Kind,
						Passed: false,
						Error:  fmt.Sprintf("failed to start sequence server: %v", err),
					})
				}
				mu.Unlock()
				return nil
			}
			defer driver.Close()

			// Serial execution within sequence
			for _, c := range seq {
				res := e.executeSingleCase(ctx, comp, driver, c)
				mu.Lock()
				results = append(results, res)
				mu.Unlock()
			}

			return nil
		})
	}

	_ = g.Wait()

	// Mandatory Rule 2: Deterministic sorting by case_id!
	sort.Slice(results, func(i, j int) bool {
		return results[i].CaseID < results[j].CaseID
	})

	passedCount := 0
	failedCount := 0
	for _, r := range results {
		if r.Passed {
			passedCount++
		} else {
			failedCount++
		}
	}

	summary := "PASS: all sequence cases matched"
	if failedCount > 0 {
		summary = fmt.Sprintf("FAIL: %d/%d sequence cases failed", failedCount, totalCases)
	}

	return &ReplayReport{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		UnitID:        unitID,
		TotalCases:    totalCases,
		PassedCases:   passedCount,
		FailedCases:   failedCount,
		Results:       results,
		Summary:       summary,
	}, nil
}
