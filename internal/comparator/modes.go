package comparator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
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

var volatileEffectHeaders = map[string]bool{
	"date":          true,
	"message-id":    true,
	"authorization": true,
	"x-request-id":  true,
}

// CompareEffectMultiset evaluates whether actual side effects match expected side effects as a multiset.
// Extra effects = divergence! Missing effects = divergence!
// Volatile headers (Date, Message-ID, Authorization, X-Request-Id) are ignored.
func (c *Comparator) CompareEffectMultiset(expected, actual []adapterkit.Effect) []adapterkit.Mismatch {
	var mismatches []adapterkit.Mismatch

	matchedExp := make([]bool, len(expected))
	matchedAct := make([]bool, len(actual))

	// Pass 1: Greedily match exactly equivalent effects
	for i, exp := range expected {
		for j, act := range actual {
			if matchedAct[j] {
				continue
			}
			if c.effectsEquivalent(exp, act) {
				matchedExp[i] = true
				matchedAct[j] = true
				break
			}
		}
	}

	// Pass 2: For remaining unmatched, pair items of the same Type & Target/Subject
	// to report detailed field-level discrepancies.
	for i, exp := range expected {
		if matchedExp[i] {
			continue
		}
		for j, act := range actual {
			if matchedAct[j] {
				continue
			}
			if strings.EqualFold(exp.Type, act.Type) && c.effectsCompatible(exp, act) {
				matchedExp[i] = true
				matchedAct[j] = true
				diffs := c.compareDetailedEffect(exp, act, i)
				mismatches = append(mismatches, diffs...)
				break
			}
		}
	}

	// Pass 3: Report missing effects (in expected, but not produced by actual)
	for i, exp := range expected {
		if !matchedExp[i] {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     fmt.Sprintf("effects[%d]", i),
				Expected: describeEffect(exp),
				Actual:   "<missing>",
				Message:  fmt.Sprintf("missing expected side effect: %s", describeEffect(exp)),
			})
		}
	}

	// Pass 4: Report extra effects (produced by actual, but unexpected)
	for j, act := range actual {
		if !matchedAct[j] {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     fmt.Sprintf("effects[%d]", j),
				Expected: "<none>",
				Actual:   describeEffect(act),
				Message:  fmt.Sprintf("extra unexpected side effect: %s", describeEffect(act)),
			})
		}
	}

	// Deterministic ordering of mismatches (Rule 2)
	sort.Slice(mismatches, func(i, j int) bool {
		if mismatches[i].Path == mismatches[j].Path {
			return mismatches[i].Message < mismatches[j].Message
		}
		return mismatches[i].Path < mismatches[j].Path
	})

	return mismatches
}

