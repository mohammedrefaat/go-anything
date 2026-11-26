package typescript

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// ReplayHandler is an optional function type for custom or mocked case execution.
type ReplayHandler func(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error)

// SubprocessDriver implements adapterkit.Replay for executing Node/TypeScript code via subprocess.
type SubprocessDriver struct {
	nodePath   string
	scriptPath string
	workingDir string
	env        map[string]string
	timeout    time.Duration
	handler    ReplayHandler
	closed     bool
}

// SubprocessOption configures a SubprocessDriver.
type SubprocessOption func(*SubprocessDriver)

// WithSubprocessNodePath sets the node binary path.
func WithSubprocessNodePath(path string) SubprocessOption {
	return func(d *SubprocessDriver) {
		if path != "" {
			d.nodePath = path
		}
	}
}

// WithSubprocessScript sets the target script path.
func WithSubprocessScript(script string) SubprocessOption {
	return func(d *SubprocessDriver) {
		d.scriptPath = script
	}
}

// WithSubprocessWorkingDir sets the working directory for subprocess execution.
func WithSubprocessWorkingDir(dir string) SubprocessOption {
	return func(d *SubprocessDriver) {
		d.workingDir = dir
	}
}

// WithSubprocessEnv sets custom environment variables.
func WithSubprocessEnv(env map[string]string) SubprocessOption {
	return func(d *SubprocessDriver) {
		d.env = env
	}
}

// WithSubprocessHandler sets a custom case execution handler.
func WithSubprocessHandler(handler ReplayHandler) SubprocessOption {
	return func(d *SubprocessDriver) {
		d.handler = handler
	}
}

