package parity

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// InvocationResult holds the output, error, and panic details from an execution.
type InvocationResult struct {
	Values     []any         `json:"values,omitempty"`
	Error      string        `json:"error,omitempty"`
	Panicked   bool          `json:"panicked,omitempty"`
	PanicValue any           `json:"panic_value,omitempty"`
	PanicStack string        `json:"panic_stack,omitempty"`
	Duration   time.Duration `json:"duration,omitempty"`
}

// Divergence represents a detected behavioral discrepancy between legacy and modern functions.
type Divergence struct {
	FunctionName string              `json:"function_name"`
	InputArgs    []any               `json:"input_args"`
	Kind         adapterkit.CaseKind `json:"kind"`
	TrapType     string              `json:"trap_type,omitempty"`
	Difference   string              `json:"difference"`
	LegacyResult InvocationResult    `json:"legacy_result"`
	ModernResult InvocationResult    `json:"modern_result"`
}

// ToCase converts a detected Divergence into an adapterkit.Case compatible with recorder and comparator.
func (d *Divergence) ToCase(unitID string, caseNumber int) *adapterkit.Case {
	var expBody any
	if d.LegacyResult.Panicked {
		expBody = map[string]any{
			"panicked":    true,
			"panic_value": fmt.Sprintf("%v", d.LegacyResult.PanicValue),
		}
	} else if len(d.LegacyResult.Values) == 1 {
		expBody = d.LegacyResult.Values[0]
	} else if len(d.LegacyResult.Values) > 1 {
		expBody = d.LegacyResult.Values
	}

	output := adapterkit.CaseOutput{
		Body:         expBody,
		ErrorMessage: d.LegacyResult.Error,
	}

	metadata := map[string]any{
		"difference":       d.Difference,
		"function_name":    d.FunctionName,
		"modern_values":    d.ModernResult.Values,
		"modern_error":     d.ModernResult.Error,
		"modern_panicked":  d.ModernResult.Panicked,
		"modern_panic_val": fmt.Sprintf("%v", d.ModernResult.PanicValue),
	}

	return &adapterkit.Case{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		CaseID:        fmt.Sprintf("case-%s-%04d", unitID, caseNumber),
		UnitID:        unitID,
		Kind:          d.Kind,
		TrapType:      d.TrapType,
		Runtime:       "go",
		CapturedAt:    time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Method: d.FunctionName,
			Body: map[string]any{
				"function": d.FunctionName,
				"args":     d.InputArgs,
			},
		},
		ExpectedOutput: output,
		Metadata:       metadata,
	}
}

// InvokeSafe invokes fn with args using reflection, safely catching panics with recover().
func InvokeSafe(fn any, args ...any) (res InvocationResult) {
	start := time.Now()
	defer func() {
		res.Duration = time.Since(start)
		if r := recover(); r != nil {
			res.Panicked = true
			res.PanicValue = r
			res.PanicStack = string(debug.Stack())
		}
	}()

	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		res.Error = fmt.Sprintf("target is not a function: %T", fn)
		return res
	}

	fnType := v.Type()
	numIn := fnType.NumIn()
	isVariadic := fnType.IsVariadic()

	inValues := make([]reflect.Value, 0, len(args))
	for i, arg := range args {
		var targetType reflect.Type
		if isVariadic && i >= numIn-1 {
			targetType = fnType.In(numIn - 1).Elem()
		} else if i < numIn {
			targetType = fnType.In(i)
		}

		if arg == nil {
			if targetType != nil {
				inValues = append(inValues, reflect.Zero(targetType))
			} else {
				inValues = append(inValues, reflect.ValueOf(nil))
			}
			continue
		}

		argVal := reflect.ValueOf(arg)
		if targetType != nil && argVal.Type() != targetType {
			if argVal.Type().ConvertibleTo(targetType) {
				argVal = argVal.Convert(targetType)
			}
		}
		inValues = append(inValues, argVal)
	}

	out := v.Call(inValues)

	var errorInterface = reflect.TypeOf((*error)(nil)).Elem()
	for i, o := range out {
		// If last value implements error, treat as error
		if i == len(out)-1 && o.Type().Implements(errorInterface) {
			if !o.IsNil() {
				res.Error = o.Interface().(error).Error()
			}
		} else {
			res.Values = append(res.Values, o.Interface())
		}
	}

	return res
}

