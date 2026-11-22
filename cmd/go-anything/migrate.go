package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/mohammedrefat/go-anything/internal/migrate"
)

func runMigrate(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	dirPath := fs.String("dir", ".", "Path to workspace repository root")
	fs.StringVar(dirPath, "d", ".", "Path to workspace repository root (shorthand)")

	orderedArgs := reorderArgs(fs, args)
	if err := fs.Parse(orderedArgs); err != nil {
		return err
	}

	targetDir := *dirPath
	if fs.NArg() > 0 {
		targetDir = fs.Arg(0)
	}

	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve workspace directory path: %w", err)
	}

	result, err := migrate.MigrateWorkspace(absPath)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	result.PrintSummary(stdout)
	return nil
}
