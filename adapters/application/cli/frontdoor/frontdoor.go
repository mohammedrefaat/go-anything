// Package frontdoor implements the strangler-pattern CLI routing front door.
// It intercepts command-line invocations: executing ported subcommands natively in Go,
// and transparently passing unported subcommands through to the original application binary.
package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Handler represents a native Go implementation for a ported CLI subcommand.
// It receives arguments (with subcommand name stripped), stdin, stdout, and stderr.
// It returns an exit status code (0 for success) and an optional error.
type Handler func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)

// FallbackFunc executes unported subcommands against the original binary.
type FallbackFunc func(ctx context.Context, origBin string, origArgs []string, argv []string, stdin io.Reader, stdout, stderr io.Writer, env []string, dir string) (int, error)

// RouteOverrideFunc allows dynamically forcing a ported subcommand back to original (kill switch).
type RouteOverrideFunc func(cmd string, unitID string) bool

// FallbackHookFunc is invoked whenever an execution falls back to the original binary.
type FallbackHookFunc func(cmd string, unitID string, argv []string)

// Option configures the FrontDoor instance.
type Option func(*FrontDoor)

// FrontDoor orchestrates CLI routing between ported Go implementations and the original legacy application.
type FrontDoor struct {
	mu             sync.RWMutex
	originalBinary string
	originalArgs   []string
	commands       map[string]Handler
	commandUnits   map[string]string // maps subcommand to unitID
	stdin          io.Reader
	stdout         io.Writer
	stderr         io.Writer
	env            []string
	workingDir     string
	fallback       FallbackFunc
	routeOverride  RouteOverrideFunc
	fallbackHook   FallbackHookFunc
}

// WithOriginalArgs specifies default arguments to prepend when executing the original binary
// (e.g. for interpreter invocations such as ["legacy/app.py"]).
func WithOriginalArgs(args ...string) Option {
	return func(fd *FrontDoor) {
		fd.originalArgs = args
	}
}

// WithPortedCommand registers a native Go handler for a ported subcommand.
func WithPortedCommand(name string, handler Handler) Option {
	return func(fd *FrontDoor) {
		fd.commands[strings.TrimSpace(name)] = handler
	}
}

// WithPortedUnitCommand registers a native Go handler linked to a specific migration unit ID.
func WithPortedUnitCommand(name, unitID string, handler Handler) Option {
	return func(fd *FrontDoor) {
		name = strings.TrimSpace(name)
		fd.commands[name] = handler
		if unitID != "" {
			fd.commandUnits[name] = unitID
		}
	}
}

// WithIO configures standard input, output, and error streams.
func WithIO(stdin io.Reader, stdout, stderr io.Writer) Option {
	return func(fd *FrontDoor) {
		if stdin != nil {
			fd.stdin = stdin
		}
		if stdout != nil {
			fd.stdout = stdout
		}
		if stderr != nil {
			fd.stderr = stderr
		}
	}
}

// WithEnv configures the environment variables passed to unported subcommands.
func WithEnv(env []string) Option {
	return func(fd *FrontDoor) {
		fd.env = env
	}
}

// WithWorkingDir sets the working directory for executing unported subcommands.
func WithWorkingDir(dir string) Option {
	return func(fd *FrontDoor) {
		fd.workingDir = dir
	}
}

// WithFallbackExecutor sets a custom fallback execution function.
func WithFallbackExecutor(fn FallbackFunc) Option {
	return func(fd *FrontDoor) {
		if fn != nil {
			fd.fallback = fn
		}
	}
}

// WithRouteOverride sets a dynamic predicate to override routing to the original binary (kill switch).
func WithRouteOverride(fn RouteOverrideFunc) Option {
	return func(fd *FrontDoor) {
		fd.routeOverride = fn
	}
}

// WithFallbackHook sets a hook called whenever fallback traffic is routed to the original binary.
func WithFallbackHook(fn FallbackHookFunc) Option {
	return func(fd *FrontDoor) {
		fd.fallbackHook = fn
	}
}

// New creates a new CLI front door for strangling a legacy CLI application.
func New(originalBinary string, opts ...Option) *FrontDoor {
	fd := &FrontDoor{
		originalBinary: originalBinary,
		commands:       make(map[string]Handler),
		commandUnits:   make(map[string]string),
		stdin:          os.Stdin,
		stdout:         os.Stdout,
		stderr:         os.Stderr,
		env:            os.Environ(),
		fallback:       defaultFallback,
	}

	for _, opt := range opts {
		opt(fd)
	}

	// If originalBinary contains spaces and does not exist on disk as a single file,
	// parse command and leading arguments (e.g. "python legacy/cli.py").
	if len(fd.originalArgs) == 0 && strings.Contains(fd.originalBinary, " ") {
		if _, err := os.Stat(fd.originalBinary); os.IsNotExist(err) {
			parts := strings.Fields(fd.originalBinary)
			if len(parts) > 1 {
				fd.originalBinary = parts[0]
				fd.originalArgs = parts[1:]
			}
		}
	}

	return fd
}

// Register registers a native Go handler for a subcommand.
func (fd *FrontDoor) Register(name string, handler Handler) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.commands[strings.TrimSpace(name)] = handler
}

