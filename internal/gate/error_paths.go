package gate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// ErrorPath describes an error condition or failing return path in a unit's source code.
type ErrorPath struct {
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	Kind       string `json:"kind"` // "errors.New", "fmt.Errorf", "http.Error", "status_code", "error_payload", "declared"
	Pattern    string `json:"pattern"`
	StatusCode int    `json:"status_code,omitempty"`
}

// ExtractErrorPathsFromDir inspects all non-test Go source files in dir and discovers error returns.
func ExtractErrorPathsFromDir(dir string) ([]ErrorPath, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	var errorPaths []ErrorPath
	seenPatterns := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", filePath, err)
		}

		filePaths := extractErrorPathsFromFile(fset, f, entry.Name())
		for _, ep := range filePaths {
			key := fmt.Sprintf("%s:%d:%s", ep.Kind, ep.StatusCode, ep.Pattern)
			if !seenPatterns[key] {
				seenPatterns[key] = true
				errorPaths = append(errorPaths, ep)
			}
		}
	}

	sort.Slice(errorPaths, func(i, j int) bool {
		if errorPaths[i].StatusCode != errorPaths[j].StatusCode {
			return errorPaths[i].StatusCode < errorPaths[j].StatusCode
		}
		return errorPaths[i].Pattern < errorPaths[j].Pattern
	})

	return errorPaths, nil
}

// ExtractErrorPathsFromSource extracts error paths from a Go source string.
func ExtractErrorPathsFromSource(filename, src string) ([]ErrorPath, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse source %s: %w", filename, err)
	}

	return extractErrorPathsFromFile(fset, f, filename), nil
}

func extractErrorPathsFromFile(fset *token.FileSet, f *ast.File, filename string) []ErrorPath {
	var paths []ErrorPath
	imports := make(map[string]string)
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name] = path
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		line := fset.Position(call.Pos()).Line

		// 1. Check for errors.New(...)
		if isPkgFuncCall(call, imports, "errors", "New") && len(call.Args) > 0 {
			if strVal, ok := extractStringLit(call.Args[0]); ok && strVal != "" {
				paths = append(paths, ErrorPath{
					File:    filename,
					Line:    line,
					Kind:    "errors.New",
					Pattern: strVal,
				})
			}
		}

		// 2. Check for fmt.Errorf(...)
		if isPkgFuncCall(call, imports, "fmt", "Errorf") && len(call.Args) > 0 {
			if strVal, ok := extractStringLit(call.Args[0]); ok && strVal != "" {
				prefix := extractFormatPrefix(strVal)
				if prefix != "" {
					paths = append(paths, ErrorPath{
						File:    filename,
						Line:    line,
						Kind:    "fmt.Errorf",
						Pattern: prefix,
					})
				}
			}
		}

		// 3. Check for http.Error(w, errStr, statusCode)
		if isPkgFuncCall(call, imports, "net/http", "Error") && len(call.Args) >= 3 {
			errStr, _ := extractStringLit(call.Args[1])
			statusCode := extractStatusCode(call.Args[2])
			paths = append(paths, ErrorPath{
				File:       filename,
				Line:       line,
				Kind:       "http.Error",
				Pattern:    errStr,
				StatusCode: statusCode,
			})
		}

		// 4. Check for w.WriteHeader(statusCode) where statusCode >= 400
		if isMethodCall(call, "WriteHeader") && len(call.Args) == 1 {
			statusCode := extractStatusCode(call.Args[0])
			if statusCode >= 400 {
				paths = append(paths, ErrorPath{
					File:       filename,
					Line:       line,
					Kind:       "status_code",
					StatusCode: statusCode,
				})
			}
		}

		// 5. Check for JSON error maps or responses, e.g. Encode(map[string]any{"error": "..."})
		if isMethodCall(call, "Encode") && len(call.Args) == 1 {
			if comp, ok := call.Args[0].(*ast.CompositeLit); ok {
				for _, elt := range comp.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						keyStr, _ := extractStringLit(kv.Key)
						if keyStr == "error" {
							valStr, ok := extractStringLit(kv.Value)
							if ok && valStr != "" {
								paths = append(paths, ErrorPath{
									File:    filename,
									Line:    line,
									Kind:    "error_payload",
									Pattern: extractFormatPrefix(valStr),
								})
							}
						}
					}
				}
			}
		}

		return true
	})

	return paths
}

