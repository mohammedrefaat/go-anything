package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mohammedrefat/go-anything/internal/app/cutover"
)

// runCutover handles cutover planning, route inspection, and emergency kill switch operations.
//
// STRICT COMPLIANCE RULE R-1:
// Cutover NEVER executes, imports, or triggers 'lift'. Lift is a separate, human-only,
// permanent structural refactoring operation. Cutover only controls dynamic traffic routing
// and kill switch engagement.
func runCutover(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	action := "status"
	var subArgs []string

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		candidate := strings.ToLower(args[0])
		if candidate == "plan" || candidate == "status" || candidate == "kill" {
			action = candidate
			subArgs = args[1:]
		} else {
			return exitErrorf(ExitCodeUsage, "invalid cutover action %q (must be plan, status, or kill)", args[0])
		}
	} else {
		subArgs = args
	}

	fs := flag.NewFlagSet("cutover", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything cutover <action> [flags]

Manage and inspect production traffic cutover for a migration unit.

Actions:
  plan      Evaluate prerequisites and verify cutover readiness
  status    Inspect current routing destination and fallback counters (default)
  kill      Trigger emergency kill switch to route traffic immediately back to legacy original

Flags:
  --unit, -u <id>              Migration unit identifier (required)
  --dir, -d <path>             Path to workspace repository root (default: '.')
  --json                       Output cutover results as JSON
  --help, -h                   Show this help message

Exit Codes:
  0   Success / OK
  2   Usage error (invalid action, missing flags, or invalid arguments)
  3   Internal execution error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	dirFlag := fs.String("dir", ".", "Path to workspace repository root")
	fs.StringVar(dirFlag, "d", ".", "Path to workspace repository root (shorthand)")

	jsonOutput := fs.Bool("json", false, "Output results as JSON")

	orderedArgs := reorderArgs(fs, subArgs)
	if err := fs.Parse(orderedArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return exitErrorf(ExitCodeUsage, "%w", err)
	}

	if *unitID == "" && fs.NArg() > 0 {
		*unitID = fs.Arg(0)
	}

	if *unitID == "" {
		return exitErrorf(ExitCodeUsage, "flag --unit (-u) is required")
	}

	absDir, err := filepath.Abs(*dirFlag)
	if err != nil {
		return exitErrorf(ExitCodeUsage, "failed to resolve directory path: %w", err)
	}

	ctx := context.Background()
	in := cutover.Input{
		UnitID: *unitID,
		Action: action,
		Dir:    absDir,
	}

	out, err := cutover.Run(ctx, in)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "cutover error: %w", err)
	}

	var buf bytes.Buffer
	if *jsonOutput {
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return exitErrorf(ExitCodeInternal, "failed to serialize json output: %w", err)
		}
		buf.Write(data)
		buf.WriteString("\n")
	} else {
		formatCutoverOutput(&buf, out)
	}

	_, _ = io.Copy(stdout, &buf)
	return nil
}

func formatCutoverOutput(w io.Writer, out *cutover.Output) {
	fmt.Fprintf(w, "go-anything cutover: unit %q (action: %s)\n", out.UnitID, out.Action)
	fmt.Fprintf(w, "Route State:        %s\n", out.RouteState)
	fmt.Fprintf(w, "Kill Switch Active: %t\n", out.KillSwitchActive)
	if out.TargetService != "" {
		fmt.Fprintf(w, "Target Service:     %s\n", out.TargetService)
	}
	fmt.Fprintf(w, "Automatic Fallbacks:%d\n", out.FallbackCount)

	if len(out.Warnings) > 0 {
		fmt.Fprintln(w, "\nWarnings:")
		for _, warn := range out.Warnings {
			fmt.Fprintf(w, "  - %s\n", warn)
		}
	}

	if out.Message != "" {
		fmt.Fprintf(w, "\n%s\n", out.Message)
	}
}
