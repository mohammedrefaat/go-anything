package python

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mohammedrefaat/go-anything/internal/planner"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// StandardLibraryModules lists known Python standard library modules (Python 3.8-3.13)
// that should not be classified as internal application dependency units.
var StandardLibraryModules = map[string]struct{}{
	"__future__":      {},
	"abc":             {},
	"argparse":        {},
	"array":           {},
	"ast":             {},
	"asynchat":        {},
	"asyncio":         {},
	"asyncore":        {},
	"base64":          {},
	"binascii":        {},
	"binhex":          {},
	"bisect":          {},
	"builtins":        {},
	"bz2":             {},
	"calendar":        {},
	"cgi":             {},
	"cgitb":           {},
	"chunk":           {},
	"cmath":           {},
	"cmd":             {},
	"code":            {},
	"codecs":          {},
	"codeop":          {},
	"collections":     {},
	"colorsys":        {},
	"compileall":      {},
	"concurrent":      {},
	"configparser":    {},
	"contextlib":      {},
	"contextvars":     {},
	"copy":            {},
	"copyreg":         {},
	"cProfile":        {},
	"crypt":           {},
	"csv":             {},
	"ctypes":          {},
	"curses":          {},
	"dataclasses":     {},
	"datetime":        {},
	"dbm":             {},
	"decimal":         {},
	"difflib":         {},
	"dis":             {},
	"distutils":       {},
	"doctest":         {},
	"email":           {},
	"encodings":       {},
	"enum":            {},
	"errno":           {},
	"faulthandler":    {},
	"fcntl":           {},
	"filecmp":         {},
	"fileinput":       {},
	"fnmatch":         {},
	"fractions":       {},
	"ftplib":          {},
	"functools":       {},
	"gc":              {},
	"getopt":          {},
	"getpass":         {},
	"gettext":         {},
	"glob":            {},
	"graphlib":        {},
	"grp":             {},
	"gzip":            {},
	"hashlib":         {},
	"heapq":           {},
	"hmac":            {},
	"html":            {},
	"http":            {},
	"idlelib":         {},
	"imaplib":         {},
	"imghdr":          {},
	"imp":             {},
	"importlib":       {},
	"inspect":         {},
	"io":              {},
	"ipaddress":       {},
	"itertools":       {},
	"json":            {},
	"keyword":         {},
	"lib2to3":         {},
	"linecache":       {},
	"locale":          {},
	"logging":         {},
	"lzma":            {},
	"mailbox":         {},
	"mailcap":         {},
	"marshal":         {},
	"math":            {},
	"mimetypes":       {},
	"mmap":            {},
	"modulefinder":    {},
	"msilib":          {},
	"msvcrt":          {},
	"multiprocessing": {},
	"netrc":           {},
	"nntplib":         {},
	"numbers":         {},
	"operator":        {},
	"optparse":        {},
	"os":              {},
	"ossaudiodev":     {},
	"pathlib":         {},
	"pdb":             {},
	"pickle":          {},
	"pickletools":     {},
	"pipes":           {},
	"pkgutil":         {},
	"platform":        {},
	"plistlib":        {},
	"poplib":          {},
	"posix":           {},
	"posixpath":       {},
	"pprint":          {},
	"profile":         {},
	"pstats":          {},
	"pty":             {},
	"pwd":             {},
	"py_compile":      {},
	"pyclbr":          {},
	"pydoc":           {},
	"queue":           {},
	"quopri":          {},
	"random":          {},
	"re":              {},
	"readline":        {},
	"reprlib":         {},
	"resource":        {},
	"rlcompleter":     {},
	"runpy":           {},
	"sched":           {},
	"secrets":         {},
	"select":          {},
	"selectors":       {},
	"shelve":          {},
	"shlex":           {},
	"shutil":          {},
	"signal":          {},
	"site":            {},
	"smtpd":           {},
	"smtplib":         {},
	"sndhdr":          {},
	"socket":          {},
	"socketserver":    {},
	"spwd":            {},
	"sqlite3":         {},
	"ssl":             {},
	"stat":            {},
	"statistics":      {},
	"string":          {},
	"stringprep":      {},
	"struct":          {},
	"subprocess":      {},
	"sunau":           {},
	"symbol":          {},
	"symtable":        {},
	"sys":             {},
	"sysconfig":       {},
	"syslog":          {},
	"tabnanny":        {},
	"tarfile":         {},
	"telnetlib":       {},
	"tempfile":        {},
	"termios":         {},
	"test":            {},
	"textwrap":        {},
	"threading":       {},
	"time":            {},
	"timeit":          {},
	"tkinter":         {},
	"token":           {},
	"tokenize":        {},
	"tomllib":         {},
	"trace":           {},
	"traceback":       {},
	"tracemalloc":     {},
	"tty":             {},
	"turtle":          {},
	"turtledemo":      {},
	"types":           {},
	"typing":          {},
	"unicodedata":     {},
	"unittest":        {},
	"urllib":          {},
	"uu":              {},
	"uuid":            {},
	"venv":            {},
	"warnings":        {},
	"wave":            {},
	"weakref":         {},
	"webbrowser":      {},
	"winreg":          {},
	"winsound":        {},
	"wsgiref":         {},
	"xdrlib":          {},
	"xml":             {},
	"xmlrpc":          {},
	"zipapp":          {},
	"zipfile":         {},
	"zipimport":       {},
	"zlib":            {},
	"zoneinfo":        {},
	// Common test frameworks
	"pytest": {},
	"mock":   {},
}