func isPkgFuncCall(call *ast.CallExpr, imports map[string]string, pkg, fn string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkgIdent.Obj == nil && imports[pkgIdent.Name] == pkg && sel.Sel.Name == fn
}

func isMethodCall(call *ast.CallExpr, method string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == method
}

func extractStringLit(expr ast.Expr) (string, bool) {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		s, err := strconv.Unquote(lit.Value)
		if err == nil {
			return s, true
		}
		return strings.Trim(lit.Value, `"`), true
	}
	return "", false
}

func extractStatusCode(expr ast.Expr) int {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.INT {
			v, _ := strconv.Atoi(e.Value)
			return v
		}
	case *ast.SelectorExpr:
		switch e.Sel.Name {
		case "StatusContinue":
			return http.StatusContinue
		case "StatusSwitchingProtocols":
			return http.StatusSwitchingProtocols
		case "StatusProcessing":
			return http.StatusProcessing
		case "StatusEarlyHints":
			return http.StatusEarlyHints
		case "StatusOK":
			return http.StatusOK
		case "StatusCreated":
			return http.StatusCreated
		case "StatusAccepted":
			return http.StatusAccepted
		case "StatusNonAuthoritativeInfo":
			return http.StatusNonAuthoritativeInfo
		case "StatusNoContent":
			return http.StatusNoContent
		case "StatusResetContent":
			return http.StatusResetContent
		case "StatusPartialContent":
			return http.StatusPartialContent
		case "StatusMultiStatus":
			return http.StatusMultiStatus
		case "StatusAlreadyReported":
			return http.StatusAlreadyReported
		case "StatusIMUsed":
			return http.StatusIMUsed
		case "StatusMultipleChoices":
			return http.StatusMultipleChoices
		case "StatusMovedPermanently":
			return http.StatusMovedPermanently
		case "StatusFound":
			return http.StatusFound
		case "StatusSeeOther":
			return http.StatusSeeOther
		case "StatusNotModified":
			return http.StatusNotModified
		case "StatusUseProxy":
			return http.StatusUseProxy
		case "StatusTemporaryRedirect":
			return http.StatusTemporaryRedirect
		case "StatusPermanentRedirect":
			return http.StatusPermanentRedirect
		case "StatusBadRequest":
			return http.StatusBadRequest
		case "StatusUnauthorized":
			return http.StatusUnauthorized
		case "StatusPaymentRequired":
			return http.StatusPaymentRequired
		case "StatusForbidden":
			return http.StatusForbidden
		case "StatusNotFound":
			return http.StatusNotFound
		case "StatusMethodNotAllowed":
			return http.StatusMethodNotAllowed
		case "StatusNotAcceptable":
			return http.StatusNotAcceptable
		case "StatusProxyAuthRequired":
			return http.StatusProxyAuthRequired
		case "StatusRequestTimeout":
			return http.StatusRequestTimeout
		case "StatusConflict":
			return http.StatusConflict
		case "StatusGone":
			return http.StatusGone
		case "StatusLengthRequired":
			return http.StatusLengthRequired
		case "StatusPreconditionFailed":
			return http.StatusPreconditionFailed
		case "StatusRequestEntityTooLarge":
			return http.StatusRequestEntityTooLarge
		case "StatusRequestURITooLong":
			return http.StatusRequestURITooLong
		case "StatusUnsupportedMediaType":
			return http.StatusUnsupportedMediaType
		case "StatusRequestedRangeNotSatisfiable":
			return http.StatusRequestedRangeNotSatisfiable
		case "StatusExpectationFailed":
			return http.StatusExpectationFailed
		case "StatusTeapot":
			return http.StatusTeapot
		case "StatusMisdirectedRequest":
			return http.StatusMisdirectedRequest
		case "StatusUnprocessableEntity":
			return http.StatusUnprocessableEntity
		case "StatusLocked":
			return http.StatusLocked
		case "StatusFailedDependency":
			return http.StatusFailedDependency
		case "StatusTooEarly":
			return http.StatusTooEarly
		case "StatusUpgradeRequired":
			return http.StatusUpgradeRequired
		case "StatusPreconditionRequired":
			return http.StatusPreconditionRequired
		case "StatusTooManyRequests":
			return http.StatusTooManyRequests
		case "StatusRequestHeaderFieldsTooLarge":
			return http.StatusRequestHeaderFieldsTooLarge
		case "StatusUnavailableForLegalReasons":
			return http.StatusUnavailableForLegalReasons
		case "StatusInternalServerError":
			return http.StatusInternalServerError
		case "StatusNotImplemented":
			return http.StatusNotImplemented
		case "StatusBadGateway":
			return http.StatusBadGateway
		case "StatusServiceUnavailable":
			return http.StatusServiceUnavailable
		case "StatusGatewayTimeout":
			return http.StatusGatewayTimeout
		case "StatusHTTPVersionNotSupported":
			return http.StatusHTTPVersionNotSupported
		case "StatusVariantAlsoNegotiates":
			return http.StatusVariantAlsoNegotiates
		case "StatusInsufficientStorage":
			return http.StatusInsufficientStorage
		case "StatusLoopDetected":
			return http.StatusLoopDetected
		case "StatusNotExtended":
			return http.StatusNotExtended
		case "StatusNetworkAuthenticationRequired":
			return http.StatusNetworkAuthenticationRequired
		}
	}
	return 0
}

