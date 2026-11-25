package frontdoor_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/adapters/application/cli/frontdoor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelperProcess serves as the mock original binary for subprocess passthrough tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FRONTDOOR_HELPER") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Println("legacy: default output")
		return
	}

	cmd := args[0]
	switch cmd {
	case "echo_args":
		fmt.Printf("legacy args: %s\n", strings.Join(args[1:], " "))
	case "echo_stdin":
		data, _ := io.ReadAll(os.Stdin)
		fmt.Printf("legacy stdin: %s\n", string(data))
	case "echo_env":
		fmt.Printf("legacy env: %s\n", os.Getenv("TEST_FRONTDOOR_ENV"))
	case "write_stderr":
		fmt.Fprintln(os.Stderr, "legacy error output")
	case "exit_code":
		code := 1
		if len(args) > 1 {
			if c, err := strconv.Atoi(args[1]); err == nil {
				code = c
			}
		}
		fmt.Fprintf(os.Stderr, "legacy exiting with %d\n", code)
		os.Exit(code)
	default:
		fmt.Printf("legacy unported: %s\n", cmd)
	}
}

func getHelperBinary(t *testing.T) (string, []string) {
	bin := os.Args[0]
	defaultArgs := []string{"-test.run=TestHelperProcess", "--"}
	return bin, defaultArgs
}

