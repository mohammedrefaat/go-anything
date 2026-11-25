package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mohammedrefaat/go-anything/internal/app/companion"
)

func runRefresh(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything refresh [flags]

Re-record behavioral test cases after original changes and evaluate updated diffs.

Flags:
  --unit, -u <id>          Migration unit identifier (required)
  --dir, -d <path>         Path to workspace repository root (default: '.')
  --json                   Output result as JSON
  --help, -h               Show this help message

Exit Codes:
  0   Success / OK
  1   Diff divergence detected on refreshed cases
  2   Usage error (invalid flags or missing required arguments)
  3   Internal execution error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	dirPath := fs.String("dir", ".", "Path to workspace repository root")
	fs.StringVar(dirPath, "d", ".", "Path to workspace repository root (shorthand)")

	jsonOutput := fs.Bool("json", false, "Output result as JSON")

	orderedArgs := reorderArgs(fs, args)
	if err := fs.Parse(orderedArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return exitErrorf(ExitCodeUsage, "%w", err)
	}

	if strings.TrimSpace(*unitID) == "" {
		return exitErrorf(ExitCodeUsage, "flag --unit (-u) is required")
	}

	uc := companion.New()
	out, err := uc.Refresh(context.Background(), companion.RefreshInput{
		UnitID: *unitID,
		Dir:    *dirPath,
	})
	if err != nil {
		return exitErrorf(ExitCodeInternal, "%w", err)
	}

	if *jsonOutput {
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(data))
		if out.DiffStatus != "clean" {
			return exitErrorf(ExitCodeFail, "diff divergence detected: %s", out.DiffStatus)
		}
		return nil
	}

	fmt.Fprintln(stdout, out.Message)
	if out.DiffStatus != "clean" {
		return exitErrorf(ExitCodeFail, "diff divergence detected: %s", out.DiffStatus)
	}
	return nil
}
