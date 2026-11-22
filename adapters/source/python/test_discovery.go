package python

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TestFramework identifies the Python test framework in use.
type TestFramework string

const (
	FrameworkPytest   TestFramework = "pytest"
	FrameworkUnittest TestFramework = "unittest"
)

// TestCase represents an individual discovered test case.
type TestCase struct {
	Framework TestFramework `json:"framework"`
	UnitID    string        `json:"unit_id"`
	FilePath  string        `json:"file_path"`
	ClassName string        `json:"class_name,omitempty"`
	Name      string        `json:"name"`
	Line      int           `json:"line"`
	IsAsync   bool          `json:"is_async,omitempty"`
}

// TestSuite represents a group of test cases in a file or class.
type TestSuite struct {
	Framework TestFramework `json:"framework"`
	UnitID    string        `json:"unit_id"`
	FilePath  string        `json:"file_path"`
	ClassName string        `json:"class_name,omitempty"`
	Cases     []TestCase    `json:"cases"`
}

// TestDiscoveryResult contains the full set of discovered test suites and cases.
type TestDiscoveryResult struct {
	TotalCases int         `json:"total_cases"`
	Suites     []TestSuite `json:"suites"`
}

// TestDiscoverer inspects a Python project for pytest and unittest suites.
type TestDiscoverer struct{}

// NewTestDiscoverer creates a new Python test discoverer.
func NewTestDiscoverer() *TestDiscoverer {
	return &TestDiscoverer{}
}

var (
	testFuncRegex    = regexp.MustCompile(`^\s*(async\s+)?def\s+(test_[a-zA-Z0-9_]+)\s*\(`)
	unittestClassReg = regexp.MustCompile(`^\s*class\s+([a-zA-Z0-9_]+)\s*\((?:[^)]*TestCase[^)]*)\):`)
	pytestClassRegex = regexp.MustCompile(`^\s*class\s+(Test[a-zA-Z0-9_]*)\s*(?:\([^)]*\))?:`)
)

// DiscoverTests scans repoPath for pytest and unittest suites.
func (td *TestDiscoverer) DiscoverTests(ctx context.Context, repoPath string, units []string) (*TestDiscoveryResult, error) {
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path %q: %w", repoPath, err)
	}

	result := &TestDiscoveryResult{}

	err = filepath.WalkDir(absRepo, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "__pycache__" || name == "venv" || name == ".venv" {
				return filepath.SkipDir
			}
			return nil
		}

		baseName := entry.Name()
		if !strings.HasSuffix(baseName, ".py") {
			return nil
		}

		// Identify test files: test_*.py, *_test.py, or any .py file under a tests/ or test/ directory
		rel, relErr := filepath.Rel(absRepo, path)
		if relErr != nil {
			return nil
		}
		relNorm := filepath.ToSlash(rel)

		isTestFile := strings.HasPrefix(baseName, "test_") ||
			strings.HasSuffix(baseName, "_test.py") ||
			strings.Contains(relNorm, "/tests/") ||
			strings.Contains(relNorm, "/test/") ||
			strings.HasPrefix(relNorm, "tests/") ||
			strings.HasPrefix(relNorm, "test/")

		if !isTestFile {
			return nil
		}

		suites, parseErr := td.parseTestFile(path, relNorm, units)
		if parseErr != nil {
			return parseErr
		}

		for _, s := range suites {
			result.TotalCases += len(s.Cases)
			result.Suites = append(result.Suites, s)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error discovering tests in %q: %w", absRepo, err)
	}

	return result, nil
}

func (td *TestDiscoverer) parseTestFile(absPath, relPath string, knownUnits []string) ([]TestSuite, error) {
	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open test file %q: %w", absPath, err)
	}
	defer file.Close()

	unitID := deduceUnitID(relPath, knownUnits)

	var suites []TestSuite
	currentClass := ""
	currentFramework := FrameworkPytest
	var currentClassCases []TestCase
	var fileLevelCases []TestCase

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check unittest TestCase class
		if matches := unittestClassReg.FindStringSubmatch(line); len(matches) > 1 {
			// Flush previous class
			if currentClass != "" && len(currentClassCases) > 0 {
				suites = append(suites, TestSuite{
					Framework: currentFramework,
					UnitID:    unitID,
					FilePath:  relPath,
					ClassName: currentClass,
					Cases:     currentClassCases,
				})
				currentClassCases = nil
			}
			currentClass = matches[1]
			currentFramework = FrameworkUnittest
			continue
		}

		// Check pytest Test* class
		if matches := pytestClassRegex.FindStringSubmatch(line); len(matches) > 1 {
			// Flush previous class
			if currentClass != "" && len(currentClassCases) > 0 {
				suites = append(suites, TestSuite{
					Framework: currentFramework,
					UnitID:    unitID,
					FilePath:  relPath,
					ClassName: currentClass,
					Cases:     currentClassCases,
				})
				currentClassCases = nil
			}
			currentClass = matches[1]
			currentFramework = FrameworkPytest
			continue
		}

		// Check if outdented out of class
		if currentClass != "" && len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			if len(currentClassCases) > 0 {
				suites = append(suites, TestSuite{
					Framework: currentFramework,
					UnitID:    unitID,
					FilePath:  relPath,
					ClassName: currentClass,
					Cases:     currentClassCases,
				})
				currentClassCases = nil
			}
			currentClass = ""
			currentFramework = FrameworkPytest
		}

		// Check test function definition
		if matches := testFuncRegex.FindStringSubmatch(line); len(matches) > 2 {
			isAsync := strings.TrimSpace(matches[1]) == "async"
			fnName := matches[2]

			tc := TestCase{
				Framework: currentFramework,
				UnitID:    unitID,
				FilePath:  relPath,
				ClassName: currentClass,
				Name:      fnName,
				Line:      lineNum,
				IsAsync:   isAsync,
			}

			if currentClass != "" {
				currentClassCases = append(currentClassCases, tc)
			} else {
				fileLevelCases = append(fileLevelCases, tc)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read test file %q: %w", absPath, err)
	}

	// Flush remaining class cases
	if currentClass != "" && len(currentClassCases) > 0 {
		suites = append(suites, TestSuite{
			Framework: currentFramework,
			UnitID:    unitID,
			FilePath:  relPath,
			ClassName: currentClass,
			Cases:     currentClassCases,
		})
	}

	// Flush file-level cases
	if len(fileLevelCases) > 0 {
		suites = append(suites, TestSuite{
			Framework: FrameworkPytest,
			UnitID:    unitID,
			FilePath:  relPath,
			Cases:     fileLevelCases,
		})
	}

	return suites, nil
}

// deduceUnitID infers which unit the test corresponds to.
func deduceUnitID(relTestPath string, knownUnits []string) string {
	base := filepath.Base(relTestPath)
	candidate := strings.TrimSuffix(base, ".py")
	candidate = strings.TrimPrefix(candidate, "test_")
	candidate = strings.TrimSuffix(candidate, "_test")

	// Check against known units
	for _, u := range knownUnits {
		if u == candidate || strings.HasSuffix(u, "/"+candidate) {
			return u
		}
	}

	// If inside a package e.g. "services/billing/test_billing.py"
	dir := filepath.Dir(relTestPath)
	dir = strings.TrimPrefix(dir, "tests/")
	dir = strings.TrimPrefix(dir, "test/")
	if dir != "" && dir != "." {
		return filepath.ToSlash(dir)
	}

	if len(knownUnits) > 0 {
		return knownUnits[0]
	}
	return candidate
}
