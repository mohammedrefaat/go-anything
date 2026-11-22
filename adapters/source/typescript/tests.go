package typescript

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TestFramework identifies a JavaScript/TypeScript testing framework.
type TestFramework string

const (
	FrameworkJest    TestFramework = "jest"
	FrameworkVitest  TestFramework = "vitest"
	FrameworkMocha   TestFramework = "mocha"
	FrameworkUnknown TestFramework = "unknown"
)

// TestCase represents an individual discovered test case.
type TestCase struct {
	Name       string `json:"name"`
	LineNumber int    `json:"line_number,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
}

// TestSuite represents a test file containing describe blocks and test cases.
type TestSuite struct {
	Name      string        `json:"name"`
	FilePath  string        `json:"file_path"`
	Framework TestFramework `json:"framework"`
	Cases     []TestCase    `json:"cases"`
}

// TestDiscoveryResult contains the discovered test frameworks, suites, and total test counts.
type TestDiscoveryResult struct {
	Frameworks []TestFramework `json:"frameworks"`
	Suites     []TestSuite     `json:"suites"`
	TotalTests int             `json:"total_tests"`
	TotalFiles int             `json:"total_files"`
}

var (
	describeRegex = regexp.MustCompile(`\bdescribe(?:\.(?:skip|only))?\s*\(\s*['"]([^'"]+)['"]`)
	testCaseRegex = regexp.MustCompile(`\b(?:it|test)(?:\.(?:skip|only))?\s*\(\s*['"]([^'"]+)['"]`)
	skippedRegex  = regexp.MustCompile(`\b(?:it|test)\.skip\s*\(\s*['"]([^'"]+)['"]`)
)

// DetectTestFrameworks identifies test frameworks used in the repository.
func DetectTestFrameworks(repoPath string, pkgJSON *PackageJSON) []TestFramework {
	found := make(map[TestFramework]bool)

	// Check package.json dependencies and scripts
	if pkgJSON != nil {
		checkDeps := func(deps map[string]string) {
			for name := range deps {
				lower := strings.ToLower(name)
				if strings.Contains(lower, "jest") {
					found[FrameworkJest] = true
				}
				if strings.Contains(lower, "vitest") {
					found[FrameworkVitest] = true
				}
				if strings.Contains(lower, "mocha") {
					found[FrameworkMocha] = true
				}
			}
		}
		checkDeps(pkgJSON.Dependencies)
		checkDeps(pkgJSON.DevDependencies)
		checkDeps(pkgJSON.PeerDependencies)

		for _, script := range pkgJSON.Scripts {
			lower := strings.ToLower(script)
			if strings.Contains(lower, "jest") {
				found[FrameworkJest] = true
			}
			if strings.Contains(lower, "vitest") {
				found[FrameworkVitest] = true
			}
			if strings.Contains(lower, "mocha") {
				found[FrameworkMocha] = true
			}
		}
	}

	// Check configuration files in repo root
	configFiles := map[string]TestFramework{
		"jest.config.js":    FrameworkJest,
		"jest.config.ts":    FrameworkJest,
		"jest.config.mjs":   FrameworkJest,
		"jest.config.cjs":   FrameworkJest,
		"jest.config.json":  FrameworkJest,
		"vitest.config.ts":  FrameworkVitest,
		"vitest.config.js":  FrameworkVitest,
		"vitest.config.mjs": FrameworkVitest,
		".mocharc.json":     FrameworkMocha,
		".mocharc.js":       FrameworkMocha,
		".mocharc.yml":      FrameworkMocha,
		".mocharc.yaml":     FrameworkMocha,
	}

	for cfg, fw := range configFiles {
		if _, err := os.Stat(filepath.Join(repoPath, cfg)); err == nil {
			found[fw] = true
		}
	}

	var frameworks []TestFramework
	for fw := range found {
		frameworks = append(frameworks, fw)
	}
	sort.Slice(frameworks, func(i, j int) bool {
		return string(frameworks[i]) < string(frameworks[j])
	})

	if len(frameworks) == 0 {
		frameworks = append(frameworks, FrameworkUnknown)
	}

	return frameworks
}

// DiscoverTests scans the repository for test files and extracts test suites and test cases.
func DiscoverTests(repoPath string) (*TestDiscoveryResult, error) {
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo path: %w", err)
	}

	pkgJSON, _ := ParsePackageJSON(absRepo)
	frameworks := DetectTestFrameworks(absRepo, pkgJSON)
	primaryFramework := FrameworkJest
	if len(frameworks) > 0 && frameworks[0] != FrameworkUnknown {
		primaryFramework = frameworks[0]
	}

	var testFiles []string
	err = filepath.Walk(absRepo, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if ignoredDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if IsTypeScriptOrJavaScriptFile(path) && IsTestFilePath(path) {
			testFiles = append(testFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk repository for test files: %w", err)
	}

	sort.Strings(testFiles)

	var suites []TestSuite
	totalTests := 0

	for _, tf := range testFiles {
		relPath, _ := filepath.Rel(absRepo, tf)
		relPath = filepath.ToSlash(relPath)

		suite, err := parseTestFile(tf, relPath, primaryFramework)
		if err != nil {
			continue
		}
		totalTests += len(suite.Cases)
		suites = append(suites, *suite)
	}

	return &TestDiscoveryResult{
		Frameworks: frameworks,
		Suites:     suites,
		TotalTests: totalTests,
		TotalFiles: len(testFiles),
	}, nil
}

func parseTestFile(filePath, relPath string, defaultFramework TestFramework) (*TestSuite, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	suiteName := filepath.Base(filePath)
	var cases []TestCase

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Look for describe suite name
		if descMatch := describeRegex.FindStringSubmatch(line); len(descMatch) > 1 {
			suiteName = descMatch[1]
		}

		// Look for test/it cases
		if testMatch := testCaseRegex.FindStringSubmatch(line); len(testMatch) > 1 {
			isSkipped := skippedRegex.MatchString(line)
			cases = append(cases, TestCase{
				Name:       testMatch[1],
				LineNumber: lineNum,
				Skipped:    isSkipped,
			})
		}
	}

	return &TestSuite{
		Name:      suiteName,
		FilePath:  relPath,
		Framework: defaultFramework,
		Cases:     cases,
	}, scanner.Err()
}
