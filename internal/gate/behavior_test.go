package gate_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/gate"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReplayDriver implements adapterkit.Replay for testing.
type mockReplayDriver struct {
	executeCaseFunc func(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error)
}

func (m *mockReplayDriver) DriverName() string {
	return "mock-replay"
}

func (m *mockReplayDriver) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if m.executeCaseFunc != nil {
		return m.executeCaseFunc(ctx, testCase)
	}
	return &adapterkit.CaseOutput{StatusCode: 200}, nil
}

func (m *mockReplayDriver) Close() error {
	return nil
}

func TestCheckReplay_PassAndFail(t *testing.T) {
	ctx := context.Background()

	cases := []*adapterkit.Case{
		{
			CaseID: "case-001",
			UnitID: "test-unit",
			Kind:   adapterkit.KindHappy,
			Input:  adapterkit.CaseInput{Path: "/test"},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: 200,
				Body:       map[string]any{"ok": true},
			},
		},
	}

	t.Run("zero mismatches passes", func(t *testing.T) {
		driver := &mockReplayDriver{
			executeCaseFunc: func(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
				return &adapterkit.CaseOutput{
					StatusCode: 200,
					Body:       map[string]any{"ok": true},
				}, nil
			},
		}

		checker := &gate.ReplayChecker{
			UnitID: "test-unit",
			Cases:  cases,
			Driver: driver,
			Jobs:   2,
		}

		out, issues, passed, err := checker.Check(ctx)
		require.NoError(t, err)
		assert.True(t, passed)
		assert.Empty(t, issues)
		assert.Contains(t, out, "100% replay matched")
	})

	t.Run("mismatch fails", func(t *testing.T) {
		driver := &mockReplayDriver{
			executeCaseFunc: func(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
				return &adapterkit.CaseOutput{
					StatusCode: 500,
					Body:       map[string]any{"error": "internal"},
				}, nil
			},
		}

		checker := &gate.ReplayChecker{
			UnitID: "test-unit",
			Cases:  cases,
			Driver: driver,
			Jobs:   2,
		}

		out, issues, passed, err := checker.Check(ctx)
		require.NoError(t, err)
		assert.False(t, passed)
		assert.NotEmpty(t, issues)
		assert.Contains(t, issues[0], "case-001")
		assert.Contains(t, out, "Replay Report")
	})
}

