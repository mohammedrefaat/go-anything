package gate_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/gate"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCleanGoWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	modContent := "module example.com/cleancode\n\ngo 1.22.0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0644))

	goContent := "package cleancode\n\n// Compute returns forty-two.\nfunc Compute() int {\n\treturn 42\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.go"), []byte(goContent), 0644))

	testContent := "package cleancode\n\nimport \"testing\"\n\nfunc TestCompute(t *testing.T) {\n\tif Compute() != 42 {\n\t\tt.Fatal(\"expected 42\")\n\t}\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean_test.go"), []byte(testContent), 0644))

	return dir
}

func TestGateRunner_CleanCode(t *testing.T) {
	dir := setupCleanGoWorkspace(t)

	runner := gate.New(gate.Config{
		Dir:               dir,
		Checks:            []string{gate.CheckBuild, gate.CheckFormat, gate.CheckVet},
		AllowMissingTools: true,
	})

	report, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.True(t, report.Passed, "clean code should pass all standard checks")
	assert.Equal(t, 3, report.PassedCount)
	assert.Equal(t, 0, report.FailedCount)

	gateChecks := report.ToGateChecks()
	require.Len(t, gateChecks, 3)
	for _, gc := range gateChecks {
		assert.Equal(t, adapterkit.StatusPass, gc.Status)
		assert.GreaterOrEqual(t, gc.DurationMS, float64(0))
	}
}

func TestGateRunner_InjectedBuildFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/broken\n\ngo 1.22.0\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package broken\n\nfunc Incomplete(\n"), 0644))

	runner := gate.New(gate.Config{
		Dir:    dir,
		Checks: []string{gate.CheckBuild},
	})

	report, err := runner.Run(context.Background())
	require.NoError(t, err)
	assert.False(t, report.Passed)
	assert.Equal(t, 1, report.FailedCount)
	require.Len(t, report.Checks, 1)

	buildCheck := report.Checks[0]
	assert.Equal(t, gate.CheckBuild, buildCheck.Name)
	assert.False(t, buildCheck.Passed)
	assert.NotEmpty(t, buildCheck.Issues)
}

func TestGateRunner_InjectedVetIssue(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/vetissue\n\ngo 1.22.0\n"), 0644))
	// Printf type mismatch triggers go vet diagnostic
	brokenCode := "package vetissue\n\nimport \"fmt\"\n\nfunc PrintStuff() {\n\tfmt.Printf(\"%d\", \"not an integer\")\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vet.go"), []byte(brokenCode), 0644))

	runner := gate.New(gate.Config{
		Dir:    dir,
		Checks: []string{gate.CheckVet},
	})

	report, err := runner.Run(context.Background())
	require.NoError(t, err)
	assert.False(t, report.Passed)
	require.Len(t, report.Checks, 1)

	vetCheck := report.Checks[0]
	assert.Equal(t, gate.CheckVet, vetCheck.Name)
	assert.False(t, vetCheck.Passed)
	assert.NotEmpty(t, vetCheck.Issues)
}

func TestGateRunner_InjectedFormatIssue(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/unformatted\n\ngo 1.22.0\n"), 0644))
	// Intentionally malformatted code (spaces instead of tabs)
	unformatted := "package unformatted\n\nfunc    Messy( ) {\n      println( \"spaces\" )\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "messy.go"), []byte(unformatted), 0644))

	runner := gate.New(gate.Config{
		Dir:    dir,
		Checks: []string{gate.CheckFormat},
	})

	report, err := runner.Run(context.Background())
	require.NoError(t, err)
	assert.False(t, report.Passed)
	require.Len(t, report.Checks, 1)

	formatCheck := report.Checks[0]
	assert.Equal(t, gate.CheckFormat, formatCheck.Name)
	assert.False(t, formatCheck.Passed)
	assert.NotEmpty(t, formatCheck.Issues)
	assert.Contains(t, formatCheck.Issues[0], "messy.go")
}