// CompareInvocations compares legacy and modern invocation results using go-cmp and panic inspection.
func CompareInvocations(fnName string, inputArgs []any, legacy, modern InvocationResult) *Divergence {
	// 1. Panic discrepancy
	if legacy.Panicked != modern.Panicked {
		return &Divergence{
			FunctionName: fnName,
			InputArgs:    inputArgs,
			Kind:         adapterkit.KindTrap,
			TrapType:     "panic_discrepancy",
			Difference: fmt.Sprintf("panic divergence: legacy panicked=%v (%v), modern panicked=%v (%v)",
				legacy.Panicked, legacy.PanicValue, modern.Panicked, modern.PanicValue),
			LegacyResult: legacy,
			ModernResult: modern,
		}
	}

	if legacy.Panicked && modern.Panicked {
		diff := cmp.Diff(fmt.Sprint(legacy.PanicValue), fmt.Sprint(modern.PanicValue))
		if diff != "" {
			return &Divergence{
				FunctionName: fnName,
				InputArgs:    inputArgs,
				Kind:         adapterkit.KindTrap,
				TrapType:     "panic_value_mismatch",
				Difference:   fmt.Sprintf("panic value mismatch (-legacy +modern):\n%s", diff),
				LegacyResult: legacy,
				ModernResult: modern,
			}
		}
		return nil
	}

	// 2. Error discrepancy
	if (legacy.Error == "") != (modern.Error == "") {
		return &Divergence{
			FunctionName: fnName,
			InputArgs:    inputArgs,
			Kind:         adapterkit.KindEdge,
			TrapType:     "error_discrepancy",
			Difference: fmt.Sprintf("error divergence: legacy error=%q, modern error=%q",
				legacy.Error, modern.Error),
			LegacyResult: legacy,
			ModernResult: modern,
		}
	}

	if legacy.Error != "" && modern.Error != "" {
		if legacy.Error != modern.Error {
			diff := cmp.Diff(legacy.Error, modern.Error)
			return &Divergence{
				FunctionName: fnName,
				InputArgs:    inputArgs,
				Kind:         adapterkit.KindEdge,
				Difference:   fmt.Sprintf("error message discrepancy (-legacy +modern):\n%s", diff),
				LegacyResult: legacy,
				ModernResult: modern,
			}
		}
	}

	// 3. Return values discrepancy
	diff := cmp.Diff(legacy.Values, modern.Values)
	if diff != "" {
		return &Divergence{
			FunctionName: fnName,
			InputArgs:    inputArgs,
			Kind:         adapterkit.KindHappy,
			Difference:   fmt.Sprintf("return values mismatch (-legacy +modern):\n%s", diff),
			LegacyResult: legacy,
			ModernResult: modern,
		}
	}

	return nil
}

// Driver coordinates differential execution between legacy and modern implementations.
type Driver struct {
	unitID string
}

// NewDriver creates a new parity test driver for unitID.
func NewDriver(unitID string) *Driver {
	return &Driver{
		unitID: unitID,
	}
}

// CompareFunction executes legacyFn and modernFn against inputs, returning all discovered divergences.
func (d *Driver) CompareFunction(fnName string, legacyFn, modernFn any, inputs [][]any) ([]*Divergence, error) {
	var divergences []*Divergence
	for _, args := range inputs {
		legacyRes := InvokeSafe(legacyFn, args...)
		modernRes := InvokeSafe(modernFn, args...)

		if div := CompareInvocations(fnName, args, legacyRes, modernRes); div != nil {
			divergences = append(divergences, div)
		}
	}
	return divergences, nil
}
