package comparator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// JSONComparator compares JSON structures enforcing missing vs null, integer vs float, and array ordering.
type JSONComparator struct {
	rules            *RuleEngine
	defaultTolerance float64
}

// NewJSONComparator creates a JSONComparator.
func NewJSONComparator(rules *RuleEngine, defaultTolerance float64) *JSONComparator {
	if defaultTolerance <= 0 {
		defaultTolerance = 1e-6
	}
	return &JSONComparator{
		rules:            rules,
		defaultTolerance: defaultTolerance,
	}
}

// CompareJSON parses two JSON inputs with UseNumber and returns structural mismatches.
func (jc *JSONComparator) CompareJSON(expectedJSON, actualJSON []byte) ([]adapterkit.Mismatch, error) {
	expVal, err := parseJSONWithNumber(expectedJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse expected JSON: %w", err)
	}

	actVal, err := parseJSONWithNumber(actualJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse actual JSON: %w", err)
	}

	var mismatches []adapterkit.Mismatch
	jc.diffValues("$", expVal, actVal, &mismatches)
	return mismatches, nil
}

func parseJSONWithNumber(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var val any
	if err := dec.Decode(&val); err != nil {
		return nil, err
	}
	return val, nil
}

func (jc *JSONComparator) diffValues(path string, expected, actual any, mismatches *[]adapterkit.Mismatch) {
	// 1. Wildcard check
	if jc.rules != nil && jc.rules.IsWildcard(path, actual) {
		return // Wildcard matches, ignore difference
	}

	// 2. Both null
	if expected == nil && actual == nil {
		return
	}

	// 3. One is null, one is non-null
	if expected == nil && actual != nil {
		*mismatches = append(*mismatches, adapterkit.Mismatch{
			Path:     path,
			Expected: nil,
			Actual:   actual,
			Message:  fmt.Sprintf("expected null at %s, got %v", path, actual),
		})
		return
	}
	if expected != nil && actual == nil {
		*mismatches = append(*mismatches, adapterkit.Mismatch{
			Path:     path,
			Expected: expected,
			Actual:   nil,
			Message:  fmt.Sprintf("expected %v at %s, got null", expected, path),
		})
		return
	}

	// 4. Type comparisons
	switch exp := expected.(type) {
	case map[string]any:
		act, ok := actual.(map[string]any)
		if !ok {
			*mismatches = append(*mismatches, adapterkit.Mismatch{
				Path:     path,
				Expected: "object",
				Actual:   fmt.Sprintf("%T", actual),
				Message:  fmt.Sprintf("type mismatch at %s: expected object, got %T", path, actual),
			})
			return
		}
		jc.diffObjects(path, exp, act, mismatches)

	case []any:
		act, ok := actual.([]any)
		if !ok {
			*mismatches = append(*mismatches, adapterkit.Mismatch{
				Path:     path,
				Expected: "array",
				Actual:   fmt.Sprintf("%T", actual),
				Message:  fmt.Sprintf("type mismatch at %s: expected array, got %T", path, actual),
			})
			return
		}
		jc.diffArrays(path, exp, act, mismatches)

	case json.Number:
		act, ok := actual.(json.Number)
		if !ok {
			*mismatches = append(*mismatches, adapterkit.Mismatch{
				Path:     path,
				Expected: exp.String(),
				Actual:   fmt.Sprintf("%v", actual),
				Message:  fmt.Sprintf("type mismatch at %s: expected number, got %T", path, actual),
			})
			return
		}
		jc.diffNumbers(path, exp, act, mismatches)

	case string:
		act, ok := actual.(string)
		if !ok {
			*mismatches = append(*mismatches, adapterkit.Mismatch{
				Path:     path,
				Expected: exp,
				Actual:   fmt.Sprintf("%v", actual),
				Message:  fmt.Sprintf("type mismatch at %s: expected string, got %T", path, actual),
			})
			return
		}
		if jc.rules != nil {
			exp = jc.rules.NormalizeString(path, exp)
			act = jc.rules.NormalizeString(path, act)
		}
		if exp != act {
			*mismatches = append(*mismatches, adapterkit.Mismatch{
				Path:     path,
				Expected: exp,
				Actual:   act,
				Message:  fmt.Sprintf("string mismatch at %s: expected %q, got %q", path, exp, act),
			})
		}

	case bool:
		act, ok := actual.(bool)
		if !ok || exp != act {
			*mismatches = append(*mismatches, adapterkit.Mismatch{
				Path:     path,
				Expected: exp,
				Actual:   actual,
				Message:  fmt.Sprintf("boolean mismatch at %s: expected %v, got %v", path, exp, actual),
			})
		}

	default:
		if fmt.Sprintf("%v", expected) != fmt.Sprintf("%v", actual) {
			*mismatches = append(*mismatches, adapterkit.Mismatch{
				Path:     path,
				Expected: expected,
				Actual:   actual,
				Message:  fmt.Sprintf("value mismatch at %s: expected %v, got %v", path, expected, actual),
			})
		}
	}
}