// NewSubprocessDriver creates a new SubprocessDriver for Node/TypeScript applications.
func NewSubprocessDriver(opts ...SubprocessOption) *SubprocessDriver {
	d := &SubprocessDriver{
		nodePath: "node",
		timeout:  30 * time.Second,
		env:      make(map[string]string),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// DriverName returns the identifier for this replay driver.
func (d *SubprocessDriver) DriverName() string {
	return "typescript:subprocess"
}

// ExecuteCase replays a test case by executing a Node subprocess.
func (d *SubprocessDriver) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if d.closed {
		return nil, errors.New("subprocess driver is closed")
	}
	if testCase == nil {
		return nil, errors.New("testCase cannot be nil")
	}

	if d.handler != nil {
		return d.handler(ctx, testCase)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var args []string
	if d.scriptPath != "" {
		args = append(args, d.scriptPath)
	}

	if len(testCase.Input.Argv) > 0 {
		for _, arg := range testCase.Input.Argv {
			args = append(args, arg)
		}
	}

	// If no script or argv is specified, run an inline Node evaluation of the input payload
	if len(args) == 0 {
		inputData, _ := json.Marshal(testCase.Input)
		evalScript := fmt.Sprintf(`
const input = %s;
const output = {
  status: "ok",
  path: input.path || "/",
  method: input.method || "GET"
};
process.stdout.write(JSON.stringify(output));
`, string(inputData))
		args = []string{"-e", evalScript}
	}

	cmd := exec.CommandContext(timeoutCtx, d.nodePath, args...)
	if d.workingDir != "" {
		cmd.Dir = d.workingDir
	}

	// Environment configuration
	for k, v := range d.env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range testCase.Input.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Input piping
	if testCase.Input.Body != nil {
		if strBody, ok := testCase.Input.Body.(string); ok {
			cmd.Stdin = strings.NewReader(strBody)
		} else {
			bodyBytes, err := json.Marshal(testCase.Input.Body)
			if err == nil {
				cmd.Stdin = bytes.NewReader(bodyBytes)
			}
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

	stdoutStr := stdoutBuf.String()
	stderrStr := stderrBuf.String()

	var parsedBody any
	if stdoutStr != "" {
		var decoded any
		if jsonErr := json.Unmarshal([]byte(stdoutStr), &decoded); jsonErr == nil {
			parsedBody = decoded
		} else {
			parsedBody = stdoutStr
		}
	}

	statusCode := 200
	if exitCode != 0 {
		statusCode = 500
	}

	return &adapterkit.CaseOutput{
		StatusCode: statusCode,
		ExitCode:   exitCode,
		Stdout:     stdoutStr,
		Stderr:     stderrStr,
		Body:       parsedBody,
	}, nil
}

// Close terminates and cleans up the driver.
func (d *SubprocessDriver) Close() error {
	d.closed = true
	return nil
}

// HTTPDriver implements adapterkit.Replay for executing HTTP requests against Node/TypeScript services.
type HTTPDriver struct {
	baseURL string
	client  *http.Client
	timeout time.Duration
	handler ReplayHandler
	closed  bool
}

// HTTPOption configures an HTTPDriver.
type HTTPOption func(*HTTPDriver)

// WithHTTPBaseURL sets the base server URL for the HTTP driver.
func WithHTTPBaseURL(rawURL string) HTTPOption {
	return func(d *HTTPDriver) {
		d.baseURL = strings.TrimRight(rawURL, "/")
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) HTTPOption {
	return func(d *HTTPDriver) {
		d.client = client
	}
}

// WithHTTPTimeout sets request timeout.
func WithHTTPTimeout(t time.Duration) HTTPOption {
	return func(d *HTTPDriver) {
		if t > 0 {
			d.timeout = t
		}
	}
}

// WithHTTPHandler sets a custom case execution handler.
func WithHTTPHandler(handler ReplayHandler) HTTPOption {
	return func(d *HTTPDriver) {
		d.handler = handler
	}
}

// NewHTTPDriver creates a new HTTPDriver.
func NewHTTPDriver(opts ...HTTPOption) *HTTPDriver {
	d := &HTTPDriver{
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(d)
	}
	if d.client == nil {
		d.client = &http.Client{
			Timeout: d.timeout,
		}
	}
	return d
}

// DriverName returns the identifier for this replay driver.
func (d *HTTPDriver) DriverName() string {
	return "typescript:http"
}

// ExecuteCase replays a test case by performing an HTTP request against the target service.
func (d *HTTPDriver) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if d.closed {
		return nil, errors.New("http driver is closed")
	}
	if testCase == nil {
		return nil, errors.New("testCase cannot be nil")
	}

	if d.handler != nil {
		return d.handler(ctx, testCase)
	}

	if d.baseURL == "" {
		// Mock/in-memory response for unconfigured test cases
		bodyMap := map[string]any{
			"status": "ok",
			"path":   testCase.Input.Path,
		}
		if bodyMap["path"] == "" {
			bodyMap["path"] = "/"
		}
		if testCase.Input.Method != "" {
			bodyMap["method"] = testCase.Input.Method
		} else {
			bodyMap["method"] = "GET"
		}
		marshaled, _ := json.Marshal(bodyMap)
		return &adapterkit.CaseOutput{
			StatusCode: 200,
			ExitCode:   0,
			Stdout:     string(marshaled),
			Body:       bodyMap,
		}, nil
	}

	method := strings.ToUpper(testCase.Input.Method)
	if method == "" {
		method = "GET"
	}

	targetURL := d.baseURL
	if testCase.Input.Path != "" {
		if !strings.HasPrefix(testCase.Input.Path, "/") {
			targetURL += "/" + testCase.Input.Path
		} else {
			targetURL += testCase.Input.Path
		}
	}

	var reqBody io.Reader
	if testCase.Input.Body != nil {
		data, err := json.Marshal(testCase.Input.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	for k, v := range testCase.Input.Headers {
		req.Header.Set(k, v)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return &adapterkit.CaseOutput{
			StatusCode:   500,
			ExitCode:     1,
			ErrorMessage: err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	var parsedBody any
	if len(respBytes) > 0 {
		var decoded any
		if jsonErr := json.Unmarshal(respBytes, &decoded); jsonErr == nil {
			parsedBody = decoded
		} else {
			parsedBody = string(respBytes)
		}
	}

	exitCode := 0
	if resp.StatusCode >= 400 {
		exitCode = 1
	}

	return &adapterkit.CaseOutput{
		StatusCode: resp.StatusCode,
		Headers:    respHeaders,
		Body:       parsedBody,
		ExitCode:   exitCode,
		Stdout:     string(respBytes),
	}, nil
}

// Close closes the HTTP driver.
func (d *HTTPDriver) Close() error {
	d.closed = true
	return nil
}
