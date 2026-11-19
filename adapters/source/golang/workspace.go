package golang

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ThreeTierWorkspace represents the three-tier workspace structure:
// Root (workspace), legacy/ (frozen original), and modern/ (working copy).
type ThreeTierWorkspace struct {
	RepoRoot   string `json:"repo_root"`
	LegacyDir  string `json:"legacy_dir"`
	ModernDir  string `json:"modern_dir"`
	ModulePath string `json:"module_path"`
	GoVersion  string `json:"go_version"`
}

// DetectModule extracts the module path and go directive from go.mod in repoRoot.
// If go.mod is missing, it falls back to the directory name and Go 1.26.
func DetectModule(repoRoot string) (modulePath, goVersion string) {
	goVersion = "1.26"
	modPath := filepath.Join(repoRoot, "go.mod")
	if data, err := os.ReadFile(modPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				modulePath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			} else if strings.HasPrefix(line, "go ") {
				goVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
			}
		}
	}

	if modulePath == "" {
		base := filepath.Base(repoRoot)
		if base == "." || base == "/" || base == "\\" || base == "" {
			modulePath = "project"
		} else {
			modulePath = base
		}
	}

	return modulePath, goVersion
}

// SetupThreeTierWorkspace establishes the three-tier workspace structure in repoRoot:
// - legacy/: frozen original copy, namespaced to <module>/legacy
// - modern/: active modernization copy, namespaced to <module>/modern
// - Root: workspace orchestration with go.work and root go.mod replacing both tiers.
func SetupThreeTierWorkspace(repoRoot string) (*ThreeTierWorkspace, error) {
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo path: %w", err)
	}

	legacyDir := filepath.Join(absRoot, "legacy")
	modernDir := filepath.Join(absRoot, "modern")

	modulePath, goVersion := DetectModule(absRoot)

	ws := &ThreeTierWorkspace{
		RepoRoot:   absRoot,
		LegacyDir:  legacyDir,
		ModernDir:  modernDir,
		ModulePath: modulePath,
		GoVersion:  goVersion,
	}

	// If both legacy/ and modern/ already exist, workspace is already partitioned
	legStat, errLeg := os.Stat(legacyDir)
	modStat, errMod := os.Stat(modernDir)
	if errLeg == nil && legStat.IsDir() && errMod == nil && modStat.IsDir() {
		return ws, nil
	}

	// 1. Copy project files into legacy/ and modern/
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create legacy dir: %w", err)
	}
	if err := os.MkdirAll(modernDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create modern dir: %w", err)
	}

	if err := copyProjectFiles(absRoot, legacyDir); err != nil {
		return nil, fmt.Errorf("failed to copy files to legacy: %w", err)
	}
	if err := copyProjectFiles(absRoot, modernDir); err != nil {
		return nil, fmt.Errorf("failed to copy files to modern: %w", err)
	}

	// 2. Namespace legacy/ (<module>/legacy)
	legacyMod := modulePath + "/legacy"
	if err := setupTierModule(legacyDir, modulePath, legacyMod, goVersion); err != nil {
		return nil, fmt.Errorf("failed to configure legacy module: %w", err)
	}

	// 3. Namespace modern/ (<module>/modern)
	modernMod := modulePath + "/modern"
	if err := setupTierModule(modernDir, modulePath, modernMod, goVersion); err != nil {
		return nil, fmt.Errorf("failed to configure modern module: %w", err)
	}

	// 4. Clean root directory of relocated package directories and .go files
	cleanRelocatedRootFiles(absRoot, legacyDir)

	// 5. Generate root go.mod and go.work
	if err := writeRootConfigs(absRoot, modulePath, legacyMod, modernMod, goVersion); err != nil {
		return nil, fmt.Errorf("failed to write root configs: %w", err)
	}

	return ws, nil
}

