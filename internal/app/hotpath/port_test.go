package hotpath_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/app/hotpath"
	"github.com/mohammedrefaat/go-anything/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSandboxRunner struct {
	configs []sandbox.Config
	result  *sandbox.Result
	err     error
}

func (r *recordingSandboxRunner) Run(_ context.Context, cfg sandbox.Config) (*sandbox.Result, error) {
	r.configs = append(r.configs, cfg)
	if r.result != nil {
		return r.result, r.err
	}
	return &sandbox.Result{ExitCode: 0}, r.err
}

func assertSandboxedGoConfig(t *testing.T, cfg sandbox.Config, expectedSubcmd, expectedDir string) {
	t.Helper()
	assert.Equal(t, "go", cfg.Command)
	require.NotEmpty(t, cfg.Args)
	assert.Equal(t, expectedSubcmd, cfg.Args[0])
	assert.Equal(t, expectedDir, cfg.WorkingDir)
	assert.False(t, cfg.AllowNetwork, "network must be disabled for sandboxed sidecar builds")
	assert.Equal(t, "-mod=readonly", cfg.CustomEnv["GOFLAGS"])
	assert.Equal(t, "local", cfg.CustomEnv["GOTOOLCHAIN"])
	assert.Equal(t, "off", cfg.CustomEnv["GOPROXY"])
}

func TestDefaultHotpathPort_ReplayAndDiff_UsesSandbox(t *testing.T) {
	runner := &recordingSandboxRunner{
		result: &sandbox.Result{
			ExitCode:      0,
			IsolationMode: "best-effort",
			IsolationWarn: sandbox.BestEffortWarning,
		},
	}
	port := hotpath.NewDefaultHotpathPortWithSandbox(runner)

	dir := t.TempDir()
	unitID := filepath.Join("pkg", "billing")
	expectedOutDir := filepath.Join(dir, "bridges", unitID)
	expectedBinName := "sidecar"
	if os.PathSeparator == '\\' {
		expectedBinName += ".exe"
	}
	expectedBinPath := filepath.Join(expectedOutDir, expectedBinName)

	ok, msg, err := port.ReplayAndDiff(context.Background(), dir, unitID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Contains(t, msg, "0 replay mismatches")

	require.Len(t, runner.configs, 1)
	cfg := runner.configs[0]
	assertSandboxedGoConfig(t, cfg, "build", expectedOutDir)
	assert.Equal(t, []string{
		"build", "-o", expectedBinPath,
		"main.go", "types.go", "handlers.go", "handlers_impl.go",
	}, cfg.Args)
}

func TestDefaultHotpathPort_RunGate_UsesSandbox(t *testing.T) {
	runner := &recordingSandboxRunner{
		result: &sandbox.Result{
			ExitCode:      0,
			IsolationMode: "namespaces",
		},
	}
	port := hotpath.NewDefaultHotpathPortWithSandbox(runner)

	dir := t.TempDir()
	unitID := filepath.Join("pkg", "billing")
	expectedOutDir := filepath.Join(dir, "bridges", unitID)

	ok, msg, err := port.RunGate(context.Background(), dir, unitID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Contains(t, msg, "Quality Gate passed")

	require.Len(t, runner.configs, 1)
	cfg := runner.configs[0]
	assertSandboxedGoConfig(t, cfg, "vet", expectedOutDir)
	assert.Equal(t, []string{
		"vet", "main.go", "types.go", "handlers.go", "handlers_impl.go",
	}, cfg.Args)
}

func TestDefaultHotpathPort_SandboxedFailures(t *testing.T) {
	tests := []struct {
		name           string
		call           func(p *hotpath.DefaultHotpathPort, dir, unitID string) (bool, string, error)
		result         *sandbox.Result
		runErr         error
		wantMsgContain string
	}{
		{
			name: "build non-zero exit",
			call: func(p *hotpath.DefaultHotpathPort, dir, unitID string) (bool, string, error) {
				return p.ReplayAndDiff(context.Background(), dir, unitID)
			},
			result: &sandbox.Result{
				ExitCode: 2,
				Stderr:   "cannot find package",
			},
			wantMsgContain: "sidecar compilation failed: cannot find package",
		},
		{
			name: "build runner error",
			call: func(p *hotpath.DefaultHotpathPort, dir, unitID string) (bool, string, error) {
				return p.ReplayAndDiff(context.Background(), dir, unitID)
			},
			runErr:         errors.New("sandbox boom"),
			wantMsgContain: "sidecar compilation failed: sandbox boom",
		},
		{
			name: "vet non-zero exit",
			call: func(p *hotpath.DefaultHotpathPort, dir, unitID string) (bool, string, error) {
				return p.RunGate(context.Background(), dir, unitID)
			},
			result: &sandbox.Result{
				ExitCode: 1,
				Stderr:   "unreachable code",
			},
			wantMsgContain: "go vet failed on sidecar: unreachable code",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &recordingSandboxRunner{
				result: tc.result,
				err:    tc.runErr,
			}
			port := hotpath.NewDefaultHotpathPortWithSandbox(runner)
			ok, msg, err := tc.call(port, t.TempDir(), "unit")
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Contains(t, msg, tc.wantMsgContain)
		})
	}
}
