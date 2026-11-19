// Package cli provides a black-box application adapter for executing CLI binaries.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mohammedrefat/go-anything/internal/sandbox"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// Option configures the CLI Adapter.
type Option func(*Adapter)

// WithWorkingDir sets the working directory for command execution.
func WithWorkingDir(dir string) Option {
	return func(a *Adapter) {
		a.workingDir = dir
	}
}

// WithSandbox configures a custom sandbox.Runner.
func WithSandbox(runner sandbox.Runner) Option {
	return func(a *Adapter) {
		a.sandbox = runner
	}
}

// WithTimeout configures the default execution timeout.
func WithTimeout(d time.Duration) Option {
	return func(a *Adapter) {
		if d > 0 {
			a.timeout = d
		}
	}
}

// WithEnv configures default environment variables.
func WithEnv(env map[string]string) Option {
	return func(a *Adapter) {
		a.env = env
	}
}

// Adapter executes black-box CLI applications and implements adapterkit.Replay.
type Adapter struct {
	binaryPath string
	workingDir string
	timeout    time.Duration
	env        map[string]string
	sandbox    sandbox.Runner
}

// NewAdapter creates a new CLI application adapter.
func NewAdapter(binaryPath string, opts ...Option) *Adapter {
	a := &Adapter{
		binaryPath: binaryPath,
		timeout:    30 * time.Second,
		env:        make(map[string]string),
		sandbox:    sandbox.New(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// DriverName returns the identifier for this replay driver.
func (a *Adapter) DriverName() string {
	if a.binaryPath != "" {
		return "cli:" + filepath.Base(a.binaryPath)
	}
	return "cli"
}

// ExecuteCase replays a single behavioral test case against the target CLI binary.
func (a *Adapter) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if testCase == nil {
		return nil, errors.New("cli: testCase cannot be nil")
	}
	return a.Execute(ctx, testCase.Input)
}

// Execute runs the CLI binary with the given input payload.
func (a *Adapter) Execute(ctx context.Context, input adapterkit.CaseInput) (*adapterkit.CaseOutput, error) {
	command := a.binaryPath
	var args []string

	if len(input.Argv) > 0 {
		if command == "" {
			command = input.Argv[0]
			args = input.Argv[1:]
		} else {
			// If first argv matches binary path or base name, strip it to avoid duplication
			if input.Argv[0] == command || filepath.Base(input.Argv[0]) == filepath.Base(command) {
				args = input.Argv[1:]
			} else {
				args = input.Argv
			}
		}
	}

	if command == "" {
		return nil, errors.New("cli: no binary path or argv provided")
	}

	// Prepare stdin payload if provided
	var stdinReader *bytes.Reader
	if input.Body != nil {
		switch b := input.Body.(type) {
		case string:
			stdinReader = bytes.NewReader([]byte(b))
		case []byte:
			stdinReader = bytes.NewReader(b)
		default:
			data, err := json.Marshal(b)
			if err != nil {
				return nil, fmt.Errorf("cli: failed to serialize input body: %w", err)
			}
			stdinReader = bytes.NewReader(data)
		}
	}

	// Merge environment variables
	mergedEnv := make(map[string]string, len(a.env)+len(input.Env))
	for k, v := range a.env {
		mergedEnv[k] = v
	}
	for k, v := range input.Env {
		mergedEnv[k] = v
	}

	sandboxCfg := sandbox.Config{
		Command:         command,
		Args:            args,
		WorkingDir:      a.workingDir,
		TempWorkingCopy: true,
		AllowNetwork:    false,
		Timeout:         a.timeout,
		CustomEnv:       mergedEnv,
	}
	if stdinReader != nil {
		sandboxCfg.Stdin = stdinReader
	}

	res, err := a.sandbox.Run(ctx, sandboxCfg)
	if err != nil && !errors.Is(err, sandbox.ErrTimeout) {
		return nil, fmt.Errorf("cli execution failed: %w", err)
	}

	out := &adapterkit.CaseOutput{
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		ExitCode: res.ExitCode,
	}

	if res.TimedOut {
		out.ErrorMessage = "execution timed out"
	} else if res.ExitCode != 0 {
		out.ErrorMessage = res.Stderr
	}

	// If stdout is valid JSON, also parse into Body
	trimmedOut := bytes.TrimSpace([]byte(res.Stdout))
	if len(trimmedOut) > 0 && (trimmedOut[0] == '{' || trimmedOut[0] == '[') {
		var parsed any
		if json.Unmarshal(trimmedOut, &parsed) == nil {
			out.Body = parsed
		}
	}

	return out, nil
}

// Close releases any resources associated with the adapter.
func (a *Adapter) Close() error {
	return nil
}
