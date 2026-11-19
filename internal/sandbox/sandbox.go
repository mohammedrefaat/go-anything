// Package sandbox provides isolated, restricted execution environments
// with network restrictions, environment filtering, and resource limits.
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// BestEffortWarning is the prominent warning printed to stderr when running on platforms
// where OS-level kernel network isolation is not supported natively without virtualization.
const BestEffortWarning = "[WARN] Sandbox running in best-effort mode: OS-level network isolation is not supported natively on this platform without virtualization. Ensure repository code is trusted."

// DefaultInvalidProxySink is the invalid loopback proxy sink address assigned to proxy environment
// variables when network isolation is enabled in best-effort mode.
const DefaultInvalidProxySink = "http://127.0.0.1:0"

var (
	// ErrTimeout is returned when execution exceeds the configured deadline.
	ErrTimeout = errors.New("sandbox: execution timed out")

	// ErrEmptyCommand is returned when Config.Command is empty.
	ErrEmptyCommand = errors.New("sandbox: command cannot be empty")
)

// ResourceLimits specifies resource constraints for execution.
type ResourceLimits struct {
	// MaxCPUs limits the number of CPU cores available (0 = unconstrained).
	MaxCPUs int

	// MemoryMB limits the memory allocation in megabytes (0 = unconstrained).
	MemoryMB int64

	// MaxFiles limits the maximum open file descriptors (0 = unconstrained).
	MaxFiles int
}

// Config specifies the configuration for a sandboxed execution.
type Config struct {
	// Command is the program or executable to run.
	Command string

	// Args are the command-line arguments.
	Args []string

	// WorkingDir is the working directory for execution.
	// If TempWorkingCopy is true, this directory's contents are copied to an ephemeral directory.
	WorkingDir string

	// AllowNetwork enables outbound network access.
	// Defaults to false (fail-closed security).
	AllowNetwork bool

	// EnvAllowlist lists host environment variables permitted to pass through.
	// All unlisted host variables are stripped.
	EnvAllowlist []string

	// CustomEnv specifies additional or overridden environment variables.
	CustomEnv map[string]string

	// TempWorkingCopy isolates file modifications to an ephemeral directory.
	TempWorkingCopy bool

	// RetainWorkingCopy prevents automatic deletion of the temporary working copy
	// upon completion, allowing inspection by caller (who must call Result.Cleanup()).
	RetainWorkingCopy bool

	// Timeout sets the maximum execution duration before termination.
	Timeout time.Duration

	// Limits specifies resource constraints (CPU, memory, files).
	Limits ResourceLimits

	// Stdin provides standard input to the command.
	Stdin io.Reader

	// Stdout receives standard output (if nil, captured in Result.Stdout).
	Stdout io.Writer

	// Stderr receives standard error (if nil, captured in Result.Stderr).
	Stderr io.Writer
}

// Result captures the outcome of a sandboxed execution.
type Result struct {
	// ExitCode is the process exit status code (0 for success).
	ExitCode int

	// Stdout is the captured standard output if Config.Stdout was nil.
	Stdout string

	// Stderr is the captured standard error if Config.Stderr was nil.
	Stderr string

	// Duration is the total execution time.
	Duration time.Duration

	// TimedOut indicates if execution was terminated due to timeout.
	TimedOut bool

	// IsolationMode indicates which isolation tier was used ("namespaces", "bubblewrap", "best-effort").
	IsolationMode string

	// IsolationWarn holds the warning emitted in best-effort mode, if any.
	IsolationWarn string

	// WorkingCopyDir is the path of the temporary working directory used, if TempWorkingCopy was true.
	WorkingCopyDir string

	// Cleanup removes the temporary working copy if RetainWorkingCopy was true.
	Cleanup func() error
}

// Runner executes processes inside restricted sandbox environments.
type Runner interface {
	Run(ctx context.Context, cfg Config) (*Result, error)
}

