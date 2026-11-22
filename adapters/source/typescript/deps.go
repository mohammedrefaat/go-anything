package typescript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mohammedrefat/go-anything/internal/planner"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// PackageJSON models the standard Node.js package.json file.
type PackageJSON struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	Description      string            `json:"description,omitempty"`
	Main             string            `json:"main,omitempty"`
	Module           string            `json:"module,omitempty"`
	Types            string            `json:"types,omitempty"`
	Typings          string            `json:"typings,omitempty"`
	Scripts          map[string]string `json:"scripts,omitempty"`
	Dependencies     map[string]string `json:"dependencies,omitempty"`
	DevDependencies  map[string]string `json:"devDependencies,omitempty"`
	PeerDependencies map[string]string `json:"peerDependencies,omitempty"`
	Workspaces       any               `json:"workspaces,omitempty"`
}

// ImportStatement represents an extracted module dependency from source code.
type ImportStatement struct {
	Specifier  string `json:"specifier"`
	IsRelative bool   `json:"is_relative"`
	IsDynamic  bool   `json:"is_dynamic"`
	IsCommonJS bool   `json:"is_commonjs"`
}

// Regex patterns for ESM imports, exports, dynamic imports, and CommonJS requires.
var (
	// Match: import ... from 'spec' or import ... from "spec"
	esmImportFromRegex = regexp.MustCompile(`(?m)\bimport\s+(?:(?:[\w*\s{},$]+)\s+from\s+)?['"]([^'"]+)['"]`)
	// Match: export ... from 'spec' or export * from "spec"
	esmExportFromRegex = regexp.MustCompile(`(?m)\bexport\s+(?:(?:[\w*\s{},$]+)\s+from\s+)['"]([^'"]+)['"]`)
	// Match: import('spec') or import("spec")
	dynamicImportRegex = regexp.MustCompile(`\bimport\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	// Match: require('spec') or require("spec")
	commonJSRegex = regexp.MustCompile(`\brequire\s*\(\s*['"]([^'"]+)['"]\s*\)`)

	// Comment removal
	blockCommentRegex = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineCommentRegex  = regexp.MustCompile(`(?m)//.*$`)
)

// ParsePackageJSON reads and unmarshals package.json from repoPath.
func ParsePackageJSON(repoPath string) (*PackageJSON, error) {
	pkgPath := filepath.Join(repoPath, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read package.json at %s: %w", pkgPath, err)
	}

	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse package.json at %s: %w", pkgPath, err)
	}

	return &pkg, nil
}

// StripComments removes single-line and multi-line comments from JavaScript/TypeScript source code.
func StripComments(source string) string {
	noBlock := blockCommentRegex.ReplaceAllString(source, " ")
	return lineCommentRegex.ReplaceAllString(noBlock, " ")
}

// ParseSourceImports extracts all ESM and CommonJS import/require specifiers from source code.
func ParseSourceImports(source string) []ImportStatement {
	cleanSource := StripComments(source)
	var imports []ImportStatement
	seen := make(map[string]bool)

	addImport := func(spec string, isDynamic, isCommonJS bool) {
		spec = strings.TrimSpace(spec)
		if spec == "" || seen[spec] {
			return
		}
		seen[spec] = true
		isRel := strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || spec == "." || spec == ".."
		imports = append(imports, ImportStatement{
			Specifier:  spec,
			IsRelative: isRel,
			IsDynamic:  isDynamic,
			IsCommonJS: isCommonJS,
		})
	}

	// 1. ESM import ... from '...'
	for _, match := range esmImportFromRegex.FindAllStringSubmatch(cleanSource, -1) {
		if len(match) > 1 {
			addImport(match[1], false, false)
		}
	}

	// 2. ESM export ... from '...'
	for _, match := range esmExportFromRegex.FindAllStringSubmatch(cleanSource, -1) {
		if len(match) > 1 {
			addImport(match[1], false, false)
		}
	}

	// 3. Dynamic import('...')
	for _, match := range dynamicImportRegex.FindAllStringSubmatch(cleanSource, -1) {
		if len(match) > 1 {
			addImport(match[1], true, false)
		}
	}

	// 4. CommonJS require('...')
	for _, match := range commonJSRegex.FindAllStringSubmatch(cleanSource, -1) {
		if len(match) > 1 {
			addImport(match[1], false, true)
		}
	}

	sort.Slice(imports, func(i, j int) bool {
		return imports[i].Specifier < imports[j].Specifier
	})

	return imports
}

