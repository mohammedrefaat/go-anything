package gate

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// APISymbol represents an exported symbol in a Go package.
type APISymbol struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"` // "func", "type", "method", "const", "var", "field", "interface_method"
	Receiver    string   `json:"receiver,omitempty"`
	Signature   string   `json:"signature"`
	ParamTypes  []string `json:"param_types,omitempty"`
	ReturnTypes []string `json:"return_types,omitempty"`
}

// APISurface contains all exported symbols for a Go package or package directory.
type APISurface struct {
	PackageName string               `json:"package_name"`
	Symbols     map[string]APISymbol `json:"symbols"` // key: Kind + ":" + Name
}

// NewAPISurface creates an initialized APISurface.
func NewAPISurface(pkgName string) *APISurface {
	return &APISurface{
		PackageName: pkgName,
		Symbols:     make(map[string]APISymbol),
	}
}

// ExtractAPISurfaceFromDir parses all non-test Go source files in dir and extracts exported symbols.
func ExtractAPISurfaceFromDir(dir string) (*APISurface, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	var pkgName string
	surface := NewAPISurface("")

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", filePath, err)
		}

		if pkgName == "" {
			pkgName = f.Name.Name
			surface.PackageName = pkgName
		}

		extractSymbolsFromFile(fset, f, surface)
	}

	return surface, nil
}

// ExtractAPISurfaceFromSource parses a Go source code string and extracts exported symbols.
func ExtractAPISurfaceFromSource(filename, src string) (*APISurface, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse source %s: %w", filename, err)
	}

	surface := NewAPISurface(f.Name.Name)
	extractSymbolsFromFile(fset, f, surface)
	return surface, nil
}

func extractSymbolsFromFile(fset *token.FileSet, f *ast.File, surface *APISurface) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			extractGenDeclSymbols(fset, d, surface)
		case *ast.FuncDecl:
			extractFuncDeclSymbols(fset, d, surface)
		}
	}
}

func extractGenDeclSymbols(fset *token.FileSet, decl *ast.GenDecl, surface *APISurface) {
	switch decl.Tok {
	case token.CONST, token.VAR:
		kind := "const"
		if decl.Tok == token.VAR {
			kind = "var"
		}
		for _, spec := range decl.Specs {
			vspec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			typeStr := ""
			if vspec.Type != nil {
				typeStr = nodeToString(fset, vspec.Type)
			}
			for _, name := range vspec.Names {
				if name.IsExported() {
					key := kind + ":" + name.Name
					surface.Symbols[key] = APISymbol{
						Name:      name.Name,
						Kind:      kind,
						Signature: typeStr,
					}
				}
			}
		}

	case token.TYPE:
		for _, spec := range decl.Specs {
			tspec, ok := spec.(*ast.TypeSpec)
			if !ok || !tspec.Name.IsExported() {
				continue
			}
			typeName := tspec.Name.Name

			switch t := tspec.Type.(type) {
			case *ast.StructType:
				surface.Symbols["type:"+typeName] = APISymbol{
					Name:      typeName,
					Kind:      "type",
					Signature: "struct",
				}
				if t.Fields != nil {
					for _, field := range t.Fields.List {
						fieldType := nodeToString(fset, field.Type)
						for _, fieldName := range field.Names {
							if fieldName.IsExported() {
								fKey := "field:" + typeName + "." + fieldName.Name
								surface.Symbols[fKey] = APISymbol{
									Name:      typeName + "." + fieldName.Name,
									Kind:      "field",
									Signature: fieldType,
								}
							}
						}
					}
				}

			case *ast.InterfaceType:
				surface.Symbols["type:"+typeName] = APISymbol{
					Name:      typeName,
					Kind:      "type",
					Signature: "interface",
				}
				if t.Methods != nil {
					for _, m := range t.Methods.List {
						mType := nodeToString(fset, m.Type)
						for _, mName := range m.Names {
							if mName.IsExported() {
								mKey := "interface_method:" + typeName + "." + mName.Name
								surface.Symbols[mKey] = APISymbol{
									Name:      typeName + "." + mName.Name,
									Kind:      "interface_method",
									Signature: mType,
								}
							}
						}
					}
				}

			default:
				surface.Symbols["type:"+typeName] = APISymbol{
					Name:      typeName,
					Kind:      "type",
					Signature: nodeToString(fset, tspec.Type),
				}
			}
		}
	}
}

