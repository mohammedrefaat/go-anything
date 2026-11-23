package analyze

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mohammedrefat/go-anything/internal/analyzer"
	"github.com/mohammedrefat/go-anything/internal/planner"
)

// AnalyzerPort abstracts the static analysis and dependency planning capabilities.
type AnalyzerPort interface {
	Analyze(ctx context.Context, dir, source string) (*analyzer.Result, error)
	Plan(ctx context.Context, res *analyzer.Result) (*planner.PlanResult, error)
}

// DefaultAnalyzerPort implements AnalyzerPort using internal/analyzer and internal/planner.
type DefaultAnalyzerPort struct {
	analyzer *analyzer.Analyzer
	planner  *planner.Planner
}

// NewDefaultAnalyzerPort creates a new DefaultAnalyzerPort.
func NewDefaultAnalyzerPort() *DefaultAnalyzerPort {
	return &DefaultAnalyzerPort{
		analyzer: analyzer.New(),
		planner:  planner.New(),
	}
}

// Analyze runs Go static analysis on the directory, automatically resolving
// modern/ or legacy/ subdirectories if running against a three-tier workspace.
func (p *DefaultAnalyzerPort) Analyze(ctx context.Context, dir, source string) (*analyzer.Result, error) {
	targetDir := dir
	if fi, err := os.Stat(filepath.Join(dir, "modern")); err == nil && fi.IsDir() {
		targetDir = filepath.Join(dir, "modern")
	} else if fi, err := os.Stat(filepath.Join(dir, "legacy")); err == nil && fi.IsDir() {
		targetDir = filepath.Join(dir, "legacy")
	}
	return p.analyzer.Analyze(ctx, targetDir, "./...")
}

// Plan computes topological migration waves from the static analysis result.
func (p *DefaultAnalyzerPort) Plan(ctx context.Context, res *analyzer.Result) (*planner.PlanResult, error) {
	return p.planner.PlanFromAnalyzerResult(res)
}
