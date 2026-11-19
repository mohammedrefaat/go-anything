package golang

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Tier1Result holds the outcomes of Tier 1 mechanical transformations.
type Tier1Result struct {
	AppliedChanges []string `json:"applied_changes"`
	TestsChecked   bool     `json:"tests_checked"`
	TestOutput     string   `json:"test_output,omitempty"`
}

// RunTier1 executes Tier 1 mechanical transformations on the modern/ working copy:
// 1. Code formatting via standard go/format (gofmt -s behavior)
// 2. Automated mechanical fixes via go fix ./...
// 3. Verification that existing tests continue to pass in modern/
func RunTier1(ctx context.Context, modernDir string) (*Tier1Result, error) {
	absDir, err := filepath.Abs(modernDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve modern directory: %w", err)
	}

	res := &Tier1Result{
		AppliedChanges: []string{},
	}

	// 1. Format code using go/format
	formattedCount := 0
	err = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == ".go-anything" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".go") {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			formatted, err := format.Source(data)
			if err == nil && !bytes.Equal(data, formatted) {
				if writeErr := os.WriteFile(path, formatted, info.Mode().Perm()); writeErr == nil {
					formattedCount++
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("formatting failed: %w", err)
	}
	res.AppliedChanges = append(res.AppliedChanges, fmt.Sprintf("gofmt: formatted %d source files", formattedCount))

	// 2. Run go fix ./... if go executable is available
	goBin, err := exec.LookPath("go")
	if err == nil && goBin != "" {
		cmdFix := exec.CommandContext(ctx, goBin, "fix", "./...")
		cmdFix.Dir = absDir
		cmdFix.Env = append(os.Environ(), "CGO_ENABLED=0")
		_ = cmdFix.Run() // go fix modifies files in place; non-zero if warnings, don't abort
		res.AppliedChanges = append(res.AppliedChanges, "go fix: applied mechanical modernization fixes")

		// Re-run format after go fix
		_ = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
				if data, err := os.ReadFile(path); err == nil {
					if formatted, err := format.Source(data); err == nil && !bytes.Equal(data, formatted) {
						_ = os.WriteFile(path, formatted, info.Mode().Perm())
					}
				}
			}
			return nil
		})

		// 3. Verify existing tests in modern/
		hasTests := false
		_ = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), "_test.go") {
				hasTests = true
				return filepath.SkipAll
			}
			return nil
		})

		if hasTests {
			cmdTest := exec.CommandContext(ctx, goBin, "test", "./...")
			cmdTest.Dir = absDir
			cmdTest.Env = append(os.Environ(), "CGO_ENABLED=0")
			out, testErr := cmdTest.CombinedOutput()
			res.TestOutput = string(out)
			if testErr != nil {
				return res, fmt.Errorf("tier 1 verification failed: existing tests in modern/ failed after mechanical changes: %s", string(out))
			}
			res.TestsChecked = true
			res.AppliedChanges = append(res.AppliedChanges, "tests: verified existing unit tests pass cleanly")
		}
	}

	return res, nil
}
