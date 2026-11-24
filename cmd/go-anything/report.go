package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mohammedrefat/go-anything/internal/app/companion"
)

func runReport(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything report [flags]

Generate comprehensive Markdown/JSON migration scorecard report for a unit.

Flags:
  --unit, -u <id>          Migration unit identifier (required)
  --dir, -d <path>         Path to workspace repository root (default: '.')
  --out, -o <file>         Output report file path (default: stdout)
  --format <fmt>           Output format: 'markdown' or 'json' (default: 'markdown')
  --json                   Output report as JSON (alias for --format=json)
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

	outFile := fs.String("out", "", "Output report file path")
	fs.StringVar(outFile, "o", "", "Output file path (shorthand)")

	formatFlag := fs.String("format", "markdown", "Output format: 'markdown' or 'json'")
	jsonOutput := fs.Bool("json", false, "Output report as JSON")

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

	format := *formatFlag
	if *jsonOutput {
		format = "json"
	}

	uc := companion.New()
	out, err := uc.Report(context.Background(), companion.ReportInput{
		UnitID: *unitID,
		Dir:    *dirPath,
		Format: format,
	})
	if err != nil {
		return exitErrorf(ExitCodeInternal, "%w", err)
	}

	var content string
	if format == "json" {
		data, _ := json.MarshalIndent(out, "", "  ")
		content = string(data)
	} else {
		content = out.Markdown
	}

	if *outFile != "" {
		if err := os.WriteFile(*outFile, []byte(content), 0644); err != nil {
			return exitErrorf(ExitCodeInternal, "failed to write report to %q: %w", *outFile, err)
		}
		fmt.Fprintf(stdout, "Report saved to %s\n", *outFile)
		return nil
	}

	fmt.Fprintln(stdout, content)
	return nil
}
