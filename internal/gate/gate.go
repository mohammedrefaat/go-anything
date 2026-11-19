// Package gate executes the quality gate pipeline including formatting,
// linting, data race detection, and vulnerability checks.
package gate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Runner coordinates quality gate verification checks across the toolchain.
type Runner struct {
	cfg       Config
	executors map[string]CheckExecutor
}

// New creates a new quality gate Runner with the given configuration.
func New(configs ...Config) *Runner {
	var cfg Config
	if len(configs) > 0 {
		cfg = configs[0]
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if len(cfg.Checks) == 0 {
		cfg.Checks = AllChecks()
	}

	executors := DefaultExecutors(cfg.AllowMissingTools)
	for k, v := range cfg.Executors {
		executors[k] = v
	}

	return &Runner{
		cfg:       cfg,
		executors: executors,
	}
}

// Run executes all configured quality gate checks and returns an aggregated GateReport.
func (r *Runner) Run(ctx context.Context) (*GateReport, error) {
	start := time.Now()
	report := &GateReport{
		Passed: true,
	}

	for _, checkName := range r.cfg.Checks {
		checkReport, err := r.RunCheck(ctx, checkName)
		if err != nil {
			return nil, fmt.Errorf("gate check %s failed to execute: %w", checkName, err)
		}

		report.Checks = append(report.Checks, *checkReport)
		switch checkReport.Status {
		case StatusPass:
			report.PassedCount++
		case StatusFail:
			report.FailedCount++
			report.Passed = false
		case StatusSkipped:
			report.SkippedCount++
		}
	}

	report.Duration = time.Since(start)
	report.DurationMS = float64(report.Duration.Milliseconds())
	return report, nil
}

// RunCheck executes a single gate check by name.
func (r *Runner) RunCheck(ctx context.Context, checkName string) (*CheckReport, error) {
	executor, ok := r.executors[checkName]
	if !ok {
		return nil, fmt.Errorf("unknown quality gate check: %s", checkName)
	}

	checkCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	start := time.Now()
	out, issues, passed, execErr := executor(checkCtx, r.cfg.Dir, r.cfg.Env)
	duration := time.Since(start)

	status := StatusPass
	if errors.Is(execErr, ErrSkipped) {
		status = StatusSkipped
		passed = false
	} else if !passed || execErr != nil {
		passed = false
		status = StatusFail
	}

	return &CheckReport{
		Name:       checkName,
		Passed:     passed,
		Status:     status,
		Duration:   duration,
		DurationMS: float64(duration.Milliseconds()),
		Output:     out,
		Issues:     issues,
	}, nil
}
