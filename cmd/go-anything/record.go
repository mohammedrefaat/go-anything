package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammedrefat/go-anything/internal/recorder"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

func runRecord(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(stderr)

	unitID := fs.String("unit", "", "Migration unit identifier to record cases for (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	cmdStr := fs.String("cmd", "", "CLI command to record (e.g. 'git status' or 'python app.py')")
	fs.StringVar(cmdStr, "c", "", "CLI command to record (shorthand)")

	curlStr := fs.String("curl", "", "curl command string to ingest and record against running service")
	urlStr := fs.String("url", "", "HTTP URL target to execute and record")
	kindStr := fs.String("kind", "happy", "Case category: 'happy', 'edge', 'trap', 'fuzz', 'captured'")
	fs.StringVar(kindStr, "k", "happy", "Case category (shorthand)")

	dirPath := fs.String("dir", ".", "Path to workspace repository root")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *unitID == "" {
		return fmt.Errorf("flag --unit (-u) is required")
	}

	absPath, err := filepath.Abs(*dirPath)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	store := state.NewStore(absPath)
	if _, err := store.GetManifest(); err != nil {
		return fmt.Errorf("workspace not found at %s: run 'go-anything init' first", absPath)
	}

	kind := adapterkit.CaseKind(*kindStr)
	rec := recorder.New(recorder.Config{
		WorkspaceRoot: absPath,
		Store:         store,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	recordedCount := 0
	var lastResult *recorder.RecordResult

	switch {
	case *cmdStr != "":
		tokens, err := recorder.SplitTokens(*cmdStr)
		if err != nil {
			return fmt.Errorf("failed to parse command line: %w", err)
		}
		if len(tokens) == 0 {
			return fmt.Errorf("empty command specified")
		}
		command := tokens[0]
		cmdArgs := tokens[1:]

		res, err := rec.RecordCommand(ctx, *unitID, command, cmdArgs, kind)
		if err != nil {
			return fmt.Errorf("command recording failed: %w", err)
		}
		recordedCount++
		lastResult = res

	case *curlStr != "":
		input, err := recorder.ParseCurl(*curlStr)
		if err != nil {
			return fmt.Errorf("failed to parse curl command: %w", err)
		}
		execFn := createHTTPExecutionFunc(input.Path)
		res, err := rec.RecordCase(ctx, *unitID, *input, kind, execFn)
		if err != nil {
			return fmt.Errorf("HTTP recording failed: %w", err)
		}
		recordedCount++
		lastResult = res

	case *urlStr != "":
		input := adapterkit.CaseInput{
			Method: "GET",
			Path:   *urlStr,
		}
		execFn := createHTTPExecutionFunc(*urlStr)
		res, err := rec.RecordCase(ctx, *unitID, input, kind, execFn)
		if err != nil {
			return fmt.Errorf("HTTP recording failed: %w", err)
		}
		recordedCount++
		lastResult = res

	default:
		return fmt.Errorf("must specify --cmd, --curl, or --url to record")
	}

	// Update unit lifecycle: if Planned, advance to Recorded
	u, err := store.GetUnit(*unitID)
	if err == nil && u.State == adapterkit.StatePlanned {
		_, _ = store.Transition(*unitID, adapterkit.StateRecorded, state.TransitionOptions{})
	}

	fmt.Fprintf(stdout, "Recorded %d case(s) for unit %q into .go-anything/cases/%s.jsonl\n", recordedCount, *unitID, *unitID)
	if lastResult != nil && len(lastResult.Wildcards) > 0 {
		fmt.Fprintf(stdout, "Detected %d volatile field(s) merged into .go-anything/rules.json\n", len(lastResult.Wildcards))
	}

	return nil
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
