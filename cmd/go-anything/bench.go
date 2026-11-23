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

	"github.com/mohammedrefat/go-anything/internal/app/bench"
)

func runBench(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything bench [flags]

Execute latency, throughput, and memory allocation benchmarks comparing original and Go implementations.

Flags:
  --unit, -u <id>        Migration unit identifier (required)
  --iterations, -n <N>   Number of measured benchmark iterations (default: 100)
  --warmup <N>           Number of unmeasured warmup iterations (default: 10)
  --target, -t <url|bin> Target executable or service base URL (optional)
  --dir, -d <path>       Path to workspace repository root (default: '.')
  --json                 Output benchmark report as JSON
  --help, -h             Show this help message

Exit Codes:
  0   Success / Benchmark executed successfully
  2   Usage error (invalid flags or missing required arguments)
  3   Internal benchmarking error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	iterations := fs.Int("iterations", 100, "Number of measured benchmark iterations")
	fs.IntVar(iterations, "n", 100, "Number of measured benchmark iterations (shorthand)")

	warmup := fs.Int("warmup", 10, "Number of unmeasured warmup iterations")

	target := fs.String("target", "", "Target executable or service base URL")
	fs.StringVar(target, "t", "", "Target executable or service base URL (shorthand)")

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
	in := bench.Input{
		UnitID:     *unitID,
		Iterations: *iterations,
		Warmup:     *warmup,
		Target:     *target,
		Dir:        absDir,
	}

	out, err := bench.Run(ctx, in)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "bench error: %w", err)
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
		formatBenchMarkdown(&buf, out)
	}

	_, _ = io.Copy(stdout, &buf)
	return nil
}

func formatBenchMarkdown(w io.Writer, out *bench.Output) {
	fmt.Fprintf(w, "# Benchmark Report: unit `%s`\n\n", out.UnitID)
	fmt.Fprintf(w, "**Summary:** %s\n\n", out.Summary)
	fmt.Fprintf(w, "**Timestamp:** %s\n\n", out.Timestamp)

	fmt.Fprintf(w, "## Performance Comparison\n\n")
	origRuntime := out.Environment.OriginalRuntime
	if origRuntime == "" {
		origRuntime = "Original"
	}
	fmt.Fprintf(w, "| Metric | %s | Ported Go | Speedup Ratio |\n", origRuntime)
	fmt.Fprintf(w, "| :--- | :--- | :--- | :--- |\n")
	fmt.Fprintf(w, "| **p50 Latency** | %.0f ns | %.0f ns | **%.2fx** |\n", out.Original.P50NS, out.Go.P50NS, out.SpeedupRatio)
	fmt.Fprintf(w, "| **p95 Latency** | %.0f ns | %.0f ns | - |\n", out.Original.P95NS, out.Go.P95NS)
	fmt.Fprintf(w, "| **p99 Latency** | %.0f ns | %.0f ns | - |\n", out.Original.P99NS, out.Go.P99NS)
	fmt.Fprintf(w, "| **Mean Latency** | %.0f ns | %.0f ns | - |\n", out.Original.MeanNS, out.Go.MeanNS)
	fmt.Fprintf(w, "| **Throughput** | %.1f ops/sec | %.1f ops/sec | - |\n\n", out.Original.ThroughputOpsSec, out.Go.ThroughputOpsSec)

	fmt.Fprintf(w, "## Memory Allocations (Go Implementation)\n\n")
	fmt.Fprintf(w, "- **Allocs / Op:** %d\n", out.Go.AllocsPerOp)
	fmt.Fprintf(w, "- **Bytes / Op:**  %d B\n\n", out.Go.BytesPerOp)

	fmt.Fprintf(w, "## Execution Environment & Reproducibility Parameters\n\n")
	fmt.Fprintf(w, "| Parameter | Value |\n")
	fmt.Fprintf(w, "| :--- | :--- |\n")
	fmt.Fprintf(w, "| **CPU Model** | %s |\n", out.Environment.CPUModel)
	fmt.Fprintf(w, "| **Operating System** | %s |\n", out.Environment.OS)
	fmt.Fprintf(w, "| **Go Version** | %s |\n", out.Environment.GoVersion)
	fmt.Fprintf(w, "| **Original Runtime** | %s |\n", out.Environment.OriginalRuntime)
	fmt.Fprintf(w, "| **CPU Cores** | %d |\n", out.Environment.Cores)
	fmt.Fprintf(w, "| **Measured Iterations** | %d |\n", out.Iterations)
	fmt.Fprintf(w, "| **Warmup Iterations** | %d |\n\n", out.Warmup)
}
