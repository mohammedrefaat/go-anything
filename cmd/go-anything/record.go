package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammedrefat/go-anything/internal/recorder"
	"github.com/mohammedrefat/go-anything/internal/recorder/fromtests"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

func runRecord(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, `Usage:
  go-anything record [flags]
  go-anything record --unit <id> --from-tests [flags] -- <test-command> [args...]

Record baseline behavioral test cases in sandbox, from commands, HTTP endpoints, or test suites.

Flags:
  --unit, -u <id>        Migration unit identifier (required)
  --from-tests           Record behavioral cases by executing unit test suites
  --max-cases <N>        Maximum cases to ingest from tests (default: 500)
  --cmd, -c <cmd>        CLI command to execute and record
  --url <url>            HTTP endpoint to query and record
  --curl <cmd>           Curl command string to ingest and record
  --kind, -k <kind>      Case category: 'happy', 'edge', 'trap', 'fuzz', 'captured', 'from-test' (default: 'happy')
  --dir, -d <path>       Path to workspace repository root (default: '.')
  --help, -h             Show this help message

Exit Codes:
  0   Success / OK
  2   Usage error (invalid flags or missing required arguments)
  3   Internal recording error
`)
	}

	unitID := fs.String("unit", "", "Migration unit identifier to record cases for (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	fromTests := fs.Bool("from-tests", false, "Record behavioral cases by executing unit test suites")
	maxCases := fs.Int("max-cases", 500, "Maximum number of recorded cases to ingest from test run")

	cmdStr := fs.String("cmd", "", "CLI command to record (e.g. 'git status' or 'python app.py')")
	fs.StringVar(cmdStr, "c", "", "CLI command to record (shorthand)")

	curlStr := fs.String("curl", "", "curl command string to ingest and record against running service")
	urlStr := fs.String("url", "", "HTTP URL target to execute and record")
	kindStr := fs.String("kind", "happy", "Case category: 'happy', 'edge', 'trap', 'fuzz', 'captured', 'from-test'")
	fs.StringVar(kindStr, "k", "happy", "Case category (shorthand)")

	dirPath := fs.String("dir", ".", "Path to workspace repository root")
	fs.StringVar(dirPath, "d", ".", "Path to workspace repository root (shorthand)")

	// Split arguments at standard '--' separator for test execution commands
	var flagArgs []string
	var testCmdArgs []string
	for i, a := range args {
		if a == "--" {
			flagArgs = args[:i]
			testCmdArgs = args[i+1:]
			break
		}
	}
	if testCmdArgs == nil {
		flagArgs = args
	}

	orderedArgs := reorderArgs(fs, flagArgs)
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

	absPath, err := filepath.Abs(*dirPath)
	if err != nil {
		return exitErrorf(ExitCodeUsage, "failed to resolve directory path: %w", err)
	}

	store := state.NewStore(absPath)
	if _, err := store.GetManifest(); err != nil {
		return exitErrorf(ExitCodeInternal, "workspace not found at %s: run 'go-anything init' first", absPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	recordedCount := 0
	var lastResult *recorder.RecordResult

	switch {
	case *fromTests:
		if len(testCmdArgs) == 0 {
			return exitErrorf(ExitCodeUsage, "no test command specified: usage 'go-anything record --unit <id> --from-tests -- <test-command>'")
		}

		// Create temporary capture directory for test hooks
		tempCaptureDir, err := os.MkdirTemp("", "ga-record-capture-*")
		if err != nil {
			return exitErrorf(ExitCodeInternal, "failed to create capture temp directory: %w", err)
		}
		defer os.RemoveAll(tempCaptureDir)

		// Set up execution environment
		env := append([]string{}, os.Environ()...)
		env = append(env,
			"GO_ANYTHING_RECORD="+tempCaptureDir,
			"GO_ANYTHING_UNIT="+*unitID,
		)

		// Inject pytest plugin if available
		repoRoot := findRepoRoot()
		pluginDir := filepath.Join(repoRoot, "adapters", "source", "python", "plugins")
		if fi, err := os.Stat(pluginDir); err == nil && fi.IsDir() {
			existingPP := os.Getenv("PYTHONPATH")
			if existingPP != "" {
				env = append(env, "PYTHONPATH="+pluginDir+string(os.PathListSeparator)+existingPP)
			} else {
				env = append(env, "PYTHONPATH="+pluginDir)
			}
			existingOpt := os.Getenv("PYTEST_ADDOPTS")
			if existingOpt != "" {
				env = append(env, "PYTEST_ADDOPTS=-p go_anything_pytest "+existingOpt)
			} else {
				env = append(env, "PYTEST_ADDOPTS=-p go_anything_pytest")
			}
		}

		testCmd := exec.CommandContext(ctx, testCmdArgs[0], testCmdArgs[1:]...)
		testCmd.Dir = absPath
		testCmd.Env = env
		testCmd.Stdout = stdout
		testCmd.Stderr = stderr
		_ = testCmd.Run() // Run tests; ingest captured observations regardless of test exit status

		manifest, _ := store.GetManifest()
		runtimeLang := "test"
		if manifest != nil && manifest.SourceLanguage != "" {
			runtimeLang = manifest.SourceLanguage
		}

		ingester := fromtests.NewIngester(fromtests.IngestConfig{
			UnitID:        *unitID,
			CaptureDir:    tempCaptureDir,
			WorkspaceRoot: absPath,
			MaxCases:      *maxCases,
			Runtime:       runtimeLang,
		})

		res, err := ingester.Ingest(ctx)
		if err != nil {
			return exitErrorf(ExitCodeInternal, "failed to ingest test cases: %w", err)
		}
		recordedCount = len(res.SavedCases)
		fmt.Fprintf(stdout, "Recorded %d case(s) from tests (deduplicated: %d, capped: %d) saved to %s\n",
			recordedCount, res.Deduplicated, res.Capped, res.CasesFile)

	case *cmdStr != "":
		kind := adapterkit.CaseKind(*kindStr)
		rec := recorder.New(recorder.Config{
			WorkspaceRoot: absPath,
			Store:         store,
		})
		tokens, err := recorder.SplitTokens(*cmdStr)
		if err != nil {
			return exitErrorf(ExitCodeUsage, "failed to parse command line: %w", err)
		}
		if len(tokens) == 0 {
			return exitErrorf(ExitCodeUsage, "empty command specified")
		}
		command := tokens[0]
		cmdArgs := tokens[1:]

		res, err := rec.RecordCommand(ctx, *unitID, command, cmdArgs, kind)
		if err != nil {
			return exitErrorf(ExitCodeInternal, "command recording failed: %w", err)
		}
		recordedCount++
		lastResult = res

	case *curlStr != "":
		kind := adapterkit.CaseKind(*kindStr)
		rec := recorder.New(recorder.Config{
			WorkspaceRoot: absPath,
			Store:         store,
		})
		input, err := recorder.ParseCurl(*curlStr)
		if err != nil {
			return exitErrorf(ExitCodeUsage, "failed to parse curl command: %w", err)
		}
		execFn := createHTTPExecutionFunc(input.Path)
		res, err := rec.RecordCase(ctx, *unitID, *input, kind, execFn)
		if err != nil {
			return exitErrorf(ExitCodeInternal, "HTTP recording failed: %w", err)
		}
		recordedCount++
		lastResult = res

	case *urlStr != "":
		kind := adapterkit.CaseKind(*kindStr)
		rec := recorder.New(recorder.Config{
			WorkspaceRoot: absPath,
			Store:         store,
		})
		input := adapterkit.CaseInput{
			Method: "GET",
			Path:   *urlStr,
		}
		execFn := createHTTPExecutionFunc(*urlStr)
		res, err := rec.RecordCase(ctx, *unitID, input, kind, execFn)
		if err != nil {
			return exitErrorf(ExitCodeInternal, "HTTP recording failed: %w", err)
		}
		recordedCount++
		lastResult = res

	default:
		return exitErrorf(ExitCodeUsage, "must specify --cmd, --curl, --url, or --from-tests to record")
	}

	// Update unit lifecycle: if Planned, advance to Recorded
	u, err := store.GetUnit(*unitID)
	if err == nil && u.State == adapterkit.StatePlanned {
		_, _ = store.Transition(*unitID, adapterkit.StateRecorded, state.TransitionOptions{})
	}

	if !*fromTests {
		fmt.Fprintf(stdout, "Recorded %d case(s) for unit %q into .go-anything/cases/%s.jsonl\n", recordedCount, *unitID, *unitID)
		if lastResult != nil && len(lastResult.Wildcards) > 0 {
			fmt.Fprintf(stdout, "Detected %d volatile field(s) merged into .go-anything/rules.json\n", len(lastResult.Wildcards))
		}
	}

	return nil
}

func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}

func createHTTPExecutionFunc(targetURL string) recorder.ExecutionFunc {
	client := &http.Client{Timeout: 10 * time.Second}
	return func(ctx context.Context, in adapterkit.CaseInput) (*adapterkit.CaseOutput, error) {
		method := in.Method
		if method == "" {
			method = "GET"
		}
		urlToCall := targetURL
		if in.Path != "" && strings.HasPrefix(in.Path, "http") {
			urlToCall = in.Path
		}

		var bodyReader io.Reader
		if in.Body != nil {
			switch b := in.Body.(type) {
			case string:
				bodyReader = strings.NewReader(b)
			case []byte:
				bodyReader = bytes.NewReader(b)
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, urlToCall, bodyReader)
		if err != nil {
			return nil, err
		}
		for k, v := range in.Headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		headers := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		out := &adapterkit.CaseOutput{
			StatusCode: resp.StatusCode,
			Headers:    headers,
			Body:       string(respBytes),
		}
		return out, nil
	}
}
