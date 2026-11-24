package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/mohammedrefat/go-anything/internal/app/analyze"
)

func runAnalyze(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything analyze [flags] [directory]

Analyze project dependencies and generate topological migration waves.

Flags:
  --source, -s <lang>  Source programming language (default: 'go')
  --json               Output analysis results as JSON
  --output, -o <file>  Write output to specified file path instead of stdout
  --dir, -d <path>     Directory path to project root (default: '.')
  --help, -h           Show this help message

Exit Codes:
  0   Success / OK
  2   Usage error (invalid flags or arguments)
  3   Internal analysis or planning error
`)
	}

	source := fs.String("source", "go", "Source programming language")
	fs.StringVar(source, "s", "go", "Source programming language (shorthand)")

	jsonOutput := fs.Bool("json", false, "Output analysis results as JSON")

	outputFile := fs.String("output", "", "Write output to specified file path")
	fs.StringVar(outputFile, "o", "", "Write output to specified file path (shorthand)")

	dirFlag := fs.String("dir", "", "Directory path to project root")
	fs.StringVar(dirFlag, "d", "", "Directory path to project root (shorthand)")

	orderedArgs := reorderArgs(fs, args)
	if err := fs.Parse(orderedArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return exitErrorf(ExitCodeUsage, "%w", err)
	}

	targetDir := "."
	if *dirFlag != "" {
		targetDir = *dirFlag
	} else if fs.NArg() > 0 {
		targetDir = fs.Arg(0)
	}

	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return exitErrorf(ExitCodeUsage, "failed to resolve directory path: %w", err)
	}

	ctx := context.Background()
	in := analyze.Input{
		Dir:    absDir,
		Source: *source,
		JSON:   *jsonOutput,
	}

	out, err := analyze.Run(ctx, in)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "analysis error: %w", err)
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
		formatAnalyzeTable(&buf, out)
	}

	if *outputFile != "" {
		if err := os.MkdirAll(filepath.Dir(*outputFile), 0755); err != nil {
			return exitErrorf(ExitCodeInternal, "failed to create output directory: %w", err)
		}
		if err := os.WriteFile(*outputFile, buf.Bytes(), 0644); err != nil {
			return exitErrorf(ExitCodeInternal, "failed to write output file: %w", err)
		}
		fmt.Fprintf(stdout, "Analysis output written to %s\n", *outputFile)
		return nil
	}

	_, err = io.Copy(stdout, &buf)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "failed to write output: %w", err)
	}
	return nil
}

func formatAnalyzeTable(w io.Writer, out *analyze.Output) {
	fmt.Fprintf(w, "go-anything analyze: %s (source: %s)\n", out.Project, out.SourceLanguage)
	fmt.Fprintf(w, "Total Units: %d | Max Wave: %d | Cycles: %d\n\n", out.TotalUnits, out.MaxWave, len(out.Cycles))

	if len(out.Cycles) > 0 {
		fmt.Fprintf(w, "WARNING: Dependency cycles detected:\n")
		for _, cycle := range out.Cycles {
			fmt.Fprintf(w, "  Cycle: %s\n", strings.Join(cycle, " -> "))
		}
		fmt.Fprintln(w)
	}

	if len(out.Waves) > 0 {
		fmt.Fprintln(w, "Topological Waves:")
		tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
		fmt.Fprintln(tw, "  Wave\tUnits")
		for _, wave := range out.Waves {
			fmt.Fprintf(tw, "  %d\t%s\n", wave.Wave, strings.Join(wave.UnitIDs, ", "))
		}
		tw.Flush()
		fmt.Fprintln(w)
	}

	if len(out.Units) > 0 {
		fmt.Fprintln(w, "Migration Units:")
		tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
		fmt.Fprintln(tw, "  Wave\tUnit ID\tDependencies\tRisk")
		for _, u := range out.Units {
			deps := "-"
			if len(u.Dependencies) > 0 {
				deps = strings.Join(u.Dependencies, ", ")
			}
			risk := "-"
			if u.Risk != "" {
				risk = u.Risk
			}
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\n", u.Wave, u.ID, deps, risk)
		}
		tw.Flush()
	}
}
