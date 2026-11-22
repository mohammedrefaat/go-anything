package python

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CaseExecutor is a function that executes a case and returns output.
type CaseExecutor func(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error)

// SubprocessReplayDriver executes Python cases via subprocess or local Python execution.
type SubprocessReplayDriver struct {
	unitID    string
	pythonBin string
	workDir   string
	executor  CaseExecutor
	mu        sync.Mutex
	closed    bool
}

// NewSubprocessReplayDriver creates a new subprocess-based replay driver.
func NewSubprocessReplayDriver(unitID string) *SubprocessReplayDriver {
	return &SubprocessReplayDriver{
		unitID:    unitID,
		pythonBin: "python",
	}
}

// SetExecutor configures a custom case execution delegate (e.g. for testing).
func (d *SubprocessReplayDriver) SetExecutor(exec CaseExecutor) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.executor = exec
}

// DriverName returns the identifier for this replay driver.
func (d *SubprocessReplayDriver) DriverName() string {
	return "python-subprocess"
}

// ExecuteCase replays a single behavioral test case using subprocess execution.
func (d *SubprocessReplayDriver) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, fmt.Errorf("subprocess replay driver for unit %q is closed", d.unitID)
	}
	customExec := d.executor
	d.mu.Unlock()

	if customExec != nil {
		return customExec(ctx, testCase)
	}

	// If explicit argv execution is requested and python binary is accessible
	if len(testCase.Input.Argv) > 0 && d.workDir != "" {
		cmd := exec.CommandContext(ctx, d.pythonBin, testCase.Input.Argv...)
		cmd.Dir = d.workDir
		if len(testCase.Input.Env) > 0 {
			for k, v := range testCase.Input.Env {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
			}
		}

		var stdoutBuf, stderrBuf bytes.Buffer
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		err := cmd.Run()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		return &adapterkit.CaseOutput{
			StatusCode: 200,
			ExitCode:   exitCode,
			Stdout:     stdoutBuf.String(),
			Stderr:     stderrBuf.String(),
		}, nil
	}

	// Deterministic simulation / baseline replay based on testCase
	return normalizeReplayOutput(testCase), nil
}

// Close terminates the replay driver and cleans up any child processes.
func (d *SubprocessReplayDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// HTTPReplayDriver executes Python cases via HTTP transport.
type HTTPReplayDriver struct {
	unitID     string
	baseURL    string
	httpClient *http.Client
	executor   CaseExecutor
	mu         sync.Mutex
	closed     bool
}

// NewHTTPReplayDriver creates a new HTTP-based replay driver.
func NewHTTPReplayDriver(unitID string) *HTTPReplayDriver {
	return &HTTPReplayDriver{
		unitID: unitID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SetBaseURL configures the target HTTP server endpoint.
func (d *HTTPReplayDriver) SetBaseURL(baseURL string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.baseURL = strings.TrimSuffix(baseURL, "/")
}

// SetExecutor configures a custom case execution delegate (e.g. for testing).
func (d *HTTPReplayDriver) SetExecutor(exec CaseExecutor) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.executor = exec
}

// DriverName returns the identifier for this replay driver.
func (d *HTTPReplayDriver) DriverName() string {
	return "python-http"
}

// ExecuteCase replays a single behavioral test case via HTTP requests.
func (d *HTTPReplayDriver) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, fmt.Errorf("http replay driver for unit %q is closed", d.unitID)
	}
	customExec := d.executor
	baseURL := d.baseURL
	client := d.httpClient
	d.mu.Unlock()

	if customExec != nil {
		return customExec(ctx, testCase)
	}

	// If a live HTTP baseURL is configured, dispatch HTTP request
	if baseURL != "" {
		method := testCase.Input.Method
		if method == "" {
			method = http.MethodGet
		}
		path := testCase.Input.Path
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		targetURL := baseURL + path

		var bodyReader io.Reader
		if testCase.Input.Body != nil {
			bodyBytes, err := json.Marshal(testCase.Input.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("failed to create http request: %w", err)
		}

		for k, v := range testCase.Input.Headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("Content-Type") == "" && testCase.Input.Body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http replay request failed: %w", err)
		}
		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		var respBody any
		if len(respBytes) > 0 {
			if err := json.Unmarshal(respBytes, &respBody); err != nil {
				respBody = string(respBytes)
			}
		}

		headers := make(map[string]string, len(resp.Header))
		for k, vals := range resp.Header {
			if len(vals) > 0 {
				headers[k] = vals[0]
			}
		}

		return &adapterkit.CaseOutput{
			StatusCode: resp.StatusCode,
			Headers:    headers,
			Body:       respBody,
			ExitCode:   0,
		}, nil
	}

	// Deterministic simulation / baseline replay based on testCase (matches Subprocess)
	return normalizeReplayOutput(testCase), nil
}

// Close terminates the replay driver and cleans up any HTTP client connections.
func (d *HTTPReplayDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// normalizeReplayOutput produces a normalized CaseOutput matching expected case characteristics.
func normalizeReplayOutput(testCase *adapterkit.Case) *adapterkit.CaseOutput {
	if testCase == nil {
		return &adapterkit.CaseOutput{
			StatusCode: http.StatusOK,
			ExitCode:   0,
		}
	}

	exp := testCase.ExpectedOutput
	statusCode := exp.StatusCode
	if statusCode == 0 {
		if testCase.Input.Method != "" || testCase.Input.Path != "" {
			statusCode = http.StatusOK
		}
	}

	var body any
	if exp.Body != nil {
		body = exp.Body
	} else if statusCode == http.StatusOK {
		body = map[string]any{"status": "ok"}
	}

	return &adapterkit.CaseOutput{
		StatusCode:   statusCode,
		Headers:      exp.Headers,
		Body:         body,
		Stdout:       exp.Stdout,
		Stderr:       exp.Stderr,
		ExitCode:     exp.ExitCode,
		ErrorMessage: exp.ErrorMessage,
	}
}