func TestCheckErrorPaths_CoverageAndMissing(t *testing.T) {
	ctx := context.Background()

	sourceCode := `package payment

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrInvalidAmount = errors.New("invalid amount")

func Process(amount float64, currency string) error {
	if amount < 0 {
		return errors.New("negative amount not allowed")
	}
	if currency != "USD" {
		return fmt.Errorf("unsupported currency: %s", currency)
	}
	return nil
}

func Handle(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "unauthorized access", http.StatusUnauthorized)
}
`
	tempDir, err := os.MkdirTemp("", "ga-err-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "payment.go"), []byte(sourceCode), 0644))

	t.Run("AST extraction discovers all error paths", func(t *testing.T) {
		discovered, err := gate.ExtractErrorPathsFromDir(tempDir)
		require.NoError(t, err)
		require.NotEmpty(t, discovered)

		patterns := make(map[string]bool)
		for _, ep := range discovered {
			patterns[ep.Pattern] = true
		}

		assert.True(t, patterns["invalid amount"])
		assert.True(t, patterns["negative amount not allowed"])
		assert.True(t, patterns["unsupported currency"])
		assert.True(t, patterns["unauthorized access"])
	})

	t.Run("missing error path fails completeness check", func(t *testing.T) {
		// Only cover 2 of the 4 error paths
		partialCases := []*adapterkit.Case{
			{
				CaseID: "case-err-1",
				Kind:   adapterkit.KindTrap,
				ExpectedOutput: adapterkit.CaseOutput{
					StatusCode:   http.StatusBadRequest,
					ErrorMessage: "negative amount not allowed",
				},
			},
			{
				CaseID: "case-err-2",
				Kind:   adapterkit.KindTrap,
				ExpectedOutput: adapterkit.CaseOutput{
					StatusCode:   http.StatusBadRequest,
					ErrorMessage: "unsupported currency: EUR",
				},
			},
		}

		checker := &gate.ErrorPathsChecker{
			SourceDir: tempDir,
			Cases:     partialCases,
		}

		out, issues, passed, err := checker.Check(ctx)
		require.NoError(t, err)
		assert.False(t, passed)
		assert.NotEmpty(t, issues)
		assert.Contains(t, out, "Error-path completeness check failed")

		issueStr := issues[0] + " " + issues[len(issues)-1]
		assert.True(t, len(issues) >= 2)
		assert.Contains(t, issueStr, "invalid amount")
	})

	t.Run("full error path coverage passes completeness check", func(t *testing.T) {
		fullCases := []*adapterkit.Case{
			{
				CaseID: "case-err-1",
				Kind:   adapterkit.KindTrap,
				ExpectedOutput: adapterkit.CaseOutput{
					StatusCode:   http.StatusBadRequest,
					ErrorMessage: "invalid amount",
				},
			},
			{
				CaseID: "case-err-2",
				Kind:   adapterkit.KindTrap,
				ExpectedOutput: adapterkit.CaseOutput{
					StatusCode:   http.StatusBadRequest,
					ErrorMessage: "negative amount not allowed",
				},
			},
			{
				CaseID: "case-err-3",
				Kind:   adapterkit.KindTrap,
				ExpectedOutput: adapterkit.CaseOutput{
					StatusCode:   http.StatusBadRequest,
					ErrorMessage: "unsupported currency: EUR",
				},
			},
			{
				CaseID: "case-err-4",
				Kind:   adapterkit.KindTrap,
				ExpectedOutput: adapterkit.CaseOutput{
					StatusCode:   http.StatusUnauthorized,
					ErrorMessage: "unauthorized access",
				},
			},
		}

		checker := &gate.ErrorPathsChecker{
			SourceDir: tempDir,
			Cases:     fullCases,
		}

		out, issues, passed, err := checker.Check(ctx)
		require.NoError(t, err)
		assert.True(t, passed)
		assert.Empty(t, issues)
		assert.Contains(t, out, "Error-path completeness clean")
	})
}

func TestCheckAPICompatibility_CleanAndBreaking(t *testing.T) {
	legacyCode := `package converter

type Rate struct {
	From string
	To   string
	Ratio float64
}

type Converter interface {
	Convert(amount float64, from, to string) (float64, error)
}

func ConvertCurrency(amount float64, from, to string) (float64, error) {
	return 0, nil
}
`

	t.Run("identical API passes", func(t *testing.T) {
		legSurface, err := gate.ExtractAPISurfaceFromSource("legacy.go", legacyCode)
		require.NoError(t, err)

		modSurface, err := gate.ExtractAPISurfaceFromSource("modern.go", legacyCode)
		require.NoError(t, err)

		issues := gate.CompareAPIs(legSurface, modSurface)
		assert.Empty(t, issues)
	})

	t.Run("non-breaking addition passes", func(t *testing.T) {
		extendedModernCode := legacyCode + `
func AdditionalHelper() string {
	return "ok"
}

type ExtendedConfig struct {
	Timeout int
}
`
		legSurface, err := gate.ExtractAPISurfaceFromSource("legacy.go", legacyCode)
		require.NoError(t, err)

		modSurface, err := gate.ExtractAPISurfaceFromSource("modern.go", extendedModernCode)
		require.NoError(t, err)

		issues := gate.CompareAPIs(legSurface, modSurface)
		assert.Empty(t, issues)
	})

	t.Run("removed exported function triggers breaking change", func(t *testing.T) {
		breakingModernCode := `package converter

type Rate struct {
	From string
	To   string
	Ratio float64
}

type Converter interface {
	Convert(amount float64, from, to string) (float64, error)
}
`
		legSurface, err := gate.ExtractAPISurfaceFromSource("legacy.go", legacyCode)
		require.NoError(t, err)

		modSurface, err := gate.ExtractAPISurfaceFromSource("modern.go", breakingModernCode)
		require.NoError(t, err)

		issues := gate.CompareAPIs(legSurface, modSurface)
		require.Len(t, issues, 1)
		assert.Contains(t, issues[0], "removed exported func: ConvertCurrency")
	})

	t.Run("removed struct field triggers breaking change", func(t *testing.T) {
		breakingModernCode := `package converter

type Rate struct {
	From string
	To   string
}

type Converter interface {
	Convert(amount float64, from, to string) (float64, error)
}

func ConvertCurrency(amount float64, from, to string) (float64, error) {
	return 0, nil
}
`
		legSurface, err := gate.ExtractAPISurfaceFromSource("legacy.go", legacyCode)
		require.NoError(t, err)

		modSurface, err := gate.ExtractAPISurfaceFromSource("modern.go", breakingModernCode)
		require.NoError(t, err)

		issues := gate.CompareAPIs(legSurface, modSurface)
		require.Len(t, issues, 1)
		assert.Contains(t, issues[0], "removed exported field: Rate.Ratio")
	})

	t.Run("altered function signature triggers breaking change", func(t *testing.T) {
		breakingModernCode := `package converter

type Rate struct {
	From string
	To   string
	Ratio float64
}

type Converter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// Incompatible signature: amount is int instead of float64
func ConvertCurrency(amount int, from, to string) (float64, error) {
	return 0, nil
}
`
		legSurface, err := gate.ExtractAPISurfaceFromSource("legacy.go", legacyCode)
		require.NoError(t, err)

		modSurface, err := gate.ExtractAPISurfaceFromSource("modern.go", breakingModernCode)
		require.NoError(t, err)

		issues := gate.CompareAPIs(legSurface, modSurface)
		require.Len(t, issues, 1)
		assert.Contains(t, issues[0], "incompatible signature change for ConvertCurrency")
	})
}