func copyProjectFiles(srcRoot, destDir string) error {
	return filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcRoot, path)
		if err != nil || rel == "." {
			return nil
		}

		// Skip VCS, metadata, and target tier directories
		topDir := strings.Split(filepath.ToSlash(rel), "/")[0]
		switch topDir {
		case ".git", ".go-anything", "legacy", "modern", ".idea", ".vscode":
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		destPath := filepath.Join(destDir, rel)
		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Skip binaries, temporary and lock files
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext == ".exe" || ext == ".tmp" || ext == ".lock" || ext == ".log" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		return os.WriteFile(destPath, data, 0644)
	})
}

func setupTierModule(tierDir, oldModule, newModule, goVersion string) error {
	modFile := filepath.Join(tierDir, "go.mod")
	if _, err := os.Stat(modFile); os.IsNotExist(err) {
		// Generate minimal go.mod
		content := fmt.Sprintf("module %s\n\ngo %s\n", newModule, goVersion)
		if err := os.WriteFile(modFile, []byte(content), 0644); err != nil {
			return err
		}
	} else {
		// Rewrite existing go.mod
		data, err := os.ReadFile(modFile)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		var newLines []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "module ") {
				newLines = append(newLines, "module "+newModule)
			} else {
				// Replace any oldModule internal replace directives
				line = strings.ReplaceAll(line, oldModule+"/", newModule+"/")
				newLines = append(newLines, line)
			}
		}
		if err := os.WriteFile(modFile, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
			return err
		}
	}

	// Rewrite internal imports in all .go files within the tier
	return rewriteImportsInDir(tierDir, oldModule, newModule)
}

func rewriteImportsInDir(dir, oldModule, newModule string) error {
	fset := token.NewFileSet()
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == ".go-anything" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil
		}

		modified := false
		for _, imp := range file.Imports {
			if imp.Path == nil {
				continue
			}
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == oldModule {
				imp.Path.Value = `"` + newModule + `"`
				modified = true
			} else if strings.HasPrefix(importPath, oldModule+"/") {
				suffix := strings.TrimPrefix(importPath, oldModule+"/")
				imp.Path.Value = `"` + newModule + "/" + suffix + `"`
				modified = true
			}
		}

		if modified {
			var buf bytes.Buffer
			if err := format.Node(&buf, fset, file); err == nil {
				_ = os.WriteFile(path, buf.Bytes(), info.Mode().Perm())
			}
		}
		return nil
	})
}

func cleanRelocatedRootFiles(srcRoot, legacyDir string) {
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		switch name {
		case ".git", ".go-anything", "legacy", "modern", "go.mod", "go.sum", "go.work", "go.work.sum", "README.md", "LICENSE", ".gitignore":
			continue
		}

		// Only remove if it was safely mirrored in legacy
		legacyPath := filepath.Join(legacyDir, name)
		if _, err := os.Stat(legacyPath); err == nil {
			_ = os.RemoveAll(filepath.Join(srcRoot, name))
		}
	}
}

func writeRootConfigs(absRoot, rootModule, legacyMod, modernMod, goVersion string) error {
	// Root go.mod
	rootModPath := filepath.Join(absRoot, "go.mod")
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("module %s\n\ngo %s\n\n", rootModule, goVersion))
	buf.WriteString(fmt.Sprintf("replace %s => ./legacy\n", legacyMod))
	buf.WriteString(fmt.Sprintf("replace %s => ./modern\n\n", modernMod))
	buf.WriteString("require (\n")
	buf.WriteString(fmt.Sprintf("\t%s v0.0.0\n", legacyMod))
	buf.WriteString(fmt.Sprintf("\t%s v0.0.0\n", modernMod))
	buf.WriteString(")\n")

	if err := os.WriteFile(rootModPath, buf.Bytes(), 0644); err != nil {
		return err
	}

	// Root go.work
	workPath := filepath.Join(absRoot, "go.work")
	var workBuf bytes.Buffer
	workBuf.WriteString(fmt.Sprintf("go %s\n\nuse (\n\t./legacy\n\t./modern\n)\n", goVersion))

	return os.WriteFile(workPath, workBuf.Bytes(), 0644)
}
