package cli_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/adapters/application/cli"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelperCLIProcess(t *testing.T) {
	if os.Getenv("GO_WANT_CLI_HELPER") != "1" {
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
		fmt.Println(`{"status":"default"}`)
		return
	}

	cmd := args[0]
	switch cmd {
	case "echo_json":
		fmt.Printf(`{"status":"ok","msg":"%s","env":"%s"}`+"\n", args[1], os.Getenv("TEST_CLI_VAR"))
	case "read_stdin":
		data, _ := io.ReadAll(os.Stdin)
		fmt.Printf(`{"stdin":%q}`+"\n", string(data))
	case "fail":
		code := 1
		if len(args) > 1 {
			if c, err := strconv.Atoi(args[1]); err == nil {
				code = c
			}
		}
		fmt.Fprintf(os.Stderr, "command failed with code %d\n", code)
		os.Exit(code)
	default:
		fmt.Printf(`{"unknown":"%s"}`+"\n", cmd)
	}
}

func TestCLIAdapter_ExecuteSuccess(t *testing.T) {
	_ = os.Setenv("GO_WANT_CLI_HELPER", "1")
	defer func() {
		_ = os.Unsetenv("GO_WANT_CLI_HELPER")
	}()

	adapter := cli.NewAdapter(
		os.Args[0],
		cli.WithTimeout(10*time.Second),
		cli.WithEnv(map[string]string{
			"GO_WANT_CLI_HELPER": "1",
			"TEST_CLI_VAR":       "val_from_adapter",
		}),
	)
	defer adapter.Close()

	assert.Contains(t, adapter.DriverName(), "cli:")

	input := adapterkit.CaseInput{
		Argv: []string{"-test.run=TestHelperCLIProcess", "--", "echo_json", "hello_world"},
	}

	out, err := adapter.Execute(context.Background(), input)
	require.NoError(t, err)

	assert.Equal(t, 0, out.ExitCode)
	assert.Contains(t, out.Stdout, `"hello_world"`)

	bodyMap, ok := out.Body.(map[string]any)
	require.True(t, ok, "stdout should be parsed as JSON body")
	assert.Equal(t, "ok", bodyMap["status"])
	assert.Equal(t, "hello_world", bodyMap["msg"])
	assert.Equal(t, "val_from_adapter", bodyMap["env"])
}

func TestCLIAdapter_ExecuteWithStdin(t *testing.T) {
	_ = os.Setenv("GO_WANT_CLI_HELPER", "1")
	defer func() {
		_ = os.Unsetenv("GO_WANT_CLI_HELPER")
	}()

	adapter := cli.NewAdapter(
		os.Args[0],
		cli.WithEnv(map[string]string{"GO_WANT_CLI_HELPER": "1"}),
	)
	defer adapter.Close()

	input := adapterkit.CaseInput{
		Argv: []string{"-test.run=TestHelperCLIProcess", "--", "read_stdin"},
		Body: "test payload from body",
	}

	out, err := adapter.Execute(context.Background(), input)
	require.NoError(t, err)

	assert.Equal(t, 0, out.ExitCode)
	bodyMap, ok := out.Body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test payload from body", bodyMap["stdin"])
}

func TestCLIAdapter_ExecuteFailure(t *testing.T) {
	_ = os.Setenv("GO_WANT_CLI_HELPER", "1")
	defer func() {
		_ = os.Unsetenv("GO_WANT_CLI_HELPER")
	}()

	adapter := cli.NewAdapter(
		os.Args[0],
		cli.WithEnv(map[string]string{"GO_WANT_CLI_HELPER": "1"}),
	)
	defer adapter.Close()

	c := &adapterkit.Case{
		CaseID: "cli-fail-case",
		UnitID: "cli-test",
		Kind:   adapterkit.KindTrap,
		Input: adapterkit.CaseInput{
			Argv: []string{"-test.run=TestHelperCLIProcess", "--", "fail", "42"},
		},
	}

	out, err := adapter.ExecuteCase(context.Background(), c)
	require.NoError(t, err)

	assert.Equal(t, 42, out.ExitCode)
	assert.True(t, strings.Contains(out.Stderr, "command failed with code 42"))
	assert.NotEmpty(t, out.ErrorMessage)
}
