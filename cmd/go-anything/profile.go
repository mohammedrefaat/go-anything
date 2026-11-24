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
	"text/tabwriter"

	"github.com/mohammedrefat/go-anything/internal/app/profile"
)

func runProfile(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("profile", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything profile [flags]

Profile legacy code execution to identify and rank compute-heavy candidate functions for Go modernization.

Flags:
  --unit, -u <id>    Migration unit identifier (required)
  --top <N>          Number of top candidate functions to display (default: 10)
  --dir, -d <path>   Path to workspace repository root (default: '.')
  --json             Output profile report as JSON
  --help, -h         Show this help message

Exit Codes:
  0   Success / Profiling completed successfully
  2   Usage error (invalid flags or missing required arguments)
  3   Internal profiling error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	top := fs.Int("top", 10, "Number of top candidate functions to display")

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

	// Positional arguments fallback: [unit]
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
	in := profile.Input{
		UnitID: *unitID,
		Top:    *top,
		Dir:    absDir,
	}

	out, err := profile.Run(ctx, in)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "profile error: %w", err)
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
		formatProfileTable(&buf, out)
	}

	_, _ = io.Copy(stdout, &buf)
	return nil
}

func formatProfileTable(w io.Writer, out *profile.Output) {
	fmt.Fprintf(w, "go-anything profile: unit %q\n", out.UnitID)
	fmt.Fprintf(w, "Language:        %s\n", out.Language)
	fmt.Fprintf(w, "Profiler:        %s\n", out.Profiler)
	fmt.Fprintf(w, "Duration:        %.2fms\n", out.TotalDurationMS)
	fmt.Fprintf(w, "Candidates:      %d (top %d)\n\n", len(out.Candidates), out.Top)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "Rank\tFunction\tFile:Line\tSelf %\tCumulative %\tCalls\tPure? (Heuristic)\tPurity Reason")
	fmt.Fprintln(tw, "----\t--------\t---------\t------\t------------\t-----\t-----------------\t-------------")
	for _, c := range out.Candidates {
		fileLoc := c.File
		if c.Line > 0 {
			fileLoc = fmt.Sprintf("%s:%d", c.File, c.Line)
		}
		pureStr := "No"
		if c.IsPureHeuristic {
			pureStr = "Yes"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%.1f%%\t%.1f%%\t%d\t%s\t%s\n",
			c.Rank, c.FunctionName, fileLoc, c.SelfPercent, c.CumulativePercent, c.CallCount, pureStr, c.PurityReason)
	}
	_ = tw.Flush()

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Notice: Purity assessment is a static/runtime heuristic and does not constitute a formal mathematical guarantee of side-effect freedom.")
	fmt.Fprintln(w)
}
