//go:build linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func isUnprivilegedUsernsDisabled() bool {
	data, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone")
	if err == nil {
		if strings.TrimSpace(string(data)) == "0" {
			return true
		}
	}
	return false
}

func isPermissionOrNamespaceError(err error) bool {
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EINVAL) {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "operation not permitted") ||
		strings.Contains(errStr, "permission denied")
}

func (r *execRunner) runPlatform(ctx context.Context, cfg Config, workingDir string, stdoutWriter, stderrWriter io.Writer) (*Result, error) {
	// Try Tier 1: Linux Namespaces (if unprivileged userns clone is not disabled)
	if !isUnprivilegedUsernsDisabled() {
		res, err := r.runLinuxNamespaces(ctx, cfg, workingDir, stdoutWriter, stderrWriter)
		if err == nil || !isPermissionOrNamespaceError(err) {
			return res, err
		}
	}

	// Try Tier 1 Fallback: Bubblewrap (bwrap)
	if bwrapPath, err := exec.LookPath("bwrap"); err == nil && bwrapPath != "" {
		res, err := r.runBubblewrap(ctx, bwrapPath, cfg, workingDir, stdoutWriter, stderrWriter)
		if err == nil || !isPermissionOrNamespaceError(err) {
			return res, err
		}
	}

	// Fallback to Tier 2: Best-Effort mode
	return r.runBestEffortLinux(ctx, cfg, workingDir, stdoutWriter, stderrWriter)
}

func (r *execRunner) runLinuxNamespaces(ctx context.Context, cfg Config, workingDir string, stdoutWriter, stderrWriter io.Writer) (*Result, error) {
	res := &Result{
		IsolationMode: "namespaces",
	}

	env := r.buildEnv(cfg, false)

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = workingDir
	cmd.Env = env
	cmd.Stdin = cfg.Stdin
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	cloneFlags := uintptr(syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS | syscall.CLONE_NEWUSER)
	if !cfg.AllowNetwork {
		cloneFlags |= syscall.CLONE_NEWNET
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: cloneFlags,
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getgid(), Size: 1},
		},
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
		return nil, err
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

func (r *execRunner) runBubblewrap(ctx context.Context, bwrapPath string, cfg Config, workingDir string, stdoutWriter, stderrWriter io.Writer) (*Result, error) {
	res := &Result{
		IsolationMode: "bubblewrap",
	}

	env := r.buildEnv(cfg, false)
	if cfg.Command == "go" {
		// Bubblewrap mounts a writable tmpfs at /tmp while $HOME may be read-only.
		hasGOCACHE := false
		for i, kv := range env {
			if strings.HasPrefix(kv, "GOCACHE=") {
				env[i] = "GOCACHE=/tmp/go-build-cache"
				hasGOCACHE = true
				break
			}
		}
		if !hasGOCACHE {
			env = append(env, "GOCACHE=/tmp/go-build-cache")
		}
	}

	bwrapArgs := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	}

	if !cfg.AllowNetwork {
		bwrapArgs = append(bwrapArgs, "--unshare-net")
	}

	if workingDir != "" {
		bwrapArgs = append(bwrapArgs, "--bind", workingDir, workingDir, "--chdir", workingDir)
	}

	bwrapArgs = append(bwrapArgs, "--", cfg.Command)
	bwrapArgs = append(bwrapArgs, cfg.Args...)

	cmd := exec.CommandContext(ctx, bwrapPath, bwrapArgs...)
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
		return nil, err
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

func (r *execRunner) runBestEffortLinux(ctx context.Context, cfg Config, workingDir string, stdoutWriter, stderrWriter io.Writer) (*Result, error) {
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