// RegisterUnit registers a native Go handler linked to a unit ID.
func (fd *FrontDoor) RegisterUnit(name, unitID string, handler Handler) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	name = strings.TrimSpace(name)
	fd.commands[name] = handler
	if unitID != "" {
		fd.commandUnits[name] = unitID
	}
}

// HasCommand reports whether a subcommand is registered as ported.
func (fd *FrontDoor) HasCommand(name string) bool {
	fd.mu.RLock()
	defer fd.mu.RUnlock()
	_, exists := fd.commands[strings.TrimSpace(name)]
	return exists
}

// Execute routes and executes the CLI invocation with the given arguments.
// argv typically contains os.Args[1:].
// Returns the process exit status code and any execution error.
func (fd *FrontDoor) Execute(ctx context.Context, argv []string) (int, error) {
	return fd.ExecuteWithIO(ctx, argv, fd.stdin, fd.stdout, fd.stderr)
}

// ExecuteWithIO routes and executes the CLI invocation with explicitly provided I/O streams.
func (fd *FrontDoor) ExecuteWithIO(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdin == nil {
		stdin = fd.stdin
	}
	if stdout == nil {
		stdout = fd.stdout
	}
	if stderr == nil {
		stderr = fd.stderr
	}

	fd.mu.RLock()
	cmdName, cmdArgs, handler, isPorted := fd.resolveCommand(argv)
	unitID := fd.commandUnits[cmdName]
	overrideOriginal := false
	if isPorted && fd.routeOverride != nil {
		overrideOriginal = fd.routeOverride(cmdName, unitID)
	}
	fd.mu.RUnlock()

	// 1. Ported subcommand native execution (unless kill-switch overridden)
	if isPorted && !overrideOriginal {
		return handler(ctx, cmdArgs, stdin, stdout, stderr)
	}

	// 2. Unported subcommand or kill-switch override: transparent passthrough to original binary
	if fd.fallbackHook != nil {
		fd.fallbackHook(cmdName, unitID, argv)
	}

	return fd.fallback(ctx, fd.originalBinary, fd.originalArgs, argv, stdin, stdout, stderr, fd.env, fd.workingDir)
}

// Run executes the CLI front door using process defaults and returns the exit status code.
func (fd *FrontDoor) Run(argv []string) int {
	code, err := fd.Execute(context.Background(), argv)
	if err != nil && code == 0 {
		return 1
	}
	return code
}

// resolveCommand inspects argv to find matching ported commands.
// It supports direct subcommand matching (e.g. "calc" in ["calc", "1", "2"])
// as well as invocations preceded by global flags (e.g. ["--verbose", "calc", "1", "2"]).
func (fd *FrontDoor) resolveCommand(argv []string) (cmdName string, cmdArgs []string, handler Handler, found bool) {
	if len(argv) == 0 {
		return "", nil, nil, false
	}

	// 1. Check multi-token subcommand match (e.g. "user add" in ["user", "add", "alice"])
	if len(argv) >= 2 {
		twoToken := argv[0] + " " + argv[1]
		if h, ok := fd.commands[twoToken]; ok {
			return twoToken, argv[2:], h, true
		}
	}

	// 2. Check first argument direct match
	if h, ok := fd.commands[argv[0]]; ok {
		return argv[0], argv[1:], h, true
	}

	// 3. Scan for first non-flag argument if global flags precede the subcommand
	for i, arg := range argv {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// Multi-token check from position i
		if i+1 < len(argv) {
			twoToken := arg + " " + argv[i+1]
			if h, ok := fd.commands[twoToken]; ok {
				remaining := make([]string, 0, len(argv)-2)
				remaining = append(remaining, argv[:i]...)
				remaining = append(remaining, argv[i+2:]...)
				return twoToken, remaining, h, true
			}
		}
		// Single token match from position i
		if h, ok := fd.commands[arg]; ok {
			remaining := make([]string, 0, len(argv)-1)
			remaining = append(remaining, argv[:i]...)
			remaining = append(remaining, argv[i+1:]...)
			return arg, remaining, h, true
		}
		// Only check the first non-flag token as the candidate subcommand
		break
	}

	return "", nil, nil, false
}

// defaultFallback runs the original binary via os/exec, completely preserving argv, stdin,
// stdout, stderr, environment, and exit status code.
func defaultFallback(ctx context.Context, origBin string, origArgs []string, argv []string, stdin io.Reader, stdout, stderr io.Writer, env []string, dir string) (int, error) {
	if origBin == "" {
		return 1, errors.New("frontdoor: original binary path is empty")
	}

	var finalArgs []string
	if len(origArgs) > 0 {
		finalArgs = append(append([]string{}, origArgs...), argv...)
	} else {
		finalArgs = argv
	}

	cmd := exec.CommandContext(ctx, origBin, finalArgs...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if len(env) > 0 {
		cmd.Env = env
	}
	if dir != "" {
		cmd.Dir = dir
	}

	err := cmd.Run()
	if err == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}

	return 1, fmt.Errorf("frontdoor: execution of original binary %q failed: %w", origBin, err)
}