// execRunner implements the Runner interface.
type execRunner struct{}

// New returns a new Runner instance.
func New() Runner {
	return &execRunner{}
}

// NewRunner returns a new Runner instance.
func NewRunner() Runner {
	return &execRunner{}
}

// Run executes the given command within a restricted sandbox environment.
func (r *execRunner) Run(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.Command == "" {
		return nil, ErrEmptyCommand
	}

	startTime := time.Now()

	workingDir := cfg.WorkingDir
	var tempDir string
	var cleanupFunc func() error

	if cfg.TempWorkingCopy {
		var err error
		tempDir, err = os.MkdirTemp("", "go-anything-sandbox-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp working copy: %w", err)
		}
		cleanupFunc = func() error {
			return os.RemoveAll(tempDir)
		}

		if workingDir != "" {
			if err := copyDir(workingDir, tempDir); err != nil {
				_ = cleanupFunc()
				return nil, fmt.Errorf("failed to copy working directory to temp copy: %w", err)
			}
		}
		workingDir = tempDir
	}

	if tempDir != "" && !cfg.RetainWorkingCopy {
		defer func() {
			_ = cleanupFunc()
		}()
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if cfg.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	stdoutWriter := cfg.Stdout
	if stdoutWriter == nil {
		stdoutWriter = &stdoutBuf
	}
	stderrWriter := cfg.Stderr
	if stderrWriter == nil {
		stderrWriter = &stderrBuf
	}

	res, err := r.runPlatform(execCtx, cfg, workingDir, stdoutWriter, stderrWriter)
	if res == nil {
		res = &Result{}
	}

	res.Duration = time.Since(startTime)
	if cfg.TempWorkingCopy {
		res.WorkingCopyDir = tempDir
		res.Cleanup = cleanupFunc
	}
	if cfg.Stdout == nil {
		res.Stdout = stdoutBuf.String()
	}
	if cfg.Stderr == nil {
		res.Stderr = stderrBuf.String()
	}

	return res, err
}

// buildEnv filters host environment variables and applies sandbox restrictions.
func (r *execRunner) buildEnv(cfg Config, isBestEffort bool) []string {
	var env []string

	if len(cfg.EnvAllowlist) > 0 {
		allowed := make(map[string]bool, len(cfg.EnvAllowlist))
		for _, k := range cfg.EnvAllowlist {
			if runtime.GOOS == "windows" {
				allowed[strings.ToUpper(k)] = true
			} else {
				allowed[k] = true
			}
		}

		for _, item := range os.Environ() {
			parts := strings.SplitN(item, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := parts[0]
			lookupKey := key
			if runtime.GOOS == "windows" {
				lookupKey = strings.ToUpper(key)
			}
			if allowed[lookupKey] {
				env = append(env, item)
			}
		}
	}

	// Always ensure SYSTEMROOT on Windows so child processes can load DLLs
	if runtime.GOOS == "windows" {
		hasSystemRoot := false
		for _, item := range env {
			if strings.HasPrefix(strings.ToUpper(item), "SYSTEMROOT=") {
				hasSystemRoot = true
				break
			}
		}
		if !hasSystemRoot {
			if sr := os.Getenv("SystemRoot"); sr != "" {
				env = append(env, "SystemRoot="+sr)
			}
		}
	}

	// Add custom env overrides
	for k, v := range cfg.CustomEnv {
		env = append(env, k+"="+v)
	}

	// Set invalid proxy sink when network is disabled in best-effort mode
	if !cfg.AllowNetwork && isBestEffort {
		proxyVars := []string{
			"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY",
			"http_proxy", "https_proxy", "all_proxy",
		}
		for _, pv := range proxyVars {
			env = append(env, pv+"="+DefaultInvalidProxySink)
		}
	}

	return env
}

// copyDir recursively copies directory src into dst.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if info.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(linkTarget, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath, info.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