// ImportStatement represents an import parsed from Python source code.
type ImportStatement struct {
	Module   string // e.g. "os", "myapp.models", "helper"
	Symbols  []string
	Relative int // 0 for absolute, 1 for '.', 2 for '..', etc.
	Line     int
}

// PythonModuleInfo represents an inspected Python file or package module.
type PythonModuleInfo struct {
	FilePath     string
	PackagePath  string // Dot-delimited module path e.g. "myapp.models"
	UnitID       string // Normalized unit ID e.g. "myapp/models"
	IsPackage    bool   // true if __init__.py
	Imports      []ImportStatement
	Classes      []string
	Functions    []string
	Dependencies []string // Resolved internal unit IDs imported by this module
}

// ASTParser extracts imports, classes, and function definitions from Python files.
type ASTParser struct{}

// NewASTParser creates a new Python AST parser.
func NewASTParser() *ASTParser {
	return &ASTParser{}
}

var (
	importRegex     = regexp.MustCompile(`^\s*import\s+([a-zA-Z0-9_.,\s]+)`)
	fromImportRegex = regexp.MustCompile(`^\s*from\s+(\.*[a-zA-Z0-9_.]*)\s+import\s+([a-zA-Z0-9_.,\s*()]+)`)
	defRegex        = regexp.MustCompile(`^\s*def\s+([a-zA-Z0-9_]+)\s*\(`)
	classRegex      = regexp.MustCompile(`^\s*class\s+([a-zA-Z0-9_]+)\s*(?:\(([^)]*)\))?:`)
)

// ParseFile parses a Python source file, returning its imports, classes, and functions.
func (p *ASTParser) ParseFile(filePath string) (*PythonModuleInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open python file %q: %w", filePath, err)
	}
	defer file.Close()

	info := &PythonModuleInfo{
		FilePath: filePath,
	}

	if filepath.Base(filePath) == "__init__.py" {
		info.IsPackage = true
	}

	scanner := bufio.NewScanner(file)
	lineNum := 0
	inMultilineComment := false
	commentDelimiter := ""
	inMultilineImport := false
	var multilineFromModule string
	var multilineRelative int
	var multilineSymbols strings.Builder

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Handle multiline triple-quoted strings
		if inMultilineComment {
			if strings.Contains(line, commentDelimiter) {
				inMultilineComment = false
			}
			continue
		}

		if strings.HasPrefix(trimmed, `"""`) {
			if !strings.HasSuffix(trimmed[3:], `"""`) || len(trimmed) == 3 {
				inMultilineComment = true
				commentDelimiter = `"""`
				continue
			}
			continue
		}
		if strings.HasPrefix(trimmed, `'''`) {
			if !strings.HasSuffix(trimmed[3:], `'''`) || len(trimmed) == 3 {
				inMultilineComment = true
				commentDelimiter = `'''`
				continue
			}
			continue
		}

		// Handle multiline parenthesized from-imports
		if inMultilineImport {
			if idx := strings.Index(line, "#"); idx != -1 {
				line = line[:idx]
			}
			if strings.Contains(line, ")") {
				inMultilineImport = false
				multilineSymbols.WriteString(line[:strings.Index(line, ")")])
				symbols := parseSymbols(multilineSymbols.String())
				info.Imports = append(info.Imports, ImportStatement{
					Module:   multilineFromModule,
					Symbols:  symbols,
					Relative: multilineRelative,
					Line:     lineNum,
				})
				multilineSymbols.Reset()
			} else {
				multilineSymbols.WriteString(line + " ")
			}
			continue
		}

		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Strip inline comments
		cleanLine := trimmed
		if idx := strings.Index(cleanLine, "#"); idx != -1 {
			cleanLine = strings.TrimSpace(cleanLine[:idx])
		}

		// Parse class declarations
		if matches := classRegex.FindStringSubmatch(cleanLine); len(matches) > 1 {
			info.Classes = append(info.Classes, matches[1])
			continue
		}

		// Parse function definitions
		if matches := defRegex.FindStringSubmatch(cleanLine); len(matches) > 1 {
			info.Functions = append(info.Functions, matches[1])
			continue
		}

		// Parse "from ... import ..."
		if matches := fromImportRegex.FindStringSubmatch(cleanLine); len(matches) > 2 {
			moduleRaw := matches[1]
			symbolsRaw := matches[2]

			relLevel := 0
			for strings.HasPrefix(moduleRaw, ".") {
				relLevel++
				moduleRaw = moduleRaw[1:]
			}

			if strings.Contains(symbolsRaw, "(") && !strings.Contains(symbolsRaw, ")") {
				inMultilineImport = true
				multilineFromModule = moduleRaw
				multilineRelative = relLevel
				multilineSymbols.Reset()
				multilineSymbols.WriteString(symbolsRaw[strings.Index(symbolsRaw, "(")+1:] + " ")
				continue
			}

			symbols := parseSymbols(symbolsRaw)
			info.Imports = append(info.Imports, ImportStatement{
				Module:   moduleRaw,
				Symbols:  symbols,
				Relative: relLevel,
				Line:     lineNum,
			})
			continue
		}

		// Parse "import ..."
		if matches := importRegex.FindStringSubmatch(cleanLine); len(matches) > 1 {
			modules := parseImportModules(matches[1])
			for _, m := range modules {
				info.Imports = append(info.Imports, ImportStatement{
					Module: m,
					Line:   lineNum,
				})
			}
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading python file %q: %w", filePath, err)
	}

	return info, nil
}

