package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mohammedrefat/go-anything/internal/app/hotpath"
)

func runGo(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("go", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything go [flags]

Execute the end-to-end hot-path porting pipeline for a specific function or candidate.
Orchestrates: profile -> record -> bridge -> [pause for implementation] -> replay/diff -> fuzz/bench -> gate -> shadow -> prompt for human approve.

Flags:
  --unit, -u <id>          Migration unit identifier (required)
  --function, -f <name>    Function name to port (optional; auto-detected from profile if omitted)
  --dir, -d <path>         Path to workspace repository root (default: '.')
  --step <name>            Resume from or execute specific step
  --force                  Re-run completed pipeline steps
  --json                   Output pipeline execution report as JSON
  --help, -h               Show this help message

Exit Codes:
  0   Success / paused waiting for developer implementation / ready for approval
  1   Pipeline verification check failed (diff divergence, fuzz mismatch, gate failure)
  2   Usage error (invalid flags or missing required arguments)
  3   Internal execution error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	functionName := fs.String("function", "", "Function name to port")
	fs.StringVar(functionName, "f", "", "Function name (shorthand)")

	dirPath := fs.String("dir", ".", "Path to workspace repository root")
	fs.StringVar(dirPath, "d", ".", "Path to workspace repository root (shorthand)")

	stepName := fs.String("step", "", "Resume from or execute specific step")

	forceFlag := fs.Bool("force", false, "Re-run completed pipeline steps")

	jsonOutput := fs.Bool("json", false, "Output results as JSON")

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

	ctx := context.Background()
	uc := hotpath.New(nil)

	out, err := uc.Run(ctx, hotpath.Input{
		UnitID:   *unitID,
		Function: *functionName,
		Dir:      *dirPath,
		Step:     *stepName,
		Force:    *forceFlag,
	})

	if *jsonOutput {
		if out != nil {
			data, marshalErr := json.MarshalIndent(out, "", "  ")
			if marshalErr == nil {
				fmt.Fprintln(stdout, string(data))
			}
		}
		if err != nil {
			return exitErrorf(ExitCodeFail, "%w", err)
		}
		return nil
	}

	if out != nil {
		fmt.Fprintf(stdout, "go-anything hot-path orchestrator: %s (function: %s)\n", out.UnitID, out.Function)
		fmt.Fprintf(stdout, "Lifecycle State: %s | Pipeline Status: %s\n\n", out.UnitState, out.Status)

		for _, s := range out.Steps {
			var badge string
			switch s.Status {
			case hotpath.StepStatusCompleted:
				badge = "[OK]"
			case hotpath.StepStatusSkipped:
				badge = "[SKIP]"
			case hotpath.StepStatusWaiting:
				badge = "[PAUSE]"
			case hotpath.StepStatusFailed:
				badge = "[FAIL]"
			default:
				badge = "[....]"
			}
			fmt.Fprintf(stdout, "  %-7s %-12s - %s\n", badge, s.Name, s.Summary)
		}

		fmt.Println()
		if out.Status == hotpath.PipelineStatusPausedForImplementation {
			fmt.Fprintf(stdout, "-> Pipeline paused waiting for developer implementation:\n")
			fmt.Fprintf(stdout, "   %s\n", out.HumanActionRequired)
			return nil
		}

		if out.Status == hotpath.PipelineStatusReadyForApproval {
			fmt.Fprintf(stdout, "-> Pipeline completed through Shadow verification!\n")
			fmt.Fprintf(stdout, "-> Human sign-off required (Rule R-1):\n")
			fmt.Fprintf(stdout, "   Run: %s\n", out.HumanActionRequired)
			return nil
		}
	}

	if err != nil {
		return exitErrorf(ExitCodeFail, "hot-path pipeline failed: %w", err)
	}

	return nil
}
