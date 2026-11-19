//go:build !windows && !linux && !darwin

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

func (r *execRunner) runPlatform(ctx context.Context, cfg Config, workingDir string, stdoutWriter, stderrWriter io.Writer) (*Result, error) {
	res := &Result{
		IsolationMode: "best-effort",
		IsolationWarn: BestEffortWarning,
	}

	_, _ = io.WriteString(stderrWriter, BestEffortWarning+"\n")

	env := r.buildEnv(cfg, true)

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = workingDir
	cmd.Env = env
	cmd.Stdin = cfg.Stdin
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Start(); err != nil {
		return res, fmt.Errorf("failed to start process: %w", err)
	}

	waitErr := cmd.Wait()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		res.ExitCode = -1
		return res, ErrTimeout
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, waitErr
	}

	res.ExitCode = 0
	return res, nil
}