func (c *Comparator) compareEffectHeaders(expHeaders, actHeaders map[string]string) []adapterkit.Mismatch {
	var mismatches []adapterkit.Mismatch

	normExp := normalizeHeaderMap(expHeaders)
	normAct := normalizeHeaderMap(actHeaders)

	for k, expV := range normExp {
		if volatileEffectHeaders[k] {
			continue
		}
		headerPath := "headers." + k
		if c.ruleEngine != nil && c.ruleEngine.IsWildcard(headerPath, normAct[k]) {
			continue
		}
		actV, ok := normAct[k]
		if !ok {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     headerPath,
				Expected: expV,
				Actual:   "<missing>",
				Message:  fmt.Sprintf("missing expected header %q", k),
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

	return mismatches
}

func (c *Comparator) effectsEquivalent(exp, act adapterkit.Effect) bool {
	if !strings.EqualFold(exp.Type, act.Type) {
		return false
	}

	switch strings.ToLower(exp.Type) {
	case "http":
		if !strings.EqualFold(exp.Method, act.Method) {
			return false
		}
		if !targetMatches(exp.Target, act.Target) {
			return false
		}
		if (exp.BodyHash != "" && act.BodyHash == "") || (exp.BodyHash == "" && act.BodyHash != "") {
			return false
		}
		if exp.BodyHash != "" && act.BodyHash != "" && exp.BodyHash != act.BodyHash {
			return false
		}
		if len(c.compareEffectHeaders(exp.Headers, act.Headers)) > 0 {
			return false
		}
		return true

	case "smtp":
		if !strings.EqualFold(strings.TrimSpace(exp.EnvelopeFrom), strings.TrimSpace(act.EnvelopeFrom)) {
			return false
		}
		if !equalStringSlices(exp.EnvelopeTo, act.EnvelopeTo) {
			return false
		}
		if strings.TrimSpace(exp.Subject) != strings.TrimSpace(act.Subject) {
			return false
		}
		if (exp.BodyHash != "" && act.BodyHash == "") || (exp.BodyHash == "" && act.BodyHash != "") {
			return false
		}
		if exp.BodyHash != "" && act.BodyHash != "" && exp.BodyHash != act.BodyHash {
			return false
		}
		return true

	default: // generic or others
		if exp.Target != act.Target {
			return false
		}
		if exp.BodyHash != "" && exp.BodyHash != act.BodyHash {
			return false
		}
		return true
	}
}

func (c *Comparator) effectsCompatible(exp, act adapterkit.Effect) bool {
	switch strings.ToLower(exp.Type) {
	case "http":
		return targetMatches(exp.Target, act.Target) || strings.EqualFold(exp.Method, act.Method)
	case "smtp":
		return strings.EqualFold(strings.TrimSpace(exp.EnvelopeFrom), strings.TrimSpace(act.EnvelopeFrom)) ||
			strings.TrimSpace(exp.Subject) == strings.TrimSpace(act.Subject)
	default:
		return exp.Target == act.Target
	}
}

func (c *Comparator) compareDetailedEffect(exp, act adapterkit.Effect, index int) []adapterkit.Mismatch {
	var mismatches []adapterkit.Mismatch

	prefix := fmt.Sprintf("effects[%d]", index)

	if !strings.EqualFold(exp.Type, act.Type) {
		mismatches = append(mismatches, adapterkit.Mismatch{
			Path:     prefix + ".type",
			Expected: exp.Type,
			Actual:   act.Type,
			Message:  fmt.Sprintf("effect type mismatch: expected %q, got %q", exp.Type, act.Type),
		})
		return mismatches
	}

	switch strings.ToLower(exp.Type) {
	case "http":
		if !strings.EqualFold(exp.Method, act.Method) {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".method",
				Expected: exp.Method,
				Actual:   act.Method,
				Message:  fmt.Sprintf("effect HTTP method mismatch: expected %s, got %s", exp.Method, act.Method),
			})
		}
		if !targetMatches(exp.Target, act.Target) {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".target",
				Expected: exp.Target,
				Actual:   act.Target,
				Message:  fmt.Sprintf("effect HTTP target mismatch: expected %s, got %s", exp.Target, act.Target),
			})
		}
		if exp.BodyHash != act.BodyHash {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".body_hash",
				Expected: exp.BodyHash,
				Actual:   act.BodyHash,
				Message:  fmt.Sprintf("effect HTTP body hash mismatch: expected %s, got %s", exp.BodyHash, act.BodyHash),
			})
		}
		for _, hm := range c.compareEffectHeaders(exp.Headers, act.Headers) {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + "." + hm.Path,
				Expected: hm.Expected,
				Actual:   hm.Actual,
				Message:  fmt.Sprintf("effect HTTP %s", hm.Message),
			})
		}

	case "smtp":
		if !strings.EqualFold(strings.TrimSpace(exp.EnvelopeFrom), strings.TrimSpace(act.EnvelopeFrom)) {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".envelope_from",
				Expected: exp.EnvelopeFrom,
				Actual:   act.EnvelopeFrom,
				Message:  fmt.Sprintf("SMTP envelope from mismatch: expected %s, got %s", exp.EnvelopeFrom, act.EnvelopeFrom),
			})
		}
		if !equalStringSlices(exp.EnvelopeTo, act.EnvelopeTo) {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".envelope_to",
				Expected: exp.EnvelopeTo,
				Actual:   act.EnvelopeTo,
				Message:  fmt.Sprintf("SMTP envelope to mismatch: expected %v, got %v", exp.EnvelopeTo, act.EnvelopeTo),
			})
		}
		if strings.TrimSpace(exp.Subject) != strings.TrimSpace(act.Subject) {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".subject",
				Expected: exp.Subject,
				Actual:   act.Subject,
				Message:  fmt.Sprintf("SMTP subject mismatch: expected %q, got %q", exp.Subject, act.Subject),
			})
		}
		if exp.BodyHash != act.BodyHash {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".body_hash",
				Expected: exp.BodyHash,
				Actual:   act.BodyHash,
				Message:  fmt.Sprintf("SMTP body hash mismatch: expected %s, got %s", exp.BodyHash, act.BodyHash),
			})
		}

	default:
		if exp.Target != act.Target {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".target",
				Expected: exp.Target,
				Actual:   act.Target,
				Message:  fmt.Sprintf("effect target mismatch: expected %s, got %s", exp.Target, act.Target),
			})
		}
		if exp.BodyHash != act.BodyHash {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     prefix + ".body_hash",
				Expected: exp.BodyHash,
				Actual:   act.BodyHash,
				Message:  fmt.Sprintf("effect body hash mismatch: expected %s, got %s", exp.BodyHash, act.BodyHash),
			})
		}
	}

	if len(mismatches) == 0 {
		mismatches = append(mismatches, adapterkit.Mismatch{
			Path:     prefix,
			Expected: describeEffect(exp),
			Actual:   describeEffect(act),
			Message:  fmt.Sprintf("effect discrepancy: expected %s, got %s", describeEffect(exp), describeEffect(act)),
		})
	}

	return mismatches
}

