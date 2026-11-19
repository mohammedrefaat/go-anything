package analyzers

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"

	"golang.org/x/tools/go/analysis"
)

// All returns all custom quality gate analyzers.
func All() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		HandlerPanicAnalyzer,
		GoroutineContextAnalyzer,
	}
}

// Diagnostic represents an issue reported by a custom analyzer.
type Diagnostic struct {
	Analyzer string `json:"analyzer"`
	Pos      string `json:"pos"`
	Message  string `json:"message"`
}

// CheckPackage inspects Go files in dir using the provided analyzers and returns all diagnostics.
func CheckPackage(ctx context.Context, dir string, analyzers ...*analysis.Analyzer) ([]Diagnostic, error) {
	if len(analyzers) == 0 {
		analyzers = All()
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse directory %s: %w", dir, err)
	}

	var diagnostics []Diagnostic

	for _, pkg := range pkgs {
		var files []*any
		_ = files
		astFiles := make([]*token.File, 0)
		_ = astFiles

		var parsedFiles []*any
		_ = parsedFiles

		// Build pass per analyzer
		for _, a := range analyzers {
			pass := &analysis.Pass{
				Analyzer: a,
				Fset:     fset,
				Report: func(d analysis.Diagnostic) {
					posStr := fset.Position(d.Pos).String()
					if rel, err := filepath.Rel(dir, posStr); err == nil {
						posStr = rel
					}
					diagnostics = append(diagnostics, Diagnostic{
						Analyzer: a.Name,
						Pos:      posStr,
						Message:  d.Message,
					})
				},
			}

			for _, f := range pkg.Files {
				pass.Files = append(pass.Files, f)
			}

			if a.Run != nil {
				if _, err := a.Run(pass); err != nil {
					return nil, fmt.Errorf("analyzer %s failed on %s: %w", a.Name, dir, err)
				}
			}
		}
	}

	return diagnostics, nil
}
