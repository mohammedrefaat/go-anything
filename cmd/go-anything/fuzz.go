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

	"github.com/mohammedrefaat/go-anything/internal/app/fuzz"
)

func runFuzz(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("fuzz", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything fuzz [flags]

Execute differential fuzzing against recorded cases and target implementation.

Flags:
  --unit, -u <id>        Migration unit identifier (required)
  --time, -t <duration>  Maximum fuzzing duration (e.g. 10s, 1m)
  --runs, -r <N>         Maximum fuzzing runs/iterations (default: 100)
  --seed <int64>         Random seed for deterministic fuzz mutations
  --max-input-bytes <N>  Maximum mutated payload size in bytes (default: 4096)
  --promote-findings     Append discovered divergences to .go-anything/cases/<unit>.jsonl
  --target <url|bin>     Target executable or service base URL (optional)
  --dir, -d <path>       Path to workspace repository root (default: '.')
  --json                 Output fuzz report as JSON
  --help, -h             Show this help message

Exit Codes:
  0   Success / 0 divergences discovered
  1   Divergence detected (Rule R-3: divergence blocks gate)
  2   Usage error (invalid flags or missing required arguments)
  3   Internal fuzzing error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	fuzzTime := fs.Duration("time", 0, "Maximum fuzzing duration")
	fs.DurationVar(fuzzTime, "t", 0, "Maximum fuzzing duration (shorthand)")

	runs := fs.Int("runs", 100, "Maximum fuzzing runs")
	fs.IntVar(runs, "r", 100, "Maximum fuzzing runs (shorthand)")

	seed := fs.Int64("seed", 0, "Random seed for mutations")

	maxBytes := fs.Int("max-input-bytes", 4096, "Maximum mutated input payload size in bytes")

	promoteFindings := fs.Bool("promote-findings", false, "Append discovered divergences to corpus")

	target := fs.String("target", "", "Target executable or service base URL")

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
	in := fuzz.Input{
		UnitID:          *unitID,
		Time:            *fuzzTime,
		Runs:            *runs,
		Seed:            *seed,
		MaxInputBytes:   *maxBytes,
		PromoteFindings: *promoteFindings,
		Dir:             absDir,
		Target:          *target,
	}

	out, err := fuzz.Run(ctx, in)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "fuzz error: %w", err)
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
		formatFuzzTable(&buf, out)
	}

	_, _ = io.Copy(stdout, &buf)

	if !out.Passed || out.DivergencesCount > 0 {
		return exitErrorf(ExitCodeFail, "%s", out.Summary)
	}

	return nil
}

func formatFuzzTable(w io.Writer, out *fuzz.Output) {
	fmt.Fprintf(w, "go-anything fuzz: unit %q\n", out.UnitID)
	fmt.Fprintf(w, "Total Runs:        %d\n", out.TotalRuns)
	fmt.Fprintf(w, "Duration:          %.2fms\n", out.DurationMS)
	fmt.Fprintf(w, "Divergences:       %d\n", out.DivergencesCount)
	fmt.Fprintf(w, "Promoted Cases:    %d\n", out.PromotedCount)
	if out.Passed {
		fmt.Fprintf(w, "Status:            PASS\n")
	} else {
		fmt.Fprintf(w, "Status:            FAIL (divergences detected)\n")
	}

	if out.DivergencesCount > 0 {
		fmt.Fprintf(w, "\nDiscovered Divergences (%d):\n", out.DivergencesCount)
		for _, d := range out.Divergences {
			fmt.Fprintf(w, "  [DIVERGENCE] Case %s:\n", d.CaseID)
			fmt.Fprintf(w, "    - Input:         %v\n", d.Input)
			fmt.Fprintf(w, "    - Legacy Output: %v\n", d.LegacyOutput)
			fmt.Fprintf(w, "    - Modern Output: %v\n", d.ModernOutput)
			if d.Diff != nil {
				fmt.Fprintf(w, "    - Diff:          %v\n", d.Diff)
			}
		}
	}

	if out.PromotedCount > 0 {
		fmt.Fprintf(w, "\nPromoted Findings to Corpus (%s):\n", out.CorpusPath)
		for _, c := range out.PromotedCases {
			fmt.Fprintf(w, "  + %s\n", c)
		}
	}
	fmt.Fprintln(w)
}
