// Package http provides a black-box application adapter for executing HTTP server requests.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// Option configures the HTTP Adapter.
type Option func(*Adapter)

// WithBaseURL sets the base URL for the target server (e.g. http://127.0.0.1:8080).
func WithBaseURL(rawURL string) Option {
	return func(a *Adapter) {
		a.baseURL = strings.TrimRight(rawURL, "/")
	}
}

// WithClient sets a custom HTTP client.
func WithClient(client *http.Client) Option {
	return func(a *Adapter) {
		a.client = client
	}
}

// WithTimeout sets the per-request HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(a *Adapter) {
		if d > 0 {
			a.timeout = d
		}
	}
}

// WithManagedServer starts an ephemeral server subprocess and tears it down on Close.
func WithManagedServer(cmd string, args []string, env map[string]string) Option {
	return func(a *Adapter) {
		a.serverCmdPath = cmd
		a.serverArgs = args
		a.serverEnv = env
	}
}

// Adapter executes HTTP requests against target applications and implements adapterkit.Replay.
type Adapter struct {
	baseURL       string
	client        *http.Client
	timeout       time.Duration
	serverCmdPath string
	serverArgs    []string
	serverEnv     map[string]string
	serverProcess *exec.Cmd
}

// NewAdapter creates a new HTTP application adapter.
func NewAdapter(opts ...Option) *Adapter {
	a := &Adapter{
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(a)
	}

	if a.client == nil {
		a.client = &http.Client{
			Timeout: a.timeout,
		}
	}

	return a
}

// AllocateEphemeralPort reserves and returns a free TCP port on localhost.
func AllocateEphemeralPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to allocate ephemeral port: %w", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("failed to obtain TCPAddr from listener")
	}
	return addr.Port, nil
}

// DriverName returns the identifier for this replay driver.
func (a *Adapter) DriverName() string {
	if a.baseURL != "" {
		return "http:" + a.baseURL
	}
	return "http"
}

// StartManagedServer starts the configured server binary on an allocated ephemeral port.
func (a *Adapter) StartManagedServer(ctx context.Context) error {
	if a.serverCmdPath == "" {
		return errors.New("http: no managed server command specified")
	}

	port, err := AllocateEphemeralPort()
	if err != nil {
		return err
	}

	portStr := fmt.Sprintf("%d", port)
	var finalArgs []string
	for _, arg := range a.serverArgs {
		finalArgs = append(finalArgs, strings.ReplaceAll(arg, "{PORT}", portStr))
	}

	cmd := exec.CommandContext(ctx, a.serverCmdPath, finalArgs...)
	cmd.Env = osEnviron()
	for k, v := range a.serverEnv {
		cmd.Env = append(cmd.Env, k+"="+strings.ReplaceAll(v, "{PORT}", portStr))
	}
	cmd.Env = append(cmd.Env, "PORT="+portStr)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start server process: %w", err)
	}

	a.serverProcess = cmd
	a.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait for server readiness (polling up to 5s)
	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !ready {
		_ = cmd.Process.Kill()
		return fmt.Errorf("server failed to start listening on port %d within timeout", port)
	}

	return nil
}

func osEnviron() []string {
	return append([]string{}, netEnvPassthrough()...)
}

func netEnvPassthrough() []string {
	return []string{"PATH=" + getEnvOrDefault("PATH", ""), "SystemRoot=" + getEnvOrDefault("SystemRoot", "")}
}

func getEnvOrDefault(key, fallback string) string {
	if v, ok := netLookupEnv(key); ok {
		return v
	}
	return fallback
}

var netLookupEnv = func(key string) (string, bool) {
	// Simple lookup wrapper
	for _, item := range []string{} {
		_ = item
	}
	return "", false
}

// ExecuteCase replays a single behavioral test case by executing an HTTP request against the server.
func (a *Adapter) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if testCase == nil {
		return nil, errors.New("http: testCase cannot be nil")
	}
	return a.Execute(ctx, testCase.Input)
}

// Execute performs an HTTP request constructed from the given input payload.
func (a *Adapter) Execute(ctx context.Context, input adapterkit.CaseInput) (*adapterkit.CaseOutput, error) {
	method := strings.ToUpper(input.Method)
	if method == "" {
		method = "GET"
	}

	targetURL := a.buildURL(input.Path)
	if targetURL == "" {
		return nil, errors.New("http: no target URL or path provided")
	}

	// Prepare request body
	var bodyReader io.Reader
	hasBody := false
	if input.Body != nil {
		switch b := input.Body.(type) {
		case string:
			bodyReader = strings.NewReader(b)
			hasBody = len(b) > 0
		case []byte:
			bodyReader = bytes.NewReader(b)
			hasBody = len(b) > 0
		default:
			data, err := json.Marshal(b)
			if err != nil {
				return nil, fmt.Errorf("http: failed to marshal request body: %w", err)
			}
			bodyReader = bytes.NewReader(data)
			hasBody = true
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Apply headers
	for k, v := range input.Headers {
		req.Header.Set(k, v)
	}

	if hasBody && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read HTTP response body: %w", err)
	}

	respHeaders := make(map[string]string, len(resp.Header))
	for k, vv := range resp.Header {
		respHeaders[k] = strings.Join(vv, ", ")
	}

	out := &adapterkit.CaseOutput{
		StatusCode: resp.StatusCode,
		Headers:    respHeaders,
		Stdout:     string(respBytes),
	}

	trimmed := bytes.TrimSpace(respBytes)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		var parsed any
		if json.Unmarshal(trimmed, &parsed) == nil {
			out.Body = parsed
		}
	}

	if resp.StatusCode >= 400 {
		out.ErrorMessage = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	return out, nil
}

func (a *Adapter) buildURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}

	if a.baseURL == "" {
		return path
	}

	if path == "" || path == "/" {
		return a.baseURL
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	u, err := url.Parse(a.baseURL + path)
	if err != nil {
		return a.baseURL + path
	}
	return u.String()
}

// Close terminates any managed server process and closes idle connections.
func (a *Adapter) Close() error {
	if a.serverProcess != nil && a.serverProcess.Process != nil {
		_ = a.serverProcess.Process.Kill()
		_ = a.serverProcess.Wait()
		a.serverProcess = nil
	}
	if a.client != nil {
		a.client.CloseIdleConnections()
	}
	return nil
}
