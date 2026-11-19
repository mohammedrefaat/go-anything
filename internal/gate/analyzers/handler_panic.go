package analyzers

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// HandlerPanicAnalyzer detects direct calls to panic() inside HTTP handlers without recover().
var HandlerPanicAnalyzer = &analysis.Analyzer{
	Name: "handlerpanic",
	Doc:  "detects direct calls to panic() inside HTTP handlers without an immediate recover()",
	Run:  runHandlerPanic,
}

func runHandlerPanic(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if isHTTPHandler(fn.Type, fn.Name.Name) {
					checkHandlerBody(pass, fn.Body)
				}
			case *ast.FuncLit:
				if isHTTPHandler(fn.Type, "") {
					checkHandlerBody(pass, fn.Body)
				}
			}
			return true
		})
	}
	return nil, nil
}

// isHTTPHandler returns true if fnType matches standard HTTP handler signatures.
func isHTTPHandler(fnType *ast.FuncType, fnName string) bool {
	if fnName == "ServeHTTP" {
		return true
	}

	if fnType == nil || fnType.Params == nil {
		return false
	}

	params := fnType.Params.List
	if len(params) == 2 {
		p1 := typeString(params[0].Type)
		p2 := typeString(params[1].Type)

		isW := p1 == "http.ResponseWriter" || p1 == "ResponseWriter"
		isR := p2 == "*http.Request" || p2 == "*Request"

		if isW && isR {
			return true
		}
	} else if len(params) == 1 && len(params[0].Names) == 2 {
		// e.g. func(w http.ResponseWriter, r *http.Request) grouped in one Field
		p := typeString(params[0].Type)
		if p == "http.ResponseWriter" || p == "ResponseWriter" {
			return true
		}
	}

	return false
}

func checkHandlerBody(pass *analysis.Pass, body *ast.BlockStmt) {
	if body == nil {
		return
	}

	// 1. Check if there is an active recover in a defer statement at handler level
	hasRecovery := false
	for _, stmt := range body.List {
		if def, ok := stmt.(*ast.DeferStmt); ok {
			if containsRecover(def.Call) {
				hasRecovery = true
				break
			}
		}
	}

	if hasRecovery {
		return
	}

	// 2. Look for direct panic calls inside body (stop at nested function literals)
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}

		// Don't descend into nested function literals (they have their own scope)
		if _, isLit := n.(*ast.FuncLit); isLit && n != body {
			return false
		}

		if call, ok := n.(*ast.CallExpr); ok {
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
				if isBuiltinPanic(pass.TypesInfo, ident) {
					pass.Reportf(call.Pos(), "direct call to panic() inside HTTP handler without recover()")
				}
			}
		}

		return true
	})
}

func containsRecover(call *ast.CallExpr) bool {
	if call == nil {
		return false
	}

	found := false
	ast.Inspect(call, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "recover" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isBuiltinPanic(info *types.Info, ident *ast.Ident) bool {
	if info == nil {
		return true
	}
	obj := info.Uses[ident]
	if obj == nil {
		return ident.Name == "panic"
	}
	_, isBuiltin := obj.(*types.Builtin)
	return isBuiltin && obj.Name() == "panic"
}

func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	default:
		return ""
	}
}
