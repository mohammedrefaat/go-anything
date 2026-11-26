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

func runClean(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything clean [flags]

Remove generated bridge artifacts while preserving developer Go implementations.

Flags:
  --unit, -u <id>          Migration unit identifier
  --all, -a                Clean all bridge units
  --force, -y              Force cleanup including user implementation files
  --dir, -d <path>         Path to workspace repository root (default: '.')
  --json                   Output result as JSON
  --help, -h               Show this help message

Exit Codes:
  0   Success / OK
  2   Usage error (invalid flags or missing required arguments)
  3   Internal execution error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	allFlag := fs.Bool("all", false, "Clean all bridge units")
	fs.BoolVar(allFlag, "a", false, "Clean all units (shorthand)")

	forceFlag := fs.Bool("force", false, "Force cleanup including user implementation files")
	fs.BoolVar(forceFlag, "y", false, "Force cleanup (shorthand)")

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

	if strings.TrimSpace(*unitID) == "" && !*allFlag {
		return exitErrorf(ExitCodeUsage, "flag --unit (-u) or --all (-a) is required")
	}

	uc := companion.New()
	out, err := uc.Clean(context.Background(), companion.CleanInput{
		UnitID: *unitID,
		All:    *allFlag,
		Force:  *forceFlag,
		Dir:    *dirPath,
	})
	if err != nil {
		return exitErrorf(ExitCodeInternal, "%w", err)
	}

	if *jsonOutput {
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return nil
	}

	fmt.Fprintln(stdout, out.Message)
	for _, f := range out.FilesRemoved {
		fmt.Fprintf(stdout, "  - Removed: %s\n", f)
	}
	for _, f := range out.PreservedFiles {
		fmt.Fprintf(stdout, "  - Preserved: %s\n", f)
	}
	return nil
}
