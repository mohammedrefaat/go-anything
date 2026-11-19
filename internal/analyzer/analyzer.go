// Package analyzer provides static analysis for Go codebases using go/packages,
// go/types, AST inspection, and dependency graph mapping.
package analyzer

import (
	"context"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// PackageInfo holds static analysis information for a single Go package.
type PackageInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	PkgPath      string   `json:"pkg_path"`
	Dir          string   `json:"dir"`
	Imports      []string `json:"imports"`
	InternalDeps []string `json:"internal_deps"`
	Files        []string `json:"files"`
	PublicAPIs   []string `json:"public_apis"`
	Types        []string `json:"types"`
	Doc          string   `json:"doc,omitempty"`
	Errors       []string `json:"errors,omitempty"`
}

// Result holds the aggregated analysis result for a Go repository or module.
type Result struct {
	ModulePath    string                  `json:"module_path"`
	RootDirectory string                  `json:"root_dir"`
	Packages      map[string]*PackageInfo `json:"packages"`
	InternalGraph map[string][]string     `json:"internal_graph"` // pkgPath -> internal dependencies
}

// Analyzer inspects Go packages and maps dependency relationships.
type Analyzer struct{}

// New returns a new Analyzer instance.
func New() *Analyzer {
	return &Analyzer{}
}

// Analyze inspects Go packages in dir matching patterns (default: "./...").
func (a *Analyzer) Analyze(ctx context.Context, dir string, patterns ...string) (*Result, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedModule,
		Dir:     absDir,
		Context: ctx,
		Env:     append(os.Environ(), "CGO_ENABLED=0"),
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("failed to load packages: %w", err)
	}

	var modulePath string
	for _, pkg := range pkgs {
		if pkg.Module != nil && pkg.Module.Path != "" {
			modulePath = pkg.Module.Path
			break
		}
	}

	// If module path wasn't found in metadata, deduce from go.mod in dir
	if modulePath == "" {
		if modData, err := os.ReadFile(filepath.Join(absDir, "go.mod")); err == nil {
			for _, line := range strings.Split(string(modData), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					modulePath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
					break
				}
			}
		}
	}

	res := &Result{
		ModulePath:    modulePath,
		RootDirectory: absDir,
		Packages:      make(map[string]*PackageInfo),
		InternalGraph: make(map[string][]string),
	}

	for _, pkg := range pkgs {
		// Skip test-only synthetic packages or command-line-arguments
		if pkg.PkgPath == "" || pkg.Name == "" {
			continue
		}

		pkgInfo := a.extractPackageInfo(pkg, absDir, modulePath)
		res.Packages[pkgInfo.PkgPath] = pkgInfo
	}

	// Filter internal dependency graph
	for pkgPath, pkgInfo := range res.Packages {
		var internalDeps []string
		for _, imp := range pkgInfo.Imports {
			if _, exists := res.Packages[imp]; exists {
				internalDeps = append(internalDeps, imp)
			} else if modulePath != "" && strings.HasPrefix(imp, modulePath) {
				internalDeps = append(internalDeps, imp)
			}
		}
		sort.Strings(internalDeps)
		pkgInfo.InternalDeps = internalDeps
		res.InternalGraph[pkgPath] = internalDeps
	}

	return res, nil
}

func (a *Analyzer) extractPackageInfo(pkg *packages.Package, rootDir, modulePath string) *PackageInfo {
	pkgDir := ""
	var files []string
	for _, f := range pkg.GoFiles {
		cleanF := filepath.ToSlash(f)
		files = append(files, cleanF)
		if pkgDir == "" {
			pkgDir = filepath.Dir(cleanF)
		}
	}
	sort.Strings(files)

	var imports []string
	for impPath := range pkg.Imports {
		imports = append(imports, impPath)
	}
	sort.Strings(imports)

	var errs []string
	for _, e := range pkg.Errors {
		errs = append(errs, e.Error())
	}
	sort.Strings(errs)

	var publicAPIs []string
	var typesList []string

	if pkg.Types != nil && pkg.Types.Scope() != nil {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			if token.IsExported(name) {
				obj := scope.Lookup(name)
				if obj == nil {
					continue
				}

				switch o := obj.(type) {
				case *types.TypeName:
					typesList = append(typesList, name)
					publicAPIs = append(publicAPIs, "type "+name)

					// Inspect named type's methods
					if named, ok := o.Type().(*types.Named); ok {
						for m := 0; m < named.NumMethods(); m++ {
							method := named.Method(m)
							if method.Exported() {
								publicAPIs = append(publicAPIs, fmt.Sprintf("method (%s).%s", name, method.Name()))
							}
						}
					}

				case *types.Func:
					publicAPIs = append(publicAPIs, "func "+name)

				case *types.Const:
					publicAPIs = append(publicAPIs, "const "+name)

				case *types.Var:
					publicAPIs = append(publicAPIs, "var "+name)
				}
			}
		}
	}

	sort.Strings(publicAPIs)
	sort.Strings(typesList)

	// Short ID for the package: relative to module root or directory
	shortID := pkg.PkgPath
	if modulePath != "" && strings.HasPrefix(shortID, modulePath) {
		shortID = strings.TrimPrefix(shortID, modulePath)
		shortID = strings.TrimPrefix(shortID, "/")
	}
	if shortID == "" {
		shortID = pkg.Name
	}

	return &PackageInfo{
		ID:         shortID,
		Name:       pkg.Name,
		PkgPath:    pkg.PkgPath,
		Dir:        pkgDir,
		Imports:    imports,
		Files:      files,
		PublicAPIs: publicAPIs,
		Types:      typesList,
		Errors:     errs,
	}
}
