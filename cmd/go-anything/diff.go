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

	"github.com/mohammedrefaat/go-anything/internal/app/diff"
)

func runDiff(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything diff [flags]

Replay recorded behavioral test cases against ported target and report diffs.

Flags:
  --unit, -u <id>      Migration unit identifier (required)
  --target, -t <url>   Target executable or HTTP base URL of ported Go service (required)
  --mode, -m <mode>    Comparator mode ('exact', 'structural_json', 'http_contract', 'numeric_tolerance', 'effect_multiset')
  --jobs, -j <N>       Parallel replay workers (default: runtime.NumCPU())
  --case, -c <id>      Filter and test a single specific case ID
  --dir, -d <path>     Path to workspace repository root (default: '.')
  --json               Output comparison results as JSON
  --help, -h           Show this help message

Exit Codes:
  0   Success / 100%% replay match
  1   Diff mismatch detected or replay test failure
  2   Usage error (missing required flags or invalid arguments)
  3   Internal replay or execution error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	target := fs.String("target", "", "Target executable or HTTP base URL (required)")
	fs.StringVar(target, "t", "", "Target executable or HTTP base URL (shorthand)")

	mode := fs.String("mode", "", "Comparator mode")
	fs.StringVar(mode, "m", "", "Comparator mode (shorthand)")

	jobs := fs.Int("jobs", global.Jobs, "Parallel replay workers")
	fs.IntVar(jobs, "j", global.Jobs, "Parallel replay workers (shorthand)")

	caseFilter := fs.String("case", "", "Filter and debug a single specific case ID")
	fs.StringVar(caseFilter, "c", "", "Filter case ID (shorthand)")

	dirFlag := fs.String("dir", ".", "Path to workspace repository root")
	fs.StringVar(dirFlag, "d", ".", "Path to workspace repository root (shorthand)")

	jsonOutput := fs.Bool("json", false, "Output results as JSON")

	orderedArgs := reorderArgs(fs, args)
	if err := fs.Parse(orderedArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return exitErrorf(ExitCodeUsage, "%w", err)
	}

	// Positional arguments fallback for convenience: [unit] [target]
	if *unitID == "" && fs.NArg() > 0 {
		*unitID = fs.Arg(0)
	}
	if *target == "" && fs.NArg() > 1 {
		*target = fs.Arg(1)
	}

	if *unitID == "" {
		return exitErrorf(ExitCodeUsage, "flag --unit (-u) is required")
	}
	if *target == "" {
		return exitErrorf(ExitCodeUsage, "flag --target (-t) is required")
	}

	absDir, err := filepath.Abs(*dirFlag)
	if err != nil {
		return exitErrorf(ExitCodeUsage, "failed to resolve directory path: %w", err)
	}

	ctx := context.Background()
	in := diff.Input{
		UnitID:     *unitID,
		Target:     *target,
		Mode:       *mode,
		Jobs:       *jobs,
		Dir:        absDir,
		CaseFilter: *caseFilter,
	}

	out, err := diff.Run(ctx, in)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "diff error: %w", err)
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
		formatDiffOutput(&buf, out)
	}

	_, _ = io.Copy(stdout, &buf)

	if !out.Passed || out.Mismatched > 0 {
		return exitErrorf(ExitCodeFail, "%s", out.Summary)
	}

	return nil
}

func formatDiffOutput(w io.Writer, out *diff.Output) {
	fmt.Fprintf(w, "go-anything diff: unit %q vs target %q\n", out.UnitID, out.Target)
	if out.Mode != "" {
		fmt.Fprintf(w, "Mode: %s\n", out.Mode)
	}
	fmt.Fprintf(w, "Total Cases: %d | Matched: %d | Mismatched: %d\n\n", out.TotalCases, out.Matched, out.Mismatched)

	if out.Mismatched > 0 {
		fmt.Fprintf(w, "Mismatches Detected (%d):\n", out.Mismatched)
		for _, m := range out.Mismatches {
			fmt.Fprintf(w, "  [FAIL] Case %s: %s\n", m.CaseID, m.Message)
			for _, d := range m.Diffs {
				path := d.Path
				if path == "" {
					path = "output"
				}
				fmt.Fprintf(w, "    - Path: %s\n", path)
				if d.Message != "" {
					fmt.Fprintf(w, "      Reason: %s\n", d.Message)
				}
				if d.Expected != nil {
					fmt.Fprintf(w, "      Expected: %v\n", d.Expected)
				}
				if d.Actual != nil {
					fmt.Fprintf(w, "      Actual:   %v\n", d.Actual)
				}
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, out.Summary)
}
