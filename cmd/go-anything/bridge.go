package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammedrefaat/go-anything/internal/bridge"
	"github.com/mohammedrefaat/go-anything/internal/state"
)

func runBridge(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bridge", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything bridge [flags]

Generate hot-path strangler sidecar bridge code, JSON-RPC dispatcher, and client wrappers.

Flags:
  --unit, -u <id>          Migration unit identifier (required)
  --function, -f <name>    Function name to expose across the bridge
  --transport, -t <mode>   Transport protocol: 'stdio', 'unix', 'grpc' (default: 'stdio')
  --dir, -d <path>         Path to workspace repository root (default: '.')
  --out, -o <path>         Output directory for generated sidecar (default: '<dir>/bridges/<unit>')
  --manifest, -m <path>    Custom bridge.yaml manifest path
  --json                   Output generation result as JSON
  --help, -h               Show this help message

Exit Codes:
  0   Success / OK
  2   Usage error (invalid flags or missing required arguments)
  3   Internal code generation error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	functionName := fs.String("function", "", "Function name to expose across the bridge")
	fs.StringVar(functionName, "f", "", "Function name (shorthand)")

	transport := fs.String("transport", "stdio", "Transport protocol: 'stdio', 'unix', 'grpc'")
	fs.StringVar(transport, "t", "stdio", "Transport protocol (shorthand)")

	dirPath := fs.String("dir", ".", "Path to workspace repository root")
	fs.StringVar(dirPath, "d", ".", "Path to workspace repository root (shorthand)")

	outDir := fs.String("out", "", "Output directory for generated sidecar")
	fs.StringVar(outDir, "o", "", "Output directory (shorthand)")

	manifestPath := fs.String("manifest", "", "Custom bridge.yaml manifest path")
	fs.StringVar(manifestPath, "m", "", "Custom manifest path (shorthand)")

	jsonOutput := fs.Bool("json", false, "Output results as JSON")

	orderedArgs := reorderArgs(fs, args)
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

	absDir, err := filepath.Abs(*dirPath)
	if err != nil {
		return exitErrorf(ExitCodeUsage, "failed to resolve directory path: %w", err)
	}

	store := state.NewStore(absDir)
	_, _ = store.GetManifest()

	// Determine output directory
	targetOutDir := *outDir
	if targetOutDir == "" {
		targetOutDir = filepath.Join(absDir, "bridges", filepath.FromSlash(*unitID))
	} else if !filepath.IsAbs(targetOutDir) {
		targetOutDir = filepath.Join(absDir, targetOutDir)
	}

	// Resolve or construct Manifest
	var m *bridge.Manifest
	mPath := *manifestPath
	if mPath == "" {
		candidates := []string{
			filepath.Join(targetOutDir, "bridge.yaml"),
			filepath.Join(absDir, ".go-anything", "bridges", filepath.FromSlash(*unitID), "bridge.yaml"),
			filepath.Join(absDir, "bridge.yaml"),
		}
		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				mPath = cand
				break
			}
		}
	}

	if mPath != "" {
		var err error
		m, err = bridge.LoadManifest(mPath)
		if err != nil {
			return exitErrorf(ExitCodeInternal, "failed to load bridge manifest: %w", err)
		}
	} else {
		// Synthesize default manifest
		fnName := *functionName
		if fnName == "" {
			parts := strings.Split(*unitID, "/")
			fnName = parts[len(parts)-1]
		}
		serviceName := filepath.Base(*unitID) + "-bridge"
		m = &bridge.Manifest{
			Version: "v1alpha1",
			Unit:    *unitID,
			Service: serviceName,
			Transport: bridge.TransportConfig{
				Default: *transport,
			},
			Functions: []bridge.Function{
				{
					Name:        fnName,
					Description: fmt.Sprintf("Hot-path procedure for unit %s", *unitID),
					Idempotent:  true,
					TimeoutMS:   3000,
					Params: []bridge.Param{
						{Name: "payload", Type: "object", Required: true, Description: "Input parameters"},
					},
					Returns: bridge.ReturnsConfig{
						Type:        "object",
						Properties:  map[string]string{"result": "object"},
						Description: "Procedure execution outcome",
					},
					Errors: []bridge.ErrorConfig{
						{Code: -32001, Name: "ExecutionError", Description: "Internal execution failure"},
					},
				},
			},
		}

		_ = os.MkdirAll(targetOutDir, 0755)
		manifestSavePath := filepath.Join(targetOutDir, "bridge.yaml")
		if err := bridge.SaveManifest(manifestSavePath, m); err != nil {
			return exitErrorf(ExitCodeInternal, "failed to save synthesized manifest: %w", err)
		}
	}

	if *transport != "" && *transport != "stdio" {
		m.Transport.Default = *transport
	}

	gen := bridge.NewGenerator()
	res, err := gen.Generate(m, targetOutDir)
	if err != nil {
		return exitErrorf(ExitCodeInternal, "bridge generation failed: %w", err)
	}

	if *jsonOutput {
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return exitErrorf(ExitCodeInternal, "failed to serialize json output: %w", err)
		}
		var buf bytes.Buffer
		buf.Write(data)
		buf.WriteString("\n")
		_, _ = io.Copy(stdout, &buf)
		return nil
	}

	fmt.Fprintf(stdout, "go-anything bridge: generated sidecar bridge for unit %q\n", *unitID)
	fmt.Fprintf(stdout, "Output Directory: %s\n\n", res.OutputDir)
	fmt.Fprintf(stdout, "Generated Files (%d):\n", len(res.GeneratedFiles))
	for _, f := range res.GeneratedFiles {
		fmt.Fprintf(stdout, "  + %s\n", f)
	}
	if len(res.PreservedFiles) > 0 {
		fmt.Fprintf(stdout, "\nPreserved Existing Files (%d):\n", len(res.PreservedFiles))
		for _, f := range res.PreservedFiles {
			fmt.Fprintf(stdout, "  = %s (preserved: existing port code intact)\n", f)
		}
	}

	fmt.Fprintf(stdout, `
Next Steps:
  1. Implement your ported Go logic in:
     %s
  2. Build and run tests:
     cd %s && go test -v ./...
  3. Start the sidecar (JSON-RPC over stdio):
     cd %s && go run .
  4. Use wrapper client in your legacy application:
     Python:     import from wrapper.py
     TypeScript: import from wrapper.ts
`, filepath.Join(targetOutDir, "handlers_impl.go"), targetOutDir, targetOutDir)

	return nil
}