func extractFuncDeclSymbols(fset *token.FileSet, decl *ast.FuncDecl, surface *APISurface) {
	if !decl.Name.IsExported() {
		return
	}

	sig := nodeToString(fset, decl.Type)
	var params, returns []string
	if decl.Type.Params != nil {
		for _, p := range decl.Type.Params.List {
			tStr := nodeToString(fset, p.Type)
			count := len(p.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				params = append(params, tStr)
			}
		}
	}
	if decl.Type.Results != nil {
		for _, r := range decl.Type.Results.List {
			tStr := nodeToString(fset, r.Type)
			count := len(r.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				returns = append(returns, tStr)
			}
		}
	}

	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recvType := extractReceiverTypeName(decl.Recv.List[0].Type)
		if isExportedIdentifier(recvType) {
			symName := recvType + "." + decl.Name.Name
			key := "method:" + symName
			surface.Symbols[key] = APISymbol{
				Name:        symName,
				Kind:        "method",
				Receiver:    recvType,
				Signature:   sig,
				ParamTypes:  params,
				ReturnTypes: returns,
			}
		}
	} else {
		key := "func:" + decl.Name.Name
		surface.Symbols[key] = APISymbol{
			Name:        decl.Name.Name,
			Kind:        "func",
			Signature:   sig,
			ParamTypes:  params,
			ReturnTypes: returns,
		}
	}
}

func extractReceiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return extractReceiverTypeName(e.X)
	default:
		return ""
	}
}

func isExportedIdentifier(id string) bool {
	id = strings.TrimPrefix(id, "*")
	return token.IsExported(id)
}

func nodeToString(fset *token.FileSet, node any) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, node); err != nil {
		return ""
	}
	// Normalize whitespace
	return strings.Join(strings.Fields(buf.String()), " ")
}

// CompareAPIs compares legacy and modern APISurfaces and returns a deterministic list of breaking changes.
func CompareAPIs(legacy, modern *APISurface) []string {
	var issues []string

	for key, legSym := range legacy.Symbols {
		modSym, ok := modern.Symbols[key]
		if !ok {
			issues = append(issues, fmt.Sprintf("removed exported %s: %s", legSym.Kind, legSym.Name))
			continue
		}

		// Verify signature/type compatibility
		switch legSym.Kind {
		case "func", "method":
			if !areSignaturesCompatible(legSym, modSym) {
				issues = append(issues, fmt.Sprintf("incompatible signature change for %s: expected %s, got %s",
					legSym.Name, legSym.Signature, modSym.Signature))
			}
		case "field", "interface_method", "type", "var", "const":
			if legSym.Signature != "" && modSym.Signature != "" && legSym.Signature != modSym.Signature {
				issues = append(issues, fmt.Sprintf("incompatible %s definition for %s: expected %s, got %s",
					legSym.Kind, legSym.Name, legSym.Signature, modSym.Signature))
			}
		}
	}

	sort.Strings(issues)
	return issues
}

func areSignaturesCompatible(s1, s2 APISymbol) bool {
	if len(s1.ParamTypes) != len(s2.ParamTypes) {
		return false
	}
	for i := range s1.ParamTypes {
		if s1.ParamTypes[i] != s2.ParamTypes[i] {
			return false
		}
	}

	if len(s1.ReturnTypes) != len(s2.ReturnTypes) {
		return false
	}
	for i := range s1.ReturnTypes {
		if s1.ReturnTypes[i] != s2.ReturnTypes[i] {
			return false
		}
	}

	return true
}

// APICompatChecker executes public API compatibility verification between legacy and modern implementations.
type APICompatChecker struct {
	LegacyDir string
	ModernDir string
}

// Check compares exported symbols between LegacyDir and ModernDir.
func (c *APICompatChecker) Check(ctx context.Context) (output string, issues []string, passed bool, err error) {
	legacySurface, err := ExtractAPISurfaceFromDir(c.LegacyDir)
	if err != nil {
		return "", []string{fmt.Sprintf("failed to parse legacy API surface: %v", err)}, false, err
	}

	modernSurface, err := ExtractAPISurfaceFromDir(c.ModernDir)
	if err != nil {
		return "", []string{fmt.Sprintf("failed to parse modern API surface: %v", err)}, false, err
	}

	breakingIssues := CompareAPIs(legacySurface, modernSurface)
	if len(breakingIssues) > 0 {
		return fmt.Sprintf("API compatibility failed: %d breaking change(s) detected", len(breakingIssues)),
			breakingIssues, false, nil
	}

	return fmt.Sprintf("API compatibility clean: all %d exported symbols preserved", len(legacySurface.Symbols)),
		nil, true, nil
}

// CheckAPICompatExecutor creates a CheckExecutor that runs the API compatibility check.
func CheckAPICompatExecutor(legacyDir, modernDir string) CheckExecutor {
	checker := &APICompatChecker{
		LegacyDir: legacyDir,
		ModernDir: modernDir,
	}
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		return checker.Check(ctx)
	}
}