func extractFormatPrefix(format string) string {
	idx := strings.Index(format, "%")
	if idx > 0 {
		prefix := strings.TrimSpace(format[:idx])
		return strings.TrimRight(prefix, ": -")
	}
	cleaned := strings.TrimSpace(format)
	for _, spec := range []string{"%w", "%s", "%v", "%d", "%q", "%+v", "%#v"} {
		cleaned = strings.ReplaceAll(cleaned, spec, "")
	}
	cleaned = strings.Trim(cleaned, ": - \t\n\r")
	return cleaned
}

// ErrorPathsChecker verifies that every error condition in the unit's original implementation
// is covered by at least one failing recorded test case.
type ErrorPathsChecker struct {
	SourceDir      string
	Cases          []*adapterkit.Case
	CasesFile      string
	DeclaredErrors []string
}

// Check evaluates recorded cases against error paths.
func (c *ErrorPathsChecker) Check(ctx context.Context) (output string, issues []string, passed bool, err error) {
	cases := c.Cases
	if len(cases) == 0 && c.CasesFile != "" {
		loaded, loadErr := loadCasesFromJSONL(c.CasesFile)
		if loadErr != nil {
			return "", []string{fmt.Sprintf("failed to load cases: %v", loadErr)}, false, loadErr
		}
		cases = loaded
	}

	var expectedPaths []ErrorPath

	// 1. If explicit declared errors are provided, include them
	for _, dec := range c.DeclaredErrors {
		expectedPaths = append(expectedPaths, ErrorPath{
			Kind:    "declared",
			Pattern: dec,
		})
	}

	// 2. If SourceDir is provided, discover AST error paths
	if c.SourceDir != "" {
		discovered, discErr := ExtractErrorPathsFromDir(c.SourceDir)
		if discErr != nil {
			return "", []string{fmt.Sprintf("failed to discover error paths: %v", discErr)}, false, discErr
		}
		expectedPaths = append(expectedPaths, discovered...)
	}

	if len(expectedPaths) == 0 {
		return "no error paths detected in unit source; completeness clean", nil, true, nil
	}

	if len(cases) == 0 {
		return "error-path completeness check failed: no test cases recorded",
			[]string{"unit has declared error paths but zero recorded test cases"}, false, nil
	}

	// Filter error cases from recorded cases
	var errorCases []*adapterkit.Case
	for _, tc := range cases {
		if isErrorCase(tc) {
			errorCases = append(errorCases, tc)
		}
	}

	var missingIssues []string
	coveredCount := 0

	for _, ep := range expectedPaths {
		matched := false
		for _, tc := range errorCases {
			if caseMatchesErrorPath(tc, ep) {
				matched = true
				break
			}
		}

		if matched {
			coveredCount++
		} else {
			desc := ep.Pattern
			if desc == "" && ep.StatusCode > 0 {
				desc = fmt.Sprintf("HTTP %d", ep.StatusCode)
			}
			missingIssues = append(missingIssues, fmt.Sprintf("missing error-path case for [%s]: %s", ep.Kind, desc))
		}
	}

	sort.Strings(missingIssues)

	if len(missingIssues) > 0 {
		out := fmt.Sprintf("Error-path completeness check failed: %d/%d error paths covered by recorded cases",
			coveredCount, len(expectedPaths))
		return out, missingIssues, false, nil
	}

	return fmt.Sprintf("Error-path completeness clean: all %d error paths covered by recorded cases", len(expectedPaths)),
		nil, true, nil
}