func describeEffect(eff adapterkit.Effect) string {
	switch strings.ToLower(eff.Type) {
	case "http":
		return fmt.Sprintf("http %s %s (body_hash=%s)", eff.Method, eff.Target, eff.BodyHash)
	case "smtp":
		return fmt.Sprintf("smtp from=%s to=%v subject=%q (body_hash=%s)", eff.EnvelopeFrom, eff.EnvelopeTo, eff.Subject, eff.BodyHash)
	default:
		return fmt.Sprintf("%s target=%s (body_hash=%s)", eff.Type, eff.Target, eff.BodyHash)
	}
}

func targetMatches(expTarget, actTarget string) bool {
	if expTarget == actTarget {
		return true
	}
	if expTarget == "" || actTarget == "" {
		return false
	}

	uExp, errExp := url.Parse(expTarget)
	uAct, errAct := url.Parse(actTarget)

	if errExp == nil && errAct == nil {
		expPath := uExp.Path
		if expPath == "" {
			expPath = "/"
		}
		actPath := uAct.Path
		if actPath == "" {
			actPath = "/"
		}

		if expPath == actPath && uExp.RawQuery == uAct.RawQuery {
			if isLoopbackOrLocal(uExp.Host) && isLoopbackOrLocal(uAct.Host) {
				return true
			}
			if uExp.Host == uAct.Host {
				return true
			}
		}
	}

	if strings.HasPrefix(expTarget, "/") && errAct == nil && isLoopbackOrLocal(uAct.Host) {
		return expTarget == uAct.Path
	}
	if strings.HasPrefix(actTarget, "/") && errExp == nil && isLoopbackOrLocal(uExp.Host) {
		return actTarget == uExp.Path
	}

	return false
}

func isLoopbackOrLocal(host string) bool {
	if host == "" {
		return true
	}
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	cpA := make([]string, len(a))
	cpB := make([]string, len(b))
	for i, v := range a {
		cpA[i] = strings.ToLower(strings.TrimSpace(v))
	}
	for i, v := range b {
		cpB[i] = strings.ToLower(strings.TrimSpace(v))
	}
	sort.Strings(cpA)
	sort.Strings(cpB)
	for i := range cpA {
		if cpA[i] != cpB[i] {
			return false
		}
	}
	return true
}
