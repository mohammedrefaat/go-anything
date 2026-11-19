// Package adapterhost manages subprocess execution, checksum verification,
// capability enforcement, and diagnostic doctor checks for external adapters.
package adapterhost

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

var (
	// ErrTamperedAdapter is returned when an external adapter binary does not match its pinned SHA-256 checksum.
	ErrTamperedAdapter = errors.New("adapterhost: adapter checksum mismatch; binary has been tampered with or modified")

	// ErrAdapterNotInstalled is returned when an adapter executable does not exist on disk.
	ErrAdapterNotInstalled = errors.New("adapterhost: adapter executable not found on disk")

	// ErrSubprocessTerminated is returned when an adapter subprocess exits unexpectedly.
	ErrSubprocessTerminated = errors.New("adapterhost: subprocess terminated prematurely")
)

// DefaultMethodCapabilities maps common adapter RPC methods to their required capabilities.
var DefaultMethodCapabilities = map[string]adapterkit.Capability{
	"analyze":        adapterkit.CapabilityReadRepo,
	"discover_units": adapterkit.CapabilityReadRepo,
	"read_source":    adapterkit.CapabilityReadRepo,
	"run_original":   adapterkit.CapabilityRunOriginal,
	"execute_case":   adapterkit.CapabilityRunOriginal,
	"network_fetch":  adapterkit.CapabilityNetwork,
}

// AdapterConfig defines the configuration and security constraints for an external adapter.
type AdapterConfig struct {
	Name           string                  `json:"name"`
	Executable     string                  `json:"executable"`
	Args           []string                `json:"args,omitempty"`
	SHA256Checksum string                  `json:"sha256"`
	Capabilities   []adapterkit.Capability `json:"capabilities"`
}

// ComputeSHA256 calculates the hex-encoded SHA-256 checksum of the specified file.
func ComputeSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyChecksum ensures the target file matches the expected SHA-256 checksum.
func VerifyChecksum(filePath, expectedSHA256 string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrAdapterNotInstalled, filePath)
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("adapter path %q is a directory, expected executable file", filePath)
	}

	actual, err := ComputeSHA256(filePath)
	if err != nil {
		return fmt.Errorf("failed to compute adapter checksum: %w", err)
	}

	if !strings.EqualFold(actual, strings.TrimSpace(expectedSHA256)) {
		return fmt.Errorf("%w: expected %s, got %s", ErrTamperedAdapter, expectedSHA256, actual)
	}
	return nil
}

// RPCRequest defines a JSON RPC request sent to the adapter subprocess over stdin.
type RPCRequest struct {
	ID         string          `json:"id"`
	Method     string          `json:"method"`
	Capability string          `json:"capability,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// RPCResponse defines a JSON RPC response received from the adapter subprocess over stdout.
type RPCResponse struct {
	ID     string          `json:"id"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// SubprocessTransport implements adapterkit.Transport over stdio JSON for external adapters.
type SubprocessTransport struct {
	cfg        AdapterConfig
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	scanner    *bufio.Scanner
	mu         sync.Mutex
	reqCounter uint64
	closed     bool
}

// NewSubprocessTransport verifies the adapter checksum and spawns the subprocess.
func NewSubprocessTransport(ctx context.Context, cfg AdapterConfig) (*SubprocessTransport, error) {
	if cfg.Executable == "" {
		return nil, errors.New("adapterhost: executable path cannot be empty")
	}

	// 1. Enforce pinned SHA-256 verification (Rule: tampered adapter fails)
	if cfg.SHA256Checksum != "" {
		if err := VerifyChecksum(cfg.Executable, cfg.SHA256Checksum); err != nil {
			return nil, err
		}
	}

	// 2. Validate declared capabilities
	if err := adapterkit.ValidateCapabilities(cfg.Capabilities); err != nil {
		return nil, fmt.Errorf("invalid capabilities in config: %w", err)
	}

	cmd := exec.CommandContext(ctx, cfg.Executable, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("failed to start adapter subprocess: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	return &SubprocessTransport{
		cfg:     cfg,
		cmd:     cmd,
		stdin:   stdin,
		scanner: scanner,
	}, nil
}

// Type returns adapterkit.TransportSubprocess.
func (t *SubprocessTransport) Type() adapterkit.TransportType {
	return adapterkit.TransportSubprocess
}

// Invoke sends a request to the subprocess and receives the response, enforcing capability checks.
func (t *SubprocessTransport) Invoke(ctx context.Context, method string, payload []byte) ([]byte, error) {
	// Look up default required capability for method
	requiredCap := DefaultMethodCapabilities[method]
	return t.InvokeWithCapability(ctx, requiredCap, method, payload)
}

// InvokeWithCapability verifies that requiredCap is declared before dispatching the request.
func (t *SubprocessTransport) InvokeWithCapability(ctx context.Context, requiredCap adapterkit.Capability, method string, payload []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil, errors.New("adapterhost: transport is closed")
	}

	// Enforce capability check: reject any undeclared capability
	if requiredCap != "" && !adapterkit.HasCapability(t.cfg.Capabilities, requiredCap) {
		return nil, fmt.Errorf("%w: action %q requires undeclared capability %q (adapter %q only declared: %v)",
			adapterkit.ErrUndeclaredCapability, method, requiredCap, t.cfg.Name, t.cfg.Capabilities)
	}

	reqID := fmt.Sprintf("req-%d", atomic.AddUint64(&t.reqCounter, 1))
	rpcReq := RPCRequest{
		ID:         reqID,
		Method:     method,
		Capability: string(requiredCap),
		Payload:    payload,
	}

	data, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RPC request: %w", err)
	}

	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write to adapter stdin: %w", err)
	}

	// Read response
	responseChan := make(chan []byte, 1)
	errChan := make(chan error, 1)

	go func() {
		if !t.scanner.Scan() {
			if err := t.scanner.Err(); err != nil {
				errChan <- err
			} else {
				errChan <- ErrSubprocessTerminated
			}
			return
		}
		responseChan <- t.scanner.Bytes()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errChan:
		return nil, err
	case respBytes := <-responseChan:
		var rpcResp RPCResponse
		if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
			return nil, fmt.Errorf("failed to parse adapter response JSON: %w", err)
		}
		if rpcResp.Error != "" {
			return nil, errors.New(rpcResp.Error)
		}
		return rpcResp.Result, nil
	}
}

// Close closes the transport and terminates the subprocess.
func (t *SubprocessTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	var errs []string
	if t.stdin != nil {
		if err := t.stdin.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
