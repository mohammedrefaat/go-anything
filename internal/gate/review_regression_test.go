package gate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/require"
)

func TestErrorPathImportBindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		count        int
	}{
		{"alias", `package p; import errs "errors"; func f() error { return errs.New("bad") }`, 1},
		{"standard", `package p; import "errors"; func f() error { return errors.New("bad") }`, 1},
		{"unrelated", `package p; import errors "example.com/errors"; func f() error { return errors.New("bad") }`, 0},
		{"shadow", `package p; import "errors"; func f() { errors := struct{New func(string) error}{}; errors.New("bad") }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths, err := ExtractErrorPathsFromSource("p.go", tc.source)
			require.NoError(t, err)
			require.Len(t, paths, tc.count)
		})
	}
}
func TestErrorPathStatusAndMessage(t *testing.T) {
	paths, err := ExtractErrorPathsFromSource("p.go", `package p; import "net/http"; func f(w http.ResponseWriter) { http.Error(w,"slow down", http.StatusTooManyRequests) }`)
	require.NoError(t, err)
	require.Len(t, paths, 1)
	require.Equal(t, 429, paths[0].StatusCode)
	for _, tc := range []struct {
		status   int
		pattern  string
		expected bool
	}{{429, "slow down", true}, {400, "slow down", false}, {429, "other", false}} {
		c := &adapterkit.Case{ExpectedOutput: adapterkit.CaseOutput{StatusCode: tc.status, Body: tc.pattern}}
		require.Equal(t, tc.expected, caseMatchesErrorPath(c, paths[0]))
	}
	require.True(t, caseMatchesErrorPath(&adapterkit.Case{ExpectedOutput: adapterkit.CaseOutput{ErrorMessage: "slow down"}}, ErrorPath{Pattern: "slow down"}))
}
func TestSkippedExecutorEvidence(t *testing.T) {
	r := New(Config{Checks: []string{CheckStaticcheck}, Executors: map[string]CheckExecutor{CheckStaticcheck: func(context.Context, string, []string) (string, []string, bool, error) {
		return "missing", nil, false, ErrSkipped
	}}})
	report, err := r.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, report.PassedCount)
	require.Equal(t, 1, report.SkippedCount)
	require.Equal(t, adapterkit.StatusSkipped, report.ToGateChecks()[0].Status)
}
func TestStaticcheckFallbackDiagnostics(t *testing.T) {
	dir := t.TempDir()
	binName := "golangci-lint"
	script := "#!/bin/sh\necho 'SA1000: diagnostic'\nexit 1\n"
	if runtime.GOOS == "windows" {
		binName = "golangci-lint.bat"
		script = "@echo SA1000: diagnostic\r\n@exit /b 1\r\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, binName), []byte(script), 0755))
	if runtime.GOOS == "windows" {
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("SystemRoot")+`\System32`)
	} else {
		t.Setenv("PATH", dir)
	}
	out, issues, pass, err := defaultStaticcheckExecutor(true)(context.Background(), dir, nil)
	require.False(t, pass)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrSkipped)
	require.NotEmpty(t, issues)
	require.Contains(t, out, "SA1000")
}
func TestLargeJSONLCase(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cases.jsonl")
	require.NoError(t, os.WriteFile(file, []byte(`{"case_id":"`+strings.Repeat("x", 100000)+`"}`), 0600))
	cases, err := loadCasesFromJSONL(file)
	require.NoError(t, err)
	require.Len(t, cases, 1)
}