// Supported source file extensions in TypeScript/JavaScript projects.
var supportedExtensions = []string{
	".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs",
}

// IsTypeScriptOrJavaScriptFile checks if a file path has a supported TS/JS extension.
func IsTypeScriptOrJavaScriptFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, candidate := range supportedExtensions {
		if ext == candidate {
			return true
		}
	}
	return false
}

// IsTestFilePath checks if a file name represents a test file (Jest, Vitest, Mocha).
func IsTestFilePath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasSuffix(base, "_test.ts") || strings.HasSuffix(base, "_test.js") {
		return true
	}
	// Check if in __tests__ or test/ or tests/
	normalized := filepath.ToSlash(path)
	if strings.Contains(normalized, "/__tests__/") || strings.Contains(normalized, "/test/") || strings.Contains(normalized, "/tests/") {
		return true
	}
	return false
}

// ResolveModule attempts to resolve an import specifier relative to currentFile within repoRoot.
// Returns resolved relative path from repoRoot and whether it resolved to an external package.
func ResolveModule(repoRoot, currentFilePath, specifier string) (resolvedPath string, isExternal bool) {
	if !strings.HasPrefix(specifier, "./") && !strings.HasPrefix(specifier, "../") && specifier != "." && specifier != ".." {
		// External npm module or alias
		return specifier, true
	}

	fileDir := filepath.Dir(currentFilePath)
	target := filepath.Join(fileDir, specifier)

	// Check candidate file paths
	candidates := []string{
		target,
	}
	for _, ext := range supportedExtensions {
		candidates = append(candidates, target+ext)
	}
	// Check index files
	for _, ext := range supportedExtensions {
		candidates = append(candidates, filepath.Join(target, "index"+ext))
	}

	for _, cand := range candidates {
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			rel, err := filepath.Rel(repoRoot, cand)
			if err == nil {
				return filepath.ToSlash(rel), false
			}
			return filepath.ToSlash(cand), false
		}
	}

	// Fallback to relative normalized target
	rel, err := filepath.Rel(repoRoot, target)
	if err == nil {
		return filepath.ToSlash(rel), false
	}
	return filepath.ToSlash(target), false
}

// Ignored directory names during repository traversal.
var ignoredDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	".go-anything": true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
	".next":        true,
	".nuxt":        true,
	".turbo":       true,
	".cache":       true,
}

// SourceFileInfo stores metadata for a discovered source file.
type SourceFileInfo struct {
	RelativePath string
	Imports      []ImportStatement
	UnitID       string
}