func (jc *JSONComparator) diffObjects(path string, expected, actual map[string]any, mismatches *[]adapterkit.Mismatch) {
	// Keys in expected
	for k, expVal := range expected {
		subPath := subPath(path, k)
		actVal, present := actual[k]

		if !present {
			if expVal == nil {
				// Missing vs Null semantic distinction!
				*mismatches = append(*mismatches, adapterkit.Mismatch{
					Path:     subPath,
					Expected: "null",
					Actual:   "<missing>",
					Message:  fmt.Sprintf("semantic trap: key %q is missing in actual (expected null)", k),
				})
			} else {
				*mismatches = append(*mismatches, adapterkit.Mismatch{
					Path:     subPath,
					Expected: expVal,
					Actual:   "<missing>",
					Message:  fmt.Sprintf("missing key %q in actual object", k),
				})
			}
			continue
		}

		jc.diffValues(subPath, expVal, actVal, mismatches)
	}

	// Unexpected keys in actual
	for k, actVal := range actual {
		if _, present := expected[k]; !present {
			subPath := subPath(path, k)
			if actVal == nil {
				// Present null vs missing semantic distinction!
				*mismatches = append(*mismatches, adapterkit.Mismatch{
					Path:     subPath,
					Expected: "<missing>",
					Actual:   "null",
					Message:  fmt.Sprintf("semantic trap: key %q is present as null in actual (expected missing)", k),
				})
			} else {
				*mismatches = append(*mismatches, adapterkit.Mismatch{
					Path:     subPath,
					Expected: "<missing>",
					Actual:   actVal,
					Message:  fmt.Sprintf("unexpected key %q in actual object", k),
				})
			}
		}
	}
}

func (jc *JSONComparator) diffArrays(path string, expected, actual []any, mismatches *[]adapterkit.Mismatch) {
	// Check if array should be sorted before comparison (ordered vs unordered semantic distinction)
	if jc.rules != nil && jc.rules.ShouldSortArray(path) {
		expected = sortAnySlice(expected)
		actual = sortAnySlice(actual)
	}

	if len(expected) != len(actual) {
		*mismatches = append(*mismatches, adapterkit.Mismatch{
			Path:     path,
			Expected: fmt.Sprintf("length %d", len(expected)),
			Actual:   fmt.Sprintf("length %d", len(actual)),
			Message:  fmt.Sprintf("array length mismatch at %s: expected %d items, got %d items", path, len(expected), len(actual)),
		})
	}

	minLen := len(expected)
	if len(actual) < minLen {
		minLen = len(actual)
	}

	for i := 0; i < minLen; i++ {
		subPath := fmt.Sprintf("%s[%d]", path, i)
		jc.diffValues(subPath, expected[i], actual[i], mismatches)
	}
}

func (jc *JSONComparator) diffNumbers(path string, exp, act json.Number, mismatches *[]adapterkit.Mismatch) {
	expStr := exp.String()
	actStr := act.String()

	expIsFloat := strings.ContainsAny(expStr, ".eE")
	actIsFloat := strings.ContainsAny(actStr, ".eE")

	// Semantic distinction: Integer vs Float!
	if expIsFloat != actIsFloat {
		*mismatches = append(*mismatches, adapterkit.Mismatch{
			Path:     path,
			Expected: expStr,
			Actual:   actStr,
			Message:  fmt.Sprintf("number representation mismatch at %s: expected (float: %t), got (float: %t)", path, expIsFloat, actIsFloat),
		})
		return
	}

	// Compare numerical values
	expFloat, err1 := exp.Float64()
	actFloat, err2 := act.Float64()
	if err1 != nil || err2 != nil {
		if expStr != actStr {
			*mismatches = append(*mismatches, adapterkit.Mismatch{
				Path:     path,
				Expected: expStr,
				Actual:   actStr,
				Message:  fmt.Sprintf("number mismatch at %s: %s != %s", path, expStr, actStr),
			})
		}
		return
	}

	tolerance := jc.defaultTolerance
	if jc.rules != nil {
		if t, ok := jc.rules.GetFloatTolerance(path); ok {
			tolerance = t
		}
	}

	diff := math.Abs(expFloat - actFloat)
	if diff > tolerance {
		*mismatches = append(*mismatches, adapterkit.Mismatch{
			Path:     path,
			Expected: expFloat,
			Actual:   actFloat,
			Message:  fmt.Sprintf("numeric difference at %s: delta %e exceeds tolerance %e", path, diff, tolerance),
		})
	}
}

func subPath(base, key string) string {
	if base == "$" {
		return "$." + key
	}
	return base + "." + key
}

func sortAnySlice(slice []any) []any {
	copied := make([]any, len(slice))
	copy(copied, slice)
	sort.SliceStable(copied, func(i, j int) bool {
		sI := serializeComparable(copied[i])
		sJ := serializeComparable(copied[j])
		return sI < sJ
	})
	return copied
}

func serializeComparable(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return fmt.Sprintf("%020.8f", f)
		}
		return v.String()
	case float64:
		return fmt.Sprintf("%020.8f", v)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
