package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammedrefaat/go-anything/adapters/source/golang"
	"github.com/mohammedrefaat/go-anything/internal/state"
)

func runInit(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)

	sourceLang := fs.String("source", "go", "Source language of the application to migrate (e.g. 'go', 'python', 'typescript')")
	fs.StringVar(sourceLang, "s", "go", "Source language (shorthand)")

	projectName := fs.String("name", "", "Name of the project (defaults to directory name)")
	fs.StringVar(projectName, "n", "", "Name of the project (shorthand)")

	orderedArgs := reorderArgs(fs, args)
	if err := fs.Parse(orderedArgs); err != nil {
		return err
	}

	targetDir := "."
	if fs.NArg() > 0 {
		targetDir = fs.Arg(0)
	}

	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	if *projectName == "" {
		*projectName = filepath.Base(absPath)
	}

	store, err := state.InitializeWorkspace(absPath, *projectName, *sourceLang)
	if err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	// Ensure .gitignore also covers capture files for clean doctor runs
	ensureCaptureGitignore(absPath)

	fmt.Fprintf(stdout, "Initialized empty go-anything workspace in %s\n", store.RootDir())
	fmt.Fprintf(stdout, "Project:         %s\n", *projectName)
	fmt.Fprintf(stdout, "Source language: %s\n", *sourceLang)
	fmt.Fprintf(stdout, "Target language: go\n")
	fmt.Fprintf(stdout, "Profile:         %s\n", global.Profile)

	if *sourceLang == "go" {
		goAdapter := golang.NewAdapter()
		ctx := context.Background()
		modRes, err := goAdapter.ModernizeWorkspace(ctx, absPath, store)
		if err != nil {
			return fmt.Errorf("modernization initialization failed: %w", err)
		}

		fmt.Fprintf(stdout, "Three-tier:      legacy/ (frozen original) and modern/ (working copy)\n")
		for _, ch := range modRes.Tier1.AppliedChanges {
			fmt.Fprintf(stdout, "Tier 1:          %s\n", ch)
		}
		if len(modRes.Manifest.Units) > 0 {
			fmt.Fprintf(stdout, "Planned units:   %d registered across %d waves\n", len(modRes.Manifest.Units), len(modRes.Manifest.Waves))
		}
	}

	return nil
}

func runModernize(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	// Alias for init --source=go
	modernizeArgs := []string{"--source=go"}
	modernizeArgs = append(modernizeArgs, args...)
	return runInit(global, modernizeArgs, stdout, stderr)
}

func ensureCaptureGitignore(repoRoot string) {
	gitignorePath := filepath.Join(repoRoot, state.GitignoreFileName)
	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return
	}

	text := string(content)
	if !strings.Contains(text, "captures") && !strings.Contains(text, "*.capture") {
		entry := "\n# raw captured traffic fixtures\n*.capture\n.go-anything/captures/\n"
		f, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			_, _ = f.WriteString(entry)
			_ = f.Close()
		}
	}
}
