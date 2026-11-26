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

func runMap(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("map", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything map [flags]

List functions in a unit and display their porting and routing status.

Flags:
  --unit, -u <id>          Migration unit identifier (required)
  --dir, -d <path>         Path to workspace repository root (default: '.')
  --json                   Output function map as JSON
  --help, -h               Show this help message

Exit Codes:
  0   Success / OK
  2   Usage error (invalid flags or missing required arguments)
  3   Internal execution error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	dirPath := fs.String("dir", ".", "Path to workspace repository root")
	fs.StringVar(dirPath, "d", ".", "Path to workspace repository root (shorthand)")

	jsonOutput := fs.Bool("json", false, "Output function map as JSON")

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
	out, err := uc.Map(context.Background(), companion.MapInput{
		UnitID: *unitID,
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

	fmt.Fprintf(stdout, "Function Map for Unit %s (%d functions):\n\n", out.UnitID, out.TotalFunctions)
	fmt.Fprintf(stdout, "%-25s %-15s %-12s %-12s %s\n", "FUNCTION", "STATUS", "ROUTING", "IDEMPOTENT", "CASES")
	fmt.Fprintf(stdout, "%-25s %-15s %-12s %-12s %s\n", "--------", "------", "-------", "----------", "-----")
	for _, fn := range out.Functions {
		fmt.Fprintf(stdout, "%-25s %-15s %-12s %-12t %d\n", fn.Name, fn.Status, fn.Routing, fn.Idempotent, fn.CasesCount)
	}
	return nil
}
