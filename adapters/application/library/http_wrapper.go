package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HTTPConfig defines configuration parameters for the foreign library HTTP wrapper.
type HTTPConfig struct {
	BaseURL     string
	Client      *http.Client
	Timeout     time.Duration
	Format      SerializationFormat
	Headers     map[string]string
	ManagedCmd  string
	ManagedArgs []string
	ManagedEnv  map[string]string
}

// HTTPWrapper routes foreign library invocations across HTTP endpoints.
type HTTPWrapper struct {
	cfg     HTTPConfig
	codec   Codec
	client  *http.Client
	baseURL string

	mu             sync.Mutex
	managedProcess *exec.Cmd
	closed         bool
}

// NewHTTPWrapper creates a new foreign library HTTP wrapper.
func NewHTTPWrapper(cfg HTTPConfig) (*HTTPWrapper, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	codec, err := GetCodec(cfg.Format)
	if err != nil {
		return nil, err
	}

	client := cfg.Client
	if client == nil {
		client = &http.Client{
			Timeout: cfg.Timeout,
		}
	}

	return &HTTPWrapper{
		cfg:     cfg,
		codec:   codec,
		client:  client,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
	}, nil
}

// Start boots any configured managed server process on an ephemeral port.
func (w *HTTPWrapper) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cfg.ManagedCmd == "" {
		return nil
	}
	if w.managedProcess != nil && w.managedProcess.Process != nil {
		return nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("library: failed to allocate ephemeral port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	portStr := fmt.Sprintf("%d", port)
	var finalArgs []string
	for _, arg := range w.cfg.ManagedArgs {
		finalArgs = append(finalArgs, strings.ReplaceAll(arg, "{PORT}", portStr))
	}

	cmd := exec.CommandContext(ctx, w.cfg.ManagedCmd, finalArgs...)
	cmd.Env = os.Environ()
	for k, v := range w.cfg.ManagedEnv {
		cmd.Env = append(cmd.Env, k+"="+strings.ReplaceAll(v, "{PORT}", portStr))
	}
	cmd.Env = append(cmd.Env, "PORT="+portStr)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("library: failed to start managed server: %w", err)
	}

	w.managedProcess = cmd
	w.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	// Poll until server accepts connections or timeout
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
		return fmt.Errorf("library: managed server failed to start listening on port %d", port)
	}

	return nil
}

// Invoke executes a unit invocation against the foreign HTTP service.
func (w *HTTPWrapper) Invoke(ctx context.Context, unitID string, input any) (any, error) {
	data, err := w.codec.Encode(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSerialization, err)
	}

	rawOut, err := w.InvokeRaw(ctx, unitID, data)
	if err != nil {
		return nil, err
	}

	var output any
	if err := w.codec.Decode(rawOut, &output); err != nil {
		return nil, fmt.Errorf("%w: failed to decode response: %v", ErrSerialization, err)
	}
	return output, nil
}

// InvokeRaw executes a raw payload HTTP invocation against the foreign service.
func (w *HTTPWrapper) InvokeRaw(ctx context.Context, unitID string, payload []byte) ([]byte, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, errors.New("library: HTTP wrapper is closed")
	}
	baseURL := w.baseURL
	w.mu.Unlock()

	targetURL := baseURL
	if unitID != "" {
		if targetURL != "" {
			targetURL = targetURL + "/" + url.PathEscape(unitID)
		} else {
			targetURL = "/" + url.PathEscape(unitID)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("library: failed to build HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", w.codec.ContentType())
	for k, v := range w.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, &InvocationError{
			UnitID:   unitID,
			ExitCode: -1,
			Err:      fmt.Errorf("%w: %v", ErrInvocationFailed, err),
		}
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("library: failed to read HTTP response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &InvocationError{
			UnitID:   unitID,
			ExitCode: resp.StatusCode,
			Stderr:   string(respBytes),
			Err:      fmt.Errorf("%w: HTTP status %d", ErrInvocationFailed, resp.StatusCode),
		}
	}

	return respBytes, nil
}

// Close terminates any managed server process and closes idle connections.
func (w *HTTPWrapper) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true

	if w.managedProcess != nil && w.managedProcess.Process != nil {
		_ = w.managedProcess.Process.Kill()
		_ = w.managedProcess.Wait()
		w.managedProcess = nil
	}
	if w.client != nil {
		w.client.CloseIdleConnections()
	}
	return nil
}
