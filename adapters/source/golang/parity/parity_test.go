package parity_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mohammedrefat/go-anything/adapters/source/golang/parity"
	"github.com/mohammedrefat/go-anything/internal/analyzer"
	"github.com/mohammedrefat/go-anything/internal/comparator"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sample legacy and modern functions for differential testing
func legacyAdd(a, b int) int {
	return a + b
}

func modernAddDivergent(a, b int) int {
	// Injected bug
	return a + b + 1
}

func legacyDivide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("cannot divide by zero")
	}
	return a / b, nil
}

func modernDividePanics(a, b int) (int, error) {
	// Injected panic trap
	if b == 0 {
		panic("runtime error: integer divide by zero")
	}
	return a / b, nil
}

func modernDivideWrongError(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("invalid denominator: 0")
	}
	return a / b, nil
}

func TestInvokeSafe_NormalExecution(t *testing.T) {
	res := parity.InvokeSafe(legacyAdd, 10, 20)
	assert.False(t, res.Panicked)
	assert.Empty(t, res.Error)
	require.Len(t, res.Values, 1)
	assert.Equal(t, 30, res.Values[0])
}

func TestInvokeSafe_ErrorHandling(t *testing.T) {
	res := parity.InvokeSafe(legacyDivide, 10, 0)
	assert.False(t, res.Panicked)
	assert.Equal(t, "cannot divide by zero", res.Error)
	require.Len(t, res.Values, 1)
	assert.Equal(t, 0, res.Values[0])
}

func TestInvokeSafe_PanicRecoveryAndCharacterization(t *testing.T) {
	res := parity.InvokeSafe(modernDividePanics, 10, 0)
	assert.True(t, res.Panicked)
	assert.Equal(t, "runtime error: integer divide by zero", fmt.Sprint(res.PanicValue))
	assert.NotEmpty(t, res.PanicStack)
}

func TestCompareInvocations_PanicDivergence(t *testing.T) {
	// Legacy returns error gracefully; modern panics
	legacyRes := parity.InvokeSafe(legacyDivide, 10, 0)
	modernRes := parity.InvokeSafe(modernDividePanics, 10, 0)

	div := parity.CompareInvocations("Divide", []any{10, 0}, legacyRes, modernRes)
	require.NotNil(t, div)
	assert.Equal(t, adapterkit.KindTrap, div.Kind)
	assert.Equal(t, "panic_discrepancy", div.TrapType)
	assert.Contains(t, div.Difference, "legacy panicked=false")
	assert.Contains(t, div.Difference, "modern panicked=true")
}

func TestCompareInvocations_ErrorDivergence(t *testing.T) {
	legacyRes := parity.InvokeSafe(legacyDivide, 10, 0)
	modernRes := parity.InvokeSafe(modernDivideWrongError, 10, 0)

	div := parity.CompareInvocations("Divide", []any{10, 0}, legacyRes, modernRes)
	require.NotNil(t, div)
	assert.Equal(t, adapterkit.KindEdge, div.Kind)
	assert.Contains(t, div.Difference, "cannot divide by zero")
	assert.Contains(t, div.Difference, "invalid denominator: 0")
}

func TestCompareInvocations_ReturnValueDivergence(t *testing.T) {
	legacyRes := parity.InvokeSafe(legacyAdd, 5, 7)
	modernRes := parity.InvokeSafe(modernAddDivergent, 5, 7)

	div := parity.CompareInvocations("Add", []any{5, 7}, legacyRes, modernRes)
	require.NotNil(t, div)
	assert.Equal(t, adapterkit.KindHappy, div.Kind)
	assert.Contains(t, div.Difference, "int(12)")
	assert.Contains(t, div.Difference, "int(13)")
}

func TestDriver_CompareFunction(t *testing.T) {
	driver := parity.NewDriver("math-unit")
	inputs := [][]any{
		{1, 2}, // divergent
		{0, 0}, // divergent
	}

	divergences, err := driver.CompareFunction("Add", legacyAdd, modernAddDivergent, inputs)
	require.NoError(t, err)
	assert.Len(t, divergences, 2)
}

func TestDivergence_ToCaseAndJSONLCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	caseFilePath := filepath.Join(tmpDir, "cases.jsonl")

	legacyRes := parity.InvokeSafe(legacyDivide, 10, 0)
	modernRes := parity.InvokeSafe(modernDividePanics, 10, 0)
	div := parity.CompareInvocations("Divide", []any{10, 0}, legacyRes, modernRes)
	require.NotNil(t, div)

	c := div.ToCase("calc-unit", 1)
	require.NotNil(t, c)
	assert.Equal(t, "case-calc-unit-0001", c.CaseID)
	assert.Equal(t, "calc-unit", c.UnitID)
	assert.Equal(t, adapterkit.KindTrap, c.Kind)
	assert.Equal(t, "cannot divide by zero", c.ExpectedOutput.ErrorMessage)

	// Save to JSONL
	err := parity.SaveCases(caseFilePath, []*adapterkit.Case{c})
	require.NoError(t, err)

	// Read back
	loaded, err := parity.ReadCases(caseFilePath)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, c.CaseID, loaded[0].CaseID)
	assert.Equal(t, c.Kind, loaded[0].Kind)
	assert.Equal(t, c.ExpectedOutput.ErrorMessage, loaded[0].ExpectedOutput.ErrorMessage)

	// Test compatibility with internal/comparator
	comp := comparator.New()
	compRes, err := comp.Compare(comparator.ModeExact, c.ExpectedOutput.ErrorMessage, loaded[0].ExpectedOutput.ErrorMessage)
	require.NoError(t, err)
	assert.True(t, compRes.Passed)
}

func TestGenerator_CompareAPIsAndGenerateDriver(t *testing.T) {
	legacyPkg := &analyzer.PackageInfo{
		PkgPath: "example.com/legacy/calc",
		PublicAPIs: []string{
			"func Add",
			"func Subtract",
			"func DeprecatedCalc",
			"type Calculator",
		},
	}

	modernPkg := &analyzer.PackageInfo{
		PkgPath: "example.com/modern/calc",
		PublicAPIs: []string{
			"func Add",
			"func Subtract",
			"func Multiply",
			"type Calculator",
		},
	}

	gen := parity.NewGenerator()
	pairing := gen.CompareAPIs(legacyPkg, modernPkg)

	assert.Equal(t, []string{"Add", "Subtract"}, pairing.SharedFunctions)
	assert.Equal(t, []string{"Calculator"}, pairing.SharedTypes)
	assert.Equal(t, []string{"func DeprecatedCalc"}, pairing.LegacyOnlyAPIs)
	assert.Equal(t, []string{"func Multiply"}, pairing.ModernOnlyAPIs)

	code, err := gen.GenerateTestDriver(legacyPkg, modernPkg, parity.GeneratorOptions{
		PackageName: "calc_parity_test",
		UnitID:      "calc",
	})
	require.NoError(t, err)
	assert.Contains(t, code, "package calc_parity_test")
	assert.Contains(t, code, "TestParity_Add")
	assert.Contains(t, code, "TestParity_Subtract")
	assert.NotContains(t, code, "TestParity_DeprecatedCalc")
}
