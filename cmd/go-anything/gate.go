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
	"text/tabwriter"

	"github.com/mohammedrefat/go-anything/internal/app/gate"
)

func runGate(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything gate [flags]

Execute the Quality Gate pipeline against a migration unit and verify evidence.

Flags:
  --unit, -u <id>              Migration unit identifier (required)
  --profile, -p <profile>      Evidence profile ('replay' or 'full', default: '%s')
  --allow-missing-tools        Allow skipping checks when tools (e.g. staticcheck) are not installed
  --scorecard, -s              Generate markdown scorecard report in .go-anything/reports/
  --dir, -d <path>             Path to workspace repository root (default: '.')
  --json                       Output gate results as JSON
  --help, -h                   Show this help message

Exit Codes:
  0   Success / All quality checks passed
  1   Quality gate check failure
  2   Usage error (missing required flags or invalid arguments)
  3   Internal execution error
`, global.Profile)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	profile := fs.String("profile", global.Profile, "Evidence profile ('replay' or 'full')")
	fs.StringVar(profile, "p", global.Profile, "Evidence profile (shorthand)")

	allowMissingTools := fs.Bool("allow-missing-tools", false, "Allow missing linter/tool checks to be skipped")

	scorecard := fs.Bool("scorecard", false, "Generate scorecard report")
	fs.BoolVar(scorecard, "s", false, "Generate scorecard report (shorthand)")

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

	// Positional arguments fallback for unit ID: [unit]
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
	in := gate.Input{
		UnitID:            *unitID,
		Profile:           *profile,
		AllowMissingTools: *allowMissingTools,
		Dir:               absDir,
		Scorecard:         *scorecard,
	}

	out, err := gate.Run(ctx, in)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "gate error: %w", err)
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
		formatGateOutput(&buf, out)
	}

	_, _ = io.Copy(stdout, &buf)

	if !out.Passed {
		return exitErrorf(ExitCodeFail, "%s", out.Summary)
	}

	return nil
}

func formatGateOutput(w io.Writer, out *gate.Output) {
	status := "PASSED"
	if !out.Passed {
		status = "FAILED"
	}

	fmt.Fprintf(w, "go-anything gate: unit %q (profile: %s) - %s\n", out.UnitID, out.Profile, status)
	if out.CommitSHA != "" {
		fmt.Fprintf(w, "Commit SHA: %s\n", out.CommitSHA)
	}
	fmt.Fprintf(w, "Duration:   %.2fms\n\n", out.DurationMS)

	fmt.Fprintln(w, "Quality Checks:")
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "  Check\tStatus\tDuration")
	for _, c := range out.Checks {
		tag := "[PASS]"
		switch strings.ToLower(c.Status) {
		case "fail":
			tag = "[FAIL]"
		case "skipped":
			tag = "[SKIP]"
		}
		fmt.Fprintf(tw, "  %s %s\t%s\t%.2fms\n", tag, c.Name, c.Status, c.DurationMS)
	}
	tw.Flush()
	fmt.Fprintln(w)

	hasIssues := false
	for _, c := range out.Checks {
		if len(c.Issues) > 0 {
			if !hasIssues {
				fmt.Fprintln(w, "Issues:")
				hasIssues = true
			}
			fmt.Fprintf(w, "  Check %q:\n", c.Name)
			for _, issue := range c.Issues {
				fmt.Fprintf(w, "    - %s\n", issue)
			}
		}
	}
	if hasIssues {
		fmt.Fprintln(w)
	}

	if out.ScorecardPath != "" {
		fmt.Fprintf(w, "Scorecard generated at: %s\n", out.ScorecardPath)
	}
	if out.EvidenceSaved {
		fmt.Fprintf(w, "Evidence successfully saved tied to commit %s\n", out.CommitSHA)
	}

	fmt.Fprintln(w, out.Summary)
}
