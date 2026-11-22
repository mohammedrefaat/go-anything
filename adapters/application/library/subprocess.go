package library

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// SubprocessConfig defines execution parameters for the foreign library subprocess wrapper.
type SubprocessConfig struct {
	Command    string
	Args       []string
	WorkingDir string
	Env        map[string]string
	Timeout    time.Duration
	Format     SerializationFormat
	Persistent bool
}

// SubprocessWrapper wraps a foreign library process and executes invocations over stdio.
type SubprocessWrapper struct {
	cfg   SubprocessConfig
	codec Codec

	mu         sync.Mutex
	cmd        *exec.Cmd
	stdinPipe  io.WriteCloser
	stdoutScan *bufio.Scanner
	stderrBuf  *bytes.Buffer
	running    bool
	closed     bool
}

// NewSubprocessWrapper creates a new foreign library subprocess wrapper.
func NewSubprocessWrapper(cfg SubprocessConfig) (*SubprocessWrapper, error) {
	if cfg.Command == "" {
		return nil, errors.New("library: subprocess command cannot be empty")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	codec, err := GetCodec(cfg.Format)
	if err != nil {
		return nil, err
	}

	return &SubprocessWrapper{
		cfg:   cfg,
		codec: codec,
	}, nil
}

// Start launches the persistent daemon process if persistent mode is configured.
func (w *SubprocessWrapper) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.cfg.Persistent {
		return nil
	}
	if w.running && w.cmd != nil && w.cmd.Process != nil {
		return nil
	}

	return w.startProcessLocked(ctx)
}

func (w *SubprocessWrapper) startProcessLocked(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, w.cfg.Command, w.cfg.Args...)
	if w.cfg.WorkingDir != "" {
		cmd.Dir = w.cfg.WorkingDir
	}

	cmd.Env = os.Environ()
	for k, v := range w.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("library: failed to obtain subprocess stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("library: failed to obtain subprocess stdout: %w", err)
	}

	w.stderrBuf = new(bytes.Buffer)
	cmd.Stderr = w.stderrBuf

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fmt.Errorf("library: failed to start persistent subprocess: %w", err)
	}

	w.cmd = cmd
	w.stdinPipe = stdin
	w.stdoutScan = bufio.NewScanner(stdout)
	w.running = true
	return nil
}

// Invoke executes a unit invocation with automatic payload encoding and decoding.
func (w *SubprocessWrapper) Invoke(ctx context.Context, unitID string, input any) (any, error) {
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

// InvokeRaw executes a unit invocation with raw byte payload.
func (w *SubprocessWrapper) InvokeRaw(ctx context.Context, unitID string, payload []byte) ([]byte, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, errors.New("library: subprocess wrapper is closed")
	}
	isPersistent := w.cfg.Persistent
	w.mu.Unlock()

	timeout := w.cfg.Timeout
	if dl, ok := ctx.Deadline(); ok {
		ctxTimeout := time.Until(dl)
		if ctxTimeout > 0 && ctxTimeout < timeout {
			timeout = ctxTimeout
		}
	}

	invokeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if isPersistent {
		return w.invokePersistent(invokeCtx, unitID, payload)
	}
	return w.invokeOneShot(invokeCtx, unitID, payload)
}

func (w *SubprocessWrapper) invokeOneShot(ctx context.Context, unitID string, payload []byte) ([]byte, error) {
	args := append([]string{}, w.cfg.Args...)
	if unitID != "" {
		args = append(args, "--unit", unitID)
	}

	cmd := exec.CommandContext(ctx, w.cfg.Command, args...)
	if w.cfg.WorkingDir != "" {
		cmd.Dir = w.cfg.WorkingDir
	}

	cmd.Env = os.Environ()
	for k, v := range w.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if unitID != "" {
		cmd.Env = append(cmd.Env, "GO_ANYTHING_UNIT_ID="+unitID)
	}

	if len(payload) > 0 {
		cmd.Stdin = bytes.NewReader(payload)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}

		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}

		return nil, &InvocationError{
			UnitID:   unitID,
			ExitCode: exitCode,
			Stderr:   stderrBuf.String(),
			Err:      fmt.Errorf("%w: %v", ErrInvocationFailed, err),
		}
	}

	return stdoutBuf.Bytes(), nil
}

func (w *SubprocessWrapper) invokePersistent(ctx context.Context, unitID string, payload []byte) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running || w.cmd == nil || w.cmd.Process == nil {
		if err := w.startProcessLocked(ctx); err != nil {
			return nil, err
		}
	}

	// Prepare payload with newline delimiter
	line := append(bytes.TrimRight(payload, "\r\n"), '\n')

	type result struct {
		data []byte
		err  error
	}
	resCh := make(chan result, 1)

	go func() {
		if _, err := w.stdinPipe.Write(line); err != nil {
			resCh <- result{err: fmt.Errorf("%w: failed writing to stdin: %v", ErrProcessCrashed, err)}
			return
		}

		if w.stdoutScan.Scan() {
			outBytes := append([]byte(nil), w.stdoutScan.Bytes()...)
			resCh <- result{data: outBytes}
		} else {
			scanErr := w.stdoutScan.Err()
			if scanErr == nil {
				scanErr = io.EOF
			}
			resCh <- result{err: fmt.Errorf("%w: %v", ErrProcessCrashed, scanErr)}
		}
	}()

	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			w.stopProcessLocked()
			return nil, ErrTimeout
		}
		w.stopProcessLocked()
		return nil, ctx.Err()

	case res := <-resCh:
		if res.err != nil {
			w.stopProcessLocked()
			return nil, &InvocationError{
				UnitID:   unitID,
				ExitCode: -1,
				Stderr:   w.stderrBuf.String(),
				Err:      res.err,
			}
		}
		return res.data, nil
	}
}

func (w *SubprocessWrapper) stopProcessLocked() {
	if !w.running {
		return
	}
	w.running = false
	if w.stdinPipe != nil {
		_ = w.stdinPipe.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait()
	}
	w.cmd = nil
}

// Stop gracefully stops the subprocess daemon.
func (w *SubprocessWrapper) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopProcessLocked()
	return nil
}

// Close releases all resources associated with the subprocess wrapper.
func (w *SubprocessWrapper) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	w.stopProcessLocked()
	return nil
}