// AnalyzeDependencies traverses the repository, parses package.json and source files,
// builds the dependency graph between units, and calculates topological migration waves.
func AnalyzeDependencies(repoPath string) (*adapterkit.Manifest, []*adapterkit.Unit, error) {
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve repo path: %w", err)
	}

	projectName := filepath.Base(absRepo)
	var pkgJSON *PackageJSON
	if pj, err := ParsePackageJSON(absRepo); err == nil {
		pkgJSON = pj
		if pj.Name != "" {
			projectName = pj.Name
		}
	}

	// 1. Discover all source files
	var sourceFiles []string
	err = filepath.Walk(absRepo, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if ignoredDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}
		if IsTypeScriptOrJavaScriptFile(path) && !IsTestFilePath(path) {
			sourceFiles = append(sourceFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to walk repository %s: %w", absRepo, err)
	}

	if len(sourceFiles) == 0 {
		return nil, nil, fmt.Errorf("no TypeScript or JavaScript source files found in %s", absRepo)
	}

	// 2. Map files to candidate units
	// Determine unit granularity:
	// - If files are organized in distinct subdirectories (e.g. src/auth, src/users, lib/math), each directory is a unit.
	// - If files are in a single directory or flat, each file is a unit.
	dirFileMap := make(map[string][]string)
	for _, sf := range sourceFiles {
		rel, err := filepath.Rel(absRepo, sf)
		if err != nil {
			rel = sf
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		dirFileMap[dir] = append(dirFileMap[dir], rel)
	}

	fileToUnit := make(map[string]string)
	var unitList []string
	useFileGranularity := len(dirFileMap) <= 1

	if useFileGranularity {
		for _, sf := range sourceFiles {
			rel, _ := filepath.Rel(absRepo, sf)
			rel = filepath.ToSlash(rel)
			unitID := strings.TrimSuffix(rel, filepath.Ext(rel))
			fileToUnit[rel] = unitID
			unitList = append(unitList, unitID)
		}
	} else {
		for dir, files := range dirFileMap {
			unitID := filepath.ToSlash(dir)
			if unitID == "." {
				unitID = "root"
			}
			unitList = append(unitList, unitID)
			for _, f := range files {
				fileToUnit[f] = unitID
			}
		}
	}

	sort.Strings(unitList)

	// 3. Parse imports and build unit dependency graph
	unitDeps := make(map[string]map[string]bool)
	unitFiles := make(map[string][]string)
	for _, u := range unitList {
		unitDeps[u] = make(map[string]bool)
	}

	for _, sf := range sourceFiles {
		rel, _ := filepath.Rel(absRepo, sf)
		rel = filepath.ToSlash(rel)
		uID := fileToUnit[rel]
		unitFiles[uID] = append(unitFiles[uID], rel)

		content, err := os.ReadFile(sf)
		if err != nil {
			continue
		}
		imports := ParseSourceImports(string(content))
		for _, imp := range imports {
			if imp.IsRelative {
				resolved, isExt := ResolveModule(absRepo, sf, imp.Specifier)
				if !isExt {
					// Check which unit owns the resolved file
					targetUnit, exists := fileToUnit[resolved]
					if !exists {
						// Try without extension or matching directory
						resolvedTrimmed := strings.TrimSuffix(resolved, filepath.Ext(resolved))
						if u, ok := fileToUnit[resolvedTrimmed]; ok {
							targetUnit = u
							exists = true
						} else {
							targetDir := filepath.ToSlash(filepath.Dir(resolved))
							if targetDir == "." {
								targetDir = "root"
							}
							if _, ok := unitDeps[targetDir]; ok {
								targetUnit = targetDir
								exists = true
							}
						}
					}
					if exists && targetUnit != uID {
						unitDeps[uID][targetUnit] = true
					}
				}
			}
		}
	}

	// 4. Construct graph map for internal/planner
	graph := make(map[string][]string)
	for u, depMap := range unitDeps {
		var deps []string
		for dep := range depMap {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		graph[u] = deps
	}

	// 5. Compute topological waves using internal/planner
	p := planner.New()
	planResult := p.PlanWaves(graph)

	// Enrich metadata for each unit
	var finalUnitIDs []string
	for _, u := range planResult.Units {
		if u == nil {
			continue
		}
		if u.Metadata == nil {
			u.Metadata = make(map[string]any)
		}
		u.Metadata["source_language"] = "typescript"
		if files, ok := unitFiles[u.ID]; ok {
			sort.Strings(files)
			u.Metadata["files"] = files
		}
		if pkgJSON != nil && pkgJSON.Dependencies != nil {
			u.Metadata["package_dependencies"] = pkgJSON.Dependencies
		}
		finalUnitIDs = append(finalUnitIDs, u.ID)
	}

	var waves []adapterkit.Wave
	for w := 0; w <= planResult.MaxWave; w++ {
		if unitsInWave, ok := planResult.Waves[w]; ok && len(unitsInWave) > 0 {
			var ids []string
			for _, u := range unitsInWave {
				ids = append(ids, u.ID)
			}
			sort.Strings(ids)
			waves = append(waves, adapterkit.Wave{
				Wave:    w,
				UnitIDs: ids,
			})
		}
	}

	manifest := &adapterkit.Manifest{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		ProjectName:    projectName,
		SourceLanguage: "typescript",
		TargetLanguage: "go",
		Units:          finalUnitIDs,
		Waves:          waves,
	}

	return manifest, planResult.Units, nil
}