func isErrorCase(tc *adapterkit.Case) bool {
	if tc.ExpectedOutput.StatusCode >= 400 {
		return true
	}
	if tc.ExpectedOutput.ExitCode != 0 {
		return true
	}
	if tc.ExpectedOutput.ErrorMessage != "" {
		return true
	}
	if tc.Kind == adapterkit.KindTrap {
		return true
	}
	if tc.ExpectedOutput.Stderr != "" {
		return true
	}
	if bodyMap, ok := tc.ExpectedOutput.Body.(map[string]any); ok {
		if _, hasErr := bodyMap["error"]; hasErr {
			return true
		}
	}
	return false
}

func caseMatchesErrorPath(tc *adapterkit.Case, ep ErrorPath) bool {
	// Status code check if declared on error path
	if ep.StatusCode > 0 {
		if tc.ExpectedOutput.StatusCode != ep.StatusCode {
			return false
		}
		if ep.Pattern == "" {
			return true
		}
	}

	if ep.Pattern == "" {
		return false
	}

	patternLower := strings.ToLower(ep.Pattern)

	// Check ErrorMessage
	if strings.Contains(strings.ToLower(tc.ExpectedOutput.ErrorMessage), patternLower) {
		return true
	}

	// Check Stderr
	if strings.Contains(strings.ToLower(tc.ExpectedOutput.Stderr), patternLower) {
		return true
	}

	// Check Body
	switch b := tc.ExpectedOutput.Body.(type) {
	case string:
		if strings.Contains(strings.ToLower(b), patternLower) {
			return true
		}
	case map[string]any:
		for _, v := range b {
			if strV, ok := v.(string); ok {
				if strings.Contains(strings.ToLower(strV), patternLower) {
					return true
				}
			}
		}
	}

	return false
}

func loadCasesFromJSONL(path string) ([]*adapterkit.Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cases []*adapterkit.Case
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(f)
	maxSize := int64(int(^uint(0) >> 1))
	if info.Size() < maxSize {
		maxSize = info.Size() + 1
	}
	scanner.Buffer(nil, int(maxSize))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}
		var c adapterkit.Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("failed to parse case line: %w", err)
		}
		cases = append(cases, &c)
	}
	return cases, scanner.Err()
}

// CheckErrorPathsExecutor creates a CheckExecutor for error-path completeness.
func CheckErrorPathsExecutor(sourceDir string, cases []*adapterkit.Case, declaredErrors ...string) CheckExecutor {
	checker := &ErrorPathsChecker{
		SourceDir:      sourceDir,
		Cases:          cases,
		DeclaredErrors: declaredErrors,
	}
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		return checker.Check(ctx)
	}
}