func TestFrontDoor_PortedCommandNativeExecution(t *testing.T) {
	bin, defaultArgs := getHelperBinary(t)

	calledCalc := false
	calledGreet := false

	fd := frontdoor.New(bin,
		frontdoor.WithOriginalArgs(defaultArgs...),
		frontdoor.WithPortedCommand("calc", func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
			calledCalc = true
			fmt.Fprintf(stdout, "ported calc: args=%v\n", args)
			return 0, nil
		}),
		frontdoor.WithPortedUnitCommand("greet", "unit-greet", func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
			calledGreet = true
			if len(args) == 0 {
				fmt.Fprintln(stderr, "error: missing name")
				return 2, nil
			}
			fmt.Fprintf(stdout, "hello %s\n", args[0])
			return 0, nil
		}),
	)

	assert.True(t, fd.HasCommand("calc"))
	assert.True(t, fd.HasCommand("greet"))
	assert.False(t, fd.HasCommand("unported"))

	// 1. Execute ported calc
	var stdout, stderr bytes.Buffer
	code, err := fd.ExecuteWithIO(context.Background(), []string{"calc", "10", "20"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.True(t, calledCalc)
	assert.Contains(t, stdout.String(), "ported calc: args=[10 20]")

	// 2. Execute ported greet success
	stdout.Reset()
	stderr.Reset()
	code, err = fd.ExecuteWithIO(context.Background(), []string{"greet", "alice"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.True(t, calledGreet)
	assert.Equal(t, "hello alice\n", stdout.String())

	// 3. Execute ported greet with error code
	stdout.Reset()
	stderr.Reset()
	code, err = fd.ExecuteWithIO(context.Background(), []string{"greet"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "error: missing name")
}

func TestFrontDoor_PortedMultiTokenSubcommand(t *testing.T) {
	bin, defaultArgs := getHelperBinary(t)

	calledUserAdd := false
	fd := frontdoor.New(bin,
		frontdoor.WithOriginalArgs(defaultArgs...),
		frontdoor.WithPortedCommand("user add", func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
			calledUserAdd = true
			fmt.Fprintf(stdout, "user added: %v\n", args)
			return 0, nil
		}),
	)

	var stdout, stderr bytes.Buffer
	code, err := fd.ExecuteWithIO(context.Background(), []string{"user", "add", "bob", "--role=admin"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.True(t, calledUserAdd)
	assert.Contains(t, stdout.String(), "user added: [bob --role=admin]")
}

func TestFrontDoor_PortedCommandPrecededByFlags(t *testing.T) {
	bin, defaultArgs := getHelperBinary(t)

	called := false
	fd := frontdoor.New(bin,
		frontdoor.WithOriginalArgs(defaultArgs...),
		frontdoor.WithPortedCommand("status", func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
			called = true
			fmt.Fprintf(stdout, "status args: %v\n", args)
			return 0, nil
		}),
	)

	var stdout, stderr bytes.Buffer
	code, err := fd.ExecuteWithIO(context.Background(), []string{"--verbose", "-d", "status", "--json"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.True(t, called)
	assert.Contains(t, stdout.String(), "status args: [--verbose -d --json]")
}

func TestFrontDoor_UnportedTransparentPassthrough(t *testing.T) {
	_ = os.Setenv("GO_WANT_FRONTDOOR_HELPER", "1")
	defer func() {
		_ = os.Unsetenv("GO_WANT_FRONTDOOR_HELPER")
	}()

	bin, defaultArgs := getHelperBinary(t)

	fd := frontdoor.New(bin,
		frontdoor.WithOriginalArgs(defaultArgs...),
		frontdoor.WithEnv(append(os.Environ(), "TEST_FRONTDOOR_ENV=hello_from_fd")),
		frontdoor.WithPortedCommand("ported_only", func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
			return 0, nil
		}),
	)

	// 1. Transparent passthrough of argv to original binary
	var stdout, stderr bytes.Buffer
	code, err := fd.ExecuteWithIO(context.Background(), []string{"echo_args", "arg1", "arg2", "--flag=val"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "legacy args: arg1 arg2 --flag=val\n", stdout.String())

	// 2. Transparent passthrough of stdin
	stdout.Reset()
	stderr.Reset()
	stdin := strings.NewReader("sample payload from stdin")
	code, err = fd.ExecuteWithIO(context.Background(), []string{"echo_stdin"}, stdin, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "legacy stdin: sample payload from stdin\n", stdout.String())

	// 3. Transparent passthrough of environment variables
	stdout.Reset()
	stderr.Reset()
	code, err = fd.ExecuteWithIO(context.Background(), []string{"echo_env"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "legacy env: hello_from_fd\n", stdout.String())

	// 4. Transparent passthrough of stderr
	stdout.Reset()
	stderr.Reset()
	code, err = fd.ExecuteWithIO(context.Background(), []string{"write_stderr"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "legacy error output\n", stderr.String())

	// 5. Transparent passthrough of non-zero exit codes (e.g. code 42)
	stdout.Reset()
	stderr.Reset()
	code, err = fd.ExecuteWithIO(context.Background(), []string{"exit_code", "42"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 42, code, "exit status code 42 must be preserved completely unchanged")
	assert.Contains(t, stderr.String(), "legacy exiting with 42")
}

func TestFrontDoor_RouteOverrideKillSwitch(t *testing.T) {
	_ = os.Setenv("GO_WANT_FRONTDOOR_HELPER", "1")
	defer func() {
		_ = os.Unsetenv("GO_WANT_FRONTDOOR_HELPER")
	}()

	bin, defaultArgs := getHelperBinary(t)

	var hookCommand, hookUnit string
	var hookArgv []string

	overrideActive := false

	fd := frontdoor.New(bin,
		frontdoor.WithOriginalArgs(defaultArgs...),
		frontdoor.WithPortedUnitCommand("calc", "unit-calc", func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
			fmt.Fprintln(stdout, "ported calc native output")
			return 0, nil
		}),
		frontdoor.WithRouteOverride(func(cmd, unitID string) bool {
			return overrideActive && unitID == "unit-calc"
		}),
		frontdoor.WithFallbackHook(func(cmd, unitID string, argv []string) {
			hookCommand = cmd
			hookUnit = unitID
			hookArgv = argv
		}),
	)

	// A. When override is inactive: routes natively to ported implementation
	var stdout, stderr bytes.Buffer
	code, err := fd.ExecuteWithIO(context.Background(), []string{"calc", "1", "2"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "ported calc native output\n", stdout.String())
	assert.Empty(t, hookCommand, "fallback hook must not be called when native handler executes")

	// B. When override is active (kill switch triggered): transparently routes to original binary!
	overrideActive = true
	stdout.Reset()
	stderr.Reset()
	code, err = fd.ExecuteWithIO(context.Background(), []string{"calc", "1", "2"}, nil, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "legacy unported: calc\n", stdout.String())
	assert.Equal(t, "calc", hookCommand)
	assert.Equal(t, "unit-calc", hookUnit)
	assert.Equal(t, []string{"calc", "1", "2"}, hookArgv)
}

func TestFrontDoor_NonexistentBinaryFails(t *testing.T) {
	fd := frontdoor.New("/nonexistent/binary/path/that/cannot/exist")

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	code, err := fd.ExecuteWithIO(ctx, []string{"some_command"}, nil, &stdout, &stderr)
	assert.Error(t, err)
	assert.NotEqual(t, 0, code)
}
