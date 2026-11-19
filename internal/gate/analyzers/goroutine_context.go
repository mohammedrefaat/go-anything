package analyzers

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// GoroutineContextAnalyzer detects uncoordinated goroutines launched without context.Context or tracking.
var GoroutineContextAnalyzer = &analysis.Analyzer{
	Name: "goroutinecontext",
	Doc:  "detects uncoordinated 'go func()' goroutines launched without accepting a context.Context or without tracking",
	Run:  runGoroutineContext,
}

func runGoroutineContext(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			goStmt, ok := n.(*ast.GoStmt)
			if !ok {
				return true
			}

			call := goStmt.Call
			if call == nil {
				return true
			}

			// 1. Check if any argument passed to the goroutine is a context
			for _, arg := range call.Args {
				if isContextExpr(arg) {
					return true // Coordinated via context passed as argument
				}
			}

			// 2. If it's a function literal: go func(...) { ... }(...)
			if lit, ok := call.Fun.(*ast.FuncLit); ok {
				// Check if func literal parameters accept context
				if lit.Type != nil && lit.Type.Params != nil {
					for _, param := range lit.Type.Params.List {
						if isContextType(param.Type) {
							return true
						}
					}
				}

				// Check if body uses coordination: waitgroup, errgroup, tracker, or context
				if hasCoordinationInBody(lit.Body) {
					return true
				}

				pass.Reportf(goStmt.Pos(), "goroutine launched without context.Context or coordination tracker")
				return true
			}

			// 3. If calling a named function without context arguments
			if ident, ok := call.Fun.(*ast.Ident); ok {
				if strings.Contains(strings.ToLower(ident.Name), "worker") || strings.Contains(strings.ToLower(ident.Name), "tracker") {
					return true
				}
				pass.Reportf(goStmt.Pos(), "goroutine launched without context.Context or coordination tracker")
			}

			return true
		})
	}
	return nil, nil
}

func isContextType(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "Context"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Context"
	default:
		return false
	}
}

func isContextExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return strings.Contains(strings.ToLower(e.Name), "ctx") || e.Name == "Context"
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if sel.Sel.Name == "Background" || sel.Sel.Name == "TODO" || sel.Sel.Name == "WithCancel" || sel.Sel.Name == "WithTimeout" {
				return true
			}
		}
	}
	return false
}

func hasCoordinationInBody(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}

	coordinated := false
	ast.Inspect(body, func(n ast.Node) bool {
		if coordinated {
			return false
		}

		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				name := strings.ToLower(sel.Sel.Name)
				if name == "done" || name == "add" || name == "wait" || name == "go" {
					coordinated = true
					return false
				}
			}
		case *ast.Ident:
			if strings.Contains(strings.ToLower(x.Name), "ctx") {
				coordinated = true
				return false
			}
		}
		return true
	})

	return coordinated
}