func TestGateRunner_InjectedDataRace(t *testing.T) {
	// Custom executor verifying data race diagnostics handling
	raceExecutor := func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		output := "WARNING: DATA RACE\nWrite at 0x00c0000a6010 by goroutine 7:\n  example.com/race.Worker()\nPrevious write at 0x00c0000a6010 by goroutine 6:\n  example.com/race.Worker()\n"
		issues := []string{
			"WARNING: DATA RACE",
			"Write at 0x00c0000a6010 by goroutine 7",
			"Previous write at 0x00c0000a6010 by goroutine 6",
		}
		return output, issues, false, errors.New("data race detected")
	}

	runner := gate.New(gate.Config{
		Checks: []string{gate.CheckRace},
		Executors: map[string]gate.CheckExecutor{
			gate.CheckRace: raceExecutor,
		},
	})

	report, err := runner.Run(context.Background())
	require.NoError(t, err)
	assert.False(t, report.Passed)
	require.Len(t, report.Checks, 1)

	raceCheck := report.Checks[0]
	assert.Equal(t, gate.CheckRace, raceCheck.Name)
	assert.False(t, raceCheck.Passed)
	assert.Equal(t, gate.StatusFail, raceCheck.Status)
	assert.Contains(t, raceCheck.Output, "DATA RACE")
	assert.Contains(t, raceCheck.Issues[0], "DATA RACE")
}

func TestGateRunner_InjectedUnhandledError(t *testing.T) {
	// Custom executor verifying unhandled error and type assertion diagnostics (golangci-lint errcheck/forcetypeassert)
	lintExecutor := func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		output := "service.go:45:2: Error return value of `os.Open` is not checked (errcheck)\nservice.go:78:10: type assertion `val.(string)` should use comma-ok idiom (forcetypeassert)\n"
		issues := []string{
			"service.go:45:2: Error return value of `os.Open` is not checked (errcheck)",
			"service.go:78:10: type assertion `val.(string)` should use comma-ok idiom (forcetypeassert)",
		}
		return output, issues, false, errors.New("linter issues found")
	}

	runner := gate.New(gate.Config{
		Checks: []string{gate.CheckGolangciLint},
		Executors: map[string]gate.CheckExecutor{
			gate.CheckGolangciLint: lintExecutor,
		},
	})

	report, err := runner.Run(context.Background())
	require.NoError(t, err)
	assert.False(t, report.Passed)
	require.Len(t, report.Checks, 1)

	lintCheck := report.Checks[0]
	assert.Equal(t, gate.CheckGolangciLint, lintCheck.Name)
	assert.False(t, lintCheck.Passed)
	assert.Len(t, lintCheck.Issues, 2)
	assert.Contains(t, lintCheck.Issues[0], "errcheck")
	assert.Contains(t, lintCheck.Issues[1], "forcetypeassert")
}

func TestGateRunner_StaticcheckAndGovulncheck(t *testing.T) {
	staticcheckExecutor := func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		return "handler.go:12:5: SA4006: this value of `result` is never used", []string{
			"handler.go:12:5: SA4006: this value of `result` is never used",
		}, false, errors.New("staticcheck issue")
	}

	govulncheckExecutor := func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		return "Vulnerability #1: GO-2024-1234 in golang.org/x/crypto", []string{
			"GO-2024-1234 in golang.org/x/crypto",
		}, false, errors.New("vulnerabilities detected")
	}

	runner := gate.New(gate.Config{
		Checks: []string{gate.CheckStaticcheck, gate.CheckGovulncheck},
		Executors: map[string]gate.CheckExecutor{
			gate.CheckStaticcheck: staticcheckExecutor,
			gate.CheckGovulncheck: govulncheckExecutor,
		},
	})

	report, err := runner.Run(context.Background())
	require.NoError(t, err)
	assert.False(t, report.Passed)
	assert.Equal(t, 2, report.FailedCount)

	gateChecks := report.ToGateChecks()
	require.Len(t, gateChecks, 2)
	assert.Equal(t, adapterkit.StatusFail, gateChecks[0].Status)
	assert.Equal(t, adapterkit.StatusFail, gateChecks[1].Status)
}
