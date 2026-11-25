package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mohammedrefaat/go-anything/internal/adapterhost"
)

func runDoctor(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprintf(stdout, `Usage:
  go-anything doctor [directory]

Diagnose workspace health, check .gitignore, and scan for leaked secrets.
`)
			return nil
		}
	}

	var targetDir string
	var activeFlags []string

	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			activeFlags = append(activeFlags, a)
		} else if targetDir == "" {
			targetDir = a
		}
	}

	if targetDir == "" {
		targetDir = "."
	}

	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	fmt.Fprintf(stdout, "go-anything doctor: inspecting workspace at %s\n\n", absPath)

	engine := adapterhost.NewDoctorEngine(absPath)
	report := engine.RunAllChecks(activeFlags)

	checkNames := []string{"gitignore", "committed_secrets", "unsafe_flags", "language_runtimes"}
	checkPassed := make(map[string]bool)
	for _, name := range checkNames {
		checkPassed[name] = true
	}

	for _, issue := range report.Issues {
		checkPassed[issue.CheckName] = false
		switch issue.Severity {
		case adapterhost.StatusError:
			fmt.Fprintf(stderr, "[FAIL] %s: %s\n", issue.CheckName, issue.Message)
		case adapterhost.StatusWarning:
			fmt.Fprintf(stderr, "[WARN] %s: %s\n", issue.CheckName, issue.Message)
		default:
			fmt.Fprintf(stderr, "[INFO] %s: %s\n", issue.CheckName, issue.Message)
		}
	}

	// Print PASS for checks with no issues
	if checkPassed["gitignore"] {
		fmt.Fprintf(stdout, "[PASS] gitignore: .gitignore covers .go-anything/local/ and raw captures\n")
	}
	if checkPassed["committed_secrets"] {
		fmt.Fprintf(stdout, "[PASS] committed_secrets: no unredacted secrets detected in repository\n")
	}
	if checkPassed["unsafe_flags"] {
		fmt.Fprintf(stdout, "[PASS] unsafe_flags: fail-closed safety defaults active\n")
	}
	if checkPassed["language_runtimes"] {
		fmt.Fprintf(stdout, "[PASS] language_runtimes: required language runtimes (node, python, go) accessible on PATH\n")
	}

	if !report.Healthy {
		return fmt.Errorf("doctor check failed with errors")
	}

	fmt.Fprintf(stdout, "\nDoctor: All health checks passed successfully.\n")
	return nil
}