func parseSymbols(raw string) []string {
	raw = strings.Trim(raw, "()")
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		// Handle "symbol as alias"
		if idx := strings.Index(s, " as "); idx != -1 {
			s = strings.TrimSpace(s[:idx])
		}
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

func parseImportModules(raw string) []string {
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		// Handle "module as alias"
		if idx := strings.Index(s, " as "); idx != -1 {
			s = strings.TrimSpace(s[:idx])
		}
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// DependencyDiscoverer analyzes a Python codebase to discover packages, modules, and waves.
type DependencyDiscoverer struct {
	parser *ASTParser
}

// NewDependencyDiscoverer creates a new Python dependency discoverer.
func NewDependencyDiscoverer() *DependencyDiscoverer {
	return &DependencyDiscoverer{
		parser: NewASTParser(),
	}
}

// DiscoveredPythonProject contains the analyzed modules, units, and dependency graph.
type DiscoveredPythonProject struct {
	RepoPath    string
	ProjectName string
	Units       []*adapterkit.Unit
	Plan        *planner.PlanResult
	Modules     map[string]*PythonModuleInfo
	Graph       map[string][]string // unitID -> imported unitIDs
}

// Discover analyzes a Python repository and computes dependency waves.
func (d *DependencyDiscoverer) Discover(ctx context.Context, repoPath string) (*DiscoveredPythonProject, error) {
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for repo %q: %w", repoPath, err)
	}

	// 1. Scan repository for all Python files
	pyFiles := make(map[string]string) // relative path -> absolute path
	err = filepath.WalkDir(absRepo, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "__pycache__" || name == "venv" || name == ".venv" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".py") {
			rel, relErr := filepath.Rel(absRepo, path)
			if relErr == nil {
				// Normalize to forward slashes
				relNorm := filepath.ToSlash(rel)
				pyFiles[relNorm] = path
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk repository %q: %w", absRepo, err)
	}

	if len(pyFiles) == 0 {
		return nil, fmt.Errorf("no python files discovered in %q", repoPath)
	}

	// 2. Parse all discovered Python files
	modules := make(map[string]*PythonModuleInfo)
	for relPath, absFile := range pyFiles {
		info, parseErr := d.parser.ParseFile(absFile)
		if parseErr != nil {
			return nil, parseErr
		}

		// Compute dot-delimited module path and unit ID
		modPath := filePathToModulePath(relPath)
		unitID := modulePathToUnitID(modPath)

		info.PackagePath = modPath
		info.UnitID = unitID
		modules[modPath] = info
	}

	// 3. Resolve imports into internal unit dependencies
	graph := make(map[string][]string)
	unitSet := make(map[string]struct{})

	for modPath, info := range modules {
		unitID := info.UnitID
		unitSet[unitID] = struct{}{}
		if _, exists := graph[unitID]; !exists {
			graph[unitID] = []string{}
		}

		for _, imp := range info.Imports {
			resolvedUnitID := resolveImportToUnit(modPath, imp, modules)
			if resolvedUnitID != "" && resolvedUnitID != unitID {
				// Internal dependency identified
				graph[unitID] = append(graph[unitID], resolvedUnitID)
			}
		}
	}

	// Deduplicate dependencies per unit
	for uid, deps := range graph {
		graph[uid] = deduplicateAndSort(deps)
	}

	// 4. Wave planning via planner.PlanWaves
	p := planner.New()
	plan := p.PlanWaves(graph)

	// Enrich units with Python-specific metadata
	for _, u := range plan.Units {
		u.Metadata["source_language"] = "python"
		// Collect all files belonging to this unit
		var unitFiles []string
		var classes []string
		var functions []string
		for modPath, minfo := range modules {
			if minfo.UnitID == u.ID {
				rel, _ := filepath.Rel(absRepo, minfo.FilePath)
				unitFiles = append(unitFiles, filepath.ToSlash(rel))
				classes = append(classes, minfo.Classes...)
				functions = append(functions, minfo.Functions...)
				u.Metadata["package_path"] = modPath
			}
		}
		sort.Strings(unitFiles)
		sort.Strings(classes)
		sort.Strings(functions)

		u.Metadata["files"] = unitFiles
		u.Metadata["classes"] = classes
		u.Metadata["functions"] = functions
	}

	projectName := extractProjectName(absRepo)
	if projectName == "" {
		projectName = filepath.Base(absRepo)
		if projectName == "" || projectName == "." {
			projectName = "python-project"
		}
	}

	return &DiscoveredPythonProject{
		RepoPath:    absRepo,
		ProjectName: projectName,
		Units:       plan.Units,
		Plan:        plan,
		Modules:     modules,
		Graph:       graph,
	}, nil
}

var projectNameRegex = regexp.MustCompile(`(?m)^\s*name\s*=\s*["']([^"']+)["']`)

func extractProjectName(repoPath string) string {
	pyprojectPath := filepath.Join(repoPath, "pyproject.toml")
	if data, err := os.ReadFile(pyprojectPath); err == nil {
		if matches := projectNameRegex.FindStringSubmatch(string(data)); len(matches) > 1 {
			return matches[1]
		}
	}

	setupPyPath := filepath.Join(repoPath, "setup.py")
	if data, err := os.ReadFile(setupPyPath); err == nil {
		if matches := projectNameRegex.FindStringSubmatch(string(data)); len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

// filePathToModulePath converts "pkg/sub/mod.py" or "pkg/sub/__init__.py" into "pkg.sub.mod" or "pkg.sub".
func filePathToModulePath(relPath string) string {
	relPath = filepath.ToSlash(relPath)
	relPath = strings.TrimSuffix(relPath, ".py")
	if strings.HasSuffix(relPath, "/__init__") {
		relPath = strings.TrimSuffix(relPath, "/__init__")
	} else if relPath == "__init__" {
		return "main"
	}
	return strings.ReplaceAll(relPath, "/", ".")
}

// modulePathToUnitID converts "myapp.services.billing" into "myapp/services/billing" or package level unit.
func modulePathToUnitID(modPath string) string {
	if modPath == "" {
		return "root"
	}
	return strings.ReplaceAll(modPath, ".", "/")
}

// resolveImportToUnit attempts to map an import statement to a known internal module/unit.
func resolveImportToUnit(currentMod string, imp ImportStatement, modules map[string]*PythonModuleInfo) string {
	var targetMod string

	if imp.Relative > 0 {
		// Resolve relative import
		currentParts := strings.Split(currentMod, ".")
		// If currentMod is a package (__init__.py was stripped), its parts count as package.
		// Go up (imp.Relative - 1) levels for parent packages.
		levelsUp := imp.Relative - 1
		if levelsUp >= len(currentParts) {
			levelsUp = len(currentParts) - 1
		}
		if levelsUp < 0 {
			levelsUp = 0
		}
		baseParts := currentParts[:len(currentParts)-levelsUp]
		if imp.Module != "" {
			targetMod = strings.Join(baseParts, ".") + "." + imp.Module
		} else {
			targetMod = strings.Join(baseParts, ".")
		}
	} else {
		targetMod = imp.Module
	}

	// Check if targetMod is in standard library
	rootPkg := targetMod
	if idx := strings.Index(targetMod, "."); idx != -1 {
		rootPkg = targetMod[:idx]
	}
	if _, isStd := StandardLibraryModules[rootPkg]; isStd {
		return ""
	}

	// Exact match
	if info, ok := modules[targetMod]; ok {
		return info.UnitID
	}

	// Try checking if imported symbol is a submodule: e.g. "from pkg import submodule"
	if len(imp.Symbols) > 0 {
		for _, sym := range imp.Symbols {
			candidate := targetMod + "." + sym
			if info, ok := modules[candidate]; ok {
				return info.UnitID
			}
		}
	}

	// Try prefix matches for parent package unit
	parts := strings.Split(targetMod, ".")
	for i := len(parts); i > 0; i-- {
		candidate := strings.Join(parts[:i], ".")
		if info, ok := modules[candidate]; ok {
			return info.UnitID
		}
	}

	return ""
}

func deduplicateAndSort(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	var result []string
	for _, item := range items {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}
