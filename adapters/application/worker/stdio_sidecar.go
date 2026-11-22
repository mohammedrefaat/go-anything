package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// StdioConfig defines configuration for stdio subprocess worker sidecar.
type StdioConfig struct {
	Command    string
	Args       []string
	WorkingDir string
	Env        map[string]string
	Timeout    time.Duration
}

// StdioSidecar forwards tasks to a child worker process over stdin/stdout.
type StdioSidecar struct {
	cfg        StdioConfig
	mu         sync.Mutex
	cmd        *exec.Cmd
	stdinPipe  io.WriteCloser
	stdoutScan *bufio.Scanner
	stderrBuf  *bytes.Buffer
	running    bool
	closed     bool
}

// NewStdioSidecar creates a new stdio worker sidecar client.
func NewStdioSidecar(cfg StdioConfig) (*StdioSidecar, error) {
	if cfg.Command == "" {
		return nil, errors.New("worker: stdio Command cannot be empty")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &StdioSidecar{cfg: cfg}, nil
}

// Transport returns TransportStdio.
func (s *StdioSidecar) Transport() SidecarTransport {
	return TransportStdio
}

func (s *StdioSidecar) startProcessLocked(ctx context.Context) error {
	if s.closed {
		return ErrSidecarClosed
	}
	if s.running && s.cmd != nil && s.cmd.Process != nil {
		return nil
	}

	cmd := exec.CommandContext(ctx, s.cfg.Command, s.cfg.Args...)
	if s.cfg.WorkingDir != "" {
		cmd.Dir = s.cfg.WorkingDir
	}

	cmd.Env = os.Environ()
	for k, v := range s.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("worker: failed obtaining stdio stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("worker: failed obtaining stdio stdout: %w", err)
	}

	s.stderrBuf = new(bytes.Buffer)
	cmd.Stderr = s.stderrBuf

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fmt.Errorf("worker: failed starting stdio process: %w", err)
	}

	s.cmd = cmd
	s.stdinPipe = stdin
	s.stdoutScan = bufio.NewScanner(stdout)
	s.running = true
	return nil
}

// Forward sends a task over stdin to the worker process.
func (s *StdioSidecar) Forward(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.startProcessLocked(ctx); err != nil {
		return nil, err
	}

	data, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	start := time.Now()
	type result struct {
		data []byte
		err  error
	}
	resCh := make(chan result, 1)

	go func() {
		if _, err := s.stdinPipe.Write(data); err != nil {
			resCh <- result{err: fmt.Errorf("failed writing to stdin: %w", err)}
			return
		}

		if s.stdoutScan.Scan() {
			out := append([]byte(nil), s.stdoutScan.Bytes()...)
			resCh <- result{data: out}
		} else {
			scanErr := s.stdoutScan.Err()
			if scanErr == nil {
				scanErr = io.EOF
			}
			resCh <- result{err: fmt.Errorf("subprocess exited or failed reading: %w", scanErr)}
		}
	}()

	timeout := s.cfg.Timeout
	if dl, ok := ctx.Deadline(); ok {
		tRem := time.Until(dl)
		if tRem > 0 && tRem < timeout {
			timeout = tRem
		}
	}

	select {
	case <-time.After(timeout):
		s.stopProcessLocked()
		return nil, ErrTaskTimeout
	case <-ctx.Done():
		s.stopProcessLocked()
		return nil, ctx.Err()
	case res := <-resCh:
		duration := time.Since(start).Seconds() * 1000
		if res.err != nil {
			s.stopProcessLocked()
			return &WorkerResult{
				TaskID:       task.ID,
				UnitID:       task.UnitID,
				Status:       "failed",
				ErrorMessage: fmt.Sprintf("%v (stderr: %s)", res.err, s.stderrBuf.String()),
				DurationMS:   duration,
			}, nil
		}

		var wr WorkerResult
		if err := json.Unmarshal(res.data, &wr); err != nil {
			var generic any
			_ = json.Unmarshal(res.data, &generic)
			return &WorkerResult{
				TaskID:     task.ID,
				UnitID:     task.UnitID,
				Status:     "success",
				Output:     generic,
				DurationMS: duration,
			}, nil
		}
		if wr.DurationMS == 0 {
			wr.DurationMS = duration
		}
		return &wr, nil
	}
}

// HealthCheck checks whether the stdio process is running.
func (s *StdioSidecar) HealthCheck(ctx context.Context) (*HealthReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return &HealthReport{
			Status:    HealthStateUnhealthy,
			Transport: TransportStdio,
			Message:   "stdio sidecar closed",
		}, ErrSidecarClosed
	}

	if !s.running || s.cmd == nil || s.cmd.Process == nil {
		if err := s.startProcessLocked(ctx); err != nil {
			return &HealthReport{
				Status:    HealthStateUnhealthy,
				Transport: TransportStdio,
				Message:   err.Error(),
			}, err
		}
	}

	return &HealthReport{
		Status:    HealthStateHealthy,
		Transport: TransportStdio,
		Message:   fmt.Sprintf("stdio process running (pid %d)", s.cmd.Process.Pid),
	}, nil
}

func (s *StdioSidecar) stopProcessLocked() {
	if !s.running {
		return
	}
	s.running = false
	if s.stdinPipe != nil {
		_ = s.stdinPipe.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
	s.cmd = nil
}

// Close gracefully stops the stdio subprocess and releases pipes.
func (s *StdioSidecar) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.stopProcessLocked()
	return nil
}
