package comparator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CompareExact performs strict byte-for-byte or literal equality comparison.
func (c *Comparator) compareExact(expected, actual []byte) []adapterkit.Mismatch {
	if bytes.Equal(expected, actual) {
		return nil
	}
	return []adapterkit.Mismatch{
		{
			Path:     "$",
			Expected: string(expected),
			Actual:   string(actual),
			Message:  "exact byte mismatch between expected and actual output",
		},
	}
}

// CompareNumericTolerance compares two floating-point numbers or numeric strings within a tolerance delta.
func (c *Comparator) compareNumericTolerance(expected, actual string, delta float64) []adapterkit.Mismatch {
	expF, err1 := strconv.ParseFloat(strings.TrimSpace(expected), 64)
	actF, err2 := strconv.ParseFloat(strings.TrimSpace(actual), 64)
	if err1 != nil || err2 != nil {
		return []adapterkit.Mismatch{
			{
				Path:     "$",
				Expected: expected,
				Actual:   actual,
				Message:  fmt.Sprintf("numeric parse error: expected(%v), actual(%v)", err1, err2),
			},
		}
	}

	diff := math.Abs(expF - actF)
	if diff > delta {
		return []adapterkit.Mismatch{
			{
				Path:     "$",
				Expected: expF,
				Actual:   actF,
				Message:  fmt.Sprintf("numeric tolerance exceeded: delta %e > allowed %e", diff, delta),
			},
		}
	}
	return nil
}

// CompareErrorEquivalence evaluates whether two errors or exit codes are semantically equivalent.
func (c *Comparator) compareErrorEquivalence(expErr, actErr string, expCode, actCode int) []adapterkit.Mismatch {
	var mismatches []adapterkit.Mismatch

	// 1. Exit code comparison
	if expCode != actCode {
		mismatches = append(mismatches, adapterkit.Mismatch{
			Path:     "exit_code",
			Expected: expCode,
			Actual:   actCode,
			Message:  fmt.Sprintf("exit code mismatch: expected %d, got %d", expCode, actCode),
		})
	}

	// 2. Error message comparison
	cleanExp := strings.TrimSpace(strings.ToLower(expErr))
	cleanAct := strings.TrimSpace(strings.ToLower(actErr))

	if cleanExp != cleanAct {
		// Allow prefix or substring equivalence if one contains the other's core message
		if cleanExp != "" && cleanAct != "" && !strings.Contains(cleanAct, cleanExp) && !strings.Contains(cleanExp, cleanAct) {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     "error_message",
				Expected: expErr,
				Actual:   actErr,
				Message:  fmt.Sprintf("error message discrepancy: expected %q, got %q", expErr, actErr),
			})
		}
	}

	return mismatches
}

// CompareHTTPContract compares HTTP status codes, headers, and response payloads.
func (c *Comparator) compareHTTPContract(expected, actual *adapterkit.CaseOutput) []adapterkit.Mismatch {
	var mismatches []adapterkit.Mismatch

	if expected == nil || actual == nil {
		mismatches = append(mismatches, adapterkit.Mismatch{
			Path:    "$",
			Message: "missing HTTP response payload",
		})
		return mismatches
	}

	// 1. Status Code
	if expected.StatusCode != actual.StatusCode {
		mismatches = append(mismatches, adapterkit.Mismatch{
			Path:     "status_code",
			Expected: expected.StatusCode,
			Actual:   actual.StatusCode,
			Message:  fmt.Sprintf("HTTP status code mismatch: expected %d, got %d", expected.StatusCode, actual.StatusCode),
		})
	}

	// 2. Headers
	jc := NewJSONComparator(c.ruleEngine, c.numericDelta)
	normExpHeaders := normalizeHeaderMap(expected.Headers)
	normActHeaders := normalizeHeaderMap(actual.Headers)

	for k, expV := range normExpHeaders {
		headerPath := "headers." + k
		if c.ruleEngine != nil && c.ruleEngine.IsWildcard(headerPath, normActHeaders[k]) {
			continue // skip volatile header (e.g. Date, X-Request-Id)
		}
		actV, ok := normActHeaders[k]
		if !ok {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     headerPath,
				Expected: expV,
				Actual:   "<missing>",
				Message:  fmt.Sprintf("missing expected HTTP header %q", k),
			})
			continue
		}
		if c.ruleEngine != nil {
			expV = c.ruleEngine.NormalizeString(headerPath, expV)
			actV = c.ruleEngine.NormalizeString(headerPath, actV)
		}
		if expV != actV {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     headerPath,
				Expected: expV,
				Actual:   actV,
				Message:  fmt.Sprintf("header %q value mismatch: expected %q, got %q", k, expV, actV),
			})
		}
	}

	// 3. Body
	expBodyBytes := toBytes(expected.Body)
	actBodyBytes := toBytes(actual.Body)

	if len(expBodyBytes) > 0 || len(actBodyBytes) > 0 {
		// Attempt structural JSON comparison
		bodyMismatches, err := jc.CompareJSON(expBodyBytes, actBodyBytes)
		if err == nil {
			mismatches = append(mismatches, bodyMismatches...)
		} else {
			// Fallback to exact comparison for non-JSON payloads
			exactMismatches := c.compareExact(expBodyBytes, actBodyBytes)
			mismatches = append(mismatches, exactMismatches...)
		}
	}

	return mismatches
}

// CompareFilesystemSnapshot compares files, presence, and content.
func (c *Comparator) compareFilesystemSnapshot(expected, actual map[string]string) []adapterkit.Mismatch {
	var mismatches []adapterkit.Mismatch

	for path, expContent := range expected {
		actContent, exists := actual[path]
		if !exists {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     "files." + path,
				Expected: "<present>",
				Actual:   "<missing>",
				Message:  fmt.Sprintf("missing expected file in snapshot: %s", path),
			})
			continue
		}

		if expContent != actContent {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     "files." + path,
				Expected: expContent,
				Actual:   actContent,
				Message:  fmt.Sprintf("file content mismatch at: %s", path),
			})
		}
	}

	for path := range actual {
		if _, exists := expected[path]; !exists {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     "files." + path,
				Expected: "<missing>",
				Actual:   "<present>",
				Message:  fmt.Sprintf("unexpected additional file in snapshot: %s", path),
			})
		}
	}

	// Deterministic sort by path (Rule 2)
	sort.Slice(mismatches, func(i, j int) bool {
		return mismatches[i].Path < mismatches[j].Path
	})

	return mismatches
}

func normalizeHeaderMap(headers map[string]string) map[string]string {
	res := make(map[string]string, len(headers))
	for k, v := range headers {
		normKey := strings.ToLower(http.CanonicalHeaderKey(k))
		res[normKey] = strings.TrimSpace(v)
	}
	return res
}

func toBytes(v any) []byte {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []byte:
		return val
	case string:
		return []byte(val)
	default:
		b, _ := json.Marshal(val)
		return b
	}
}
