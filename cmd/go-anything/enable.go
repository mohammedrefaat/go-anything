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

func runEnable(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("enable", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything enable [flags]

Enable Go strangler bridge routing for a migration unit or specific function.

Flags:
  --unit, -u <id>          Migration unit identifier (required)
  --function, -f <name>    Function name to enable (optional)
  --dir, -d <path>         Path to workspace repository root (default: '.')
  --json                   Output result as JSON
  --help, -h               Show this help message

Exit Codes:
  0   Success / OK
  2   Usage error (invalid flags or missing required arguments)
  3   Internal execution error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	functionName := fs.String("function", "", "Function name to enable")
	fs.StringVar(functionName, "f", "", "Function name (shorthand)")

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
	out, err := uc.Toggle(context.Background(), companion.ToggleInput{
		UnitID:   *unitID,
		Function: *functionName,
		Dir:      *dirPath,
		Enable:   true,
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
	return nil
}
