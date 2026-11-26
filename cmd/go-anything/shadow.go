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

	"github.com/mohammedrefaat/go-anything/internal/app/shadow"
)

func runShadow(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	action := "status"
	var subArgs []string

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		candidate := strings.ToLower(args[0])
		if candidate == "start" || candidate == "status" || candidate == "stop" {
			action = candidate
			subArgs = args[1:]
		} else {
			return exitErrorf(ExitCodeUsage, "invalid shadow action %q (must be start, status, or stop)", args[0])
		}
	} else {
		subArgs = args
	}

	fs := flag.NewFlagSet("shadow", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything shadow <action> [flags]

Manage and inspect shadow-mode traffic mirroring for a migration unit.

Actions:
  start     Start mirroring real traffic to the ported implementation
  status    Inspect shadow mode status, throughput, and mismatch counters (default)
  stop      Stop mirroring traffic

Flags:
  --unit, -u <id>              Migration unit identifier (required)
  --port, -p <port>            Target HTTP port for shadow service (default: 8080)
  --sample-rate, -s <rate>     Sampling rate fraction between 0.0 and 1.0 (default: 1.0)
  --dir, -d <path>             Path to workspace repository root (default: '.')
  --json                       Output shadow results as JSON
  --help, -h                   Show this help message

Exit Codes:
  0   Success / OK
  2   Usage error (invalid action, missing flags, or invalid arguments)
  3   Internal execution error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	port := fs.Int("port", 8080, "Target HTTP port for shadow service")
	fs.IntVar(port, "p", 8080, "Target HTTP port for shadow service (shorthand)")

	sampleRate := fs.Float64("sample-rate", 1.0, "Sampling rate fraction between 0.0 and 1.0")
	fs.Float64Var(sampleRate, "s", 1.0, "Sampling rate fraction (shorthand)")

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

	if *port < 0 || *port > 65535 {
		return exitErrorf(ExitCodeUsage, "invalid port %d: must be between 0 and 65535", *port)
	}

	if *sampleRate < 0.0 || *sampleRate > 1.0 {
		return exitErrorf(ExitCodeUsage, "invalid sample-rate %.2f: must be between 0.0 and 1.0", *sampleRate)
	}

	absDir, err := filepath.Abs(*dirFlag)
	if err != nil {
		return exitErrorf(ExitCodeUsage, "failed to resolve directory path: %w", err)
	}

	ctx := context.Background()
	in := shadow.Input{
		UnitID:     *unitID,
		Action:     action,
		Port:       *port,
		SampleRate: *sampleRate,
		Dir:        absDir,
	}

	out, err := shadow.Run(ctx, in)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "shadow error: %w", err)
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
		formatShadowOutput(&buf, out)
	}

	_, _ = io.Copy(stdout, &buf)
	return nil
}

func formatShadowOutput(w io.Writer, out *shadow.Output) {
	fmt.Fprintf(w, "go-anything shadow: unit %q (action: %s)\n", out.UnitID, out.Action)
	fmt.Fprintf(w, "Active:           %t\n", out.Active)
	fmt.Fprintf(w, "Port:             %d\n", out.Port)
	fmt.Fprintf(w, "Sample Rate:      %.2f\n", out.SampleRate)
	fmt.Fprintf(w, "Total Requests:   %d\n", out.RequestsTotal)
	fmt.Fprintf(w, "Shadowed:         %d\n", out.RequestsShadowed)
	fmt.Fprintf(w, "Overflow Dropped: %d\n", out.OverflowDropped)
	fmt.Fprintf(w, "Mismatches:       %d\n", out.MismatchesCount)

	if out.Summary != "" {
		fmt.Fprintf(w, "\n%s\n", out.Summary)
	}
}
