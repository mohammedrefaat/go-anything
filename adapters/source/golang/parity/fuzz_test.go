package parity_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/adapters/source/golang/parity"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Legacy function: safely parses and processes input string
func legacyProcess(input string) (string, error) {
	if strings.Contains(input, "\x00") {
		return "null_escaped", nil
	}
	if len(input) > 200 {
		return input[:200], nil
	}
	return strings.ToUpper(input), nil
}

// Modern function with an injected bug: panics on null bytes or fails on long strings
func modernProcessWithBug(input string) (string, error) {
	if strings.Contains(input, "\x00") {
		panic("unexpected null byte in input")
	}
	if len(input) > 200 {
		return "OVERFLOW", nil
	}
	return strings.ToUpper(input), nil
}

func TestDifferentialFuzzer_FindsInjectedBug(t *testing.T) {
	tmpDir := t.TempDir()
	outJSONL := filepath.Join(tmpDir, "fuzz_divergences.jsonl")

	cfg := parity.FuzzConfig{
		UnitID:     "fuzz-string-unit",
		Iterations: 100,
		Seed:       42,
		Timeout:    2 * time.Second,
		OutputFile: outJSONL,
	}

	fuzzer := parity.NewDifferentialFuzzer(cfg)
	seeds := [][]any{
		{"hello"},
		{"world123"},
		{""},
	}

	res, err := fuzzer.Fuzz("ProcessString", legacyProcess, modernProcessWithBug, seeds)
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.GreaterOrEqual(t, res.TotalRuns, 100)
	assert.NotEmpty(t, res.FuzzCases, "fuzzer must catch the injected null-byte panic or buffer overflow")

	// Validate case format
	for _, c := range res.FuzzCases {
		assert.Equal(t, adapterkit.KindFuzz, c.Kind)
		assert.Equal(t, "fuzz-string-unit", c.UnitID)
		assert.NotEmpty(t, c.CaseID)
	}

	// Validate persisted JSONL
	loaded, err := parity.ReadCases(outJSONL)
	require.NoError(t, err)
	assert.Equal(t, len(res.FuzzCases), len(loaded))
}
