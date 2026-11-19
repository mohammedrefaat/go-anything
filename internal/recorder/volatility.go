package recorder

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

var (
	regexUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	regexISO8601 = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?$`)

	// RFC 1123 date format: "Thu, 19 Nov 2026 15:00:00 GMT"
	regexRFC1123Date = regexp.MustCompile(`^[A-Za-z]{3}, \d{2} [A-Za-z]{3} \d{4} \d{2}:\d{2}:\d{2} GMT$`)

	regexRequestID = regexp.MustCompile(`(?i)^(?:req|request|trace|span|corr|tx)[-_][0-9a-zA-Z-_]+$`)
)

// DetectVolatileFields compares the outputs of two executions with identical inputs,
// identifying values that differ across runs (e.g. timestamps, UUIDs, monotonic IDs).
func DetectVolatileFields(out1, out2 *adapterkit.CaseOutput) []adapterkit.WildcardRule {
	var rules []adapterkit.WildcardRule

	if out1 == nil || out2 == nil {
		return rules
	}

	// 1. Diff Headers
	for k, v1 := range out1.Headers {
		v2, exists := out2.Headers[k]
		if !exists || v1 != v2 || isAlwaysVolatileHeader(k) {
			rule := classifyVolatileField("headers."+k, v1, v2)
			rules = append(rules, rule)
		}
	}

	// 2. Diff Body
	if out1.Body != nil && out2.Body != nil {
		body1 := out1.Body
		body2 := out2.Body

		// If bodies are JSON strings, parse them
		if str1, ok := body1.(string); ok {
			var parsed1, parsed2 any
			if str2, ok2 := body2.(string); ok2 {
				if json.Unmarshal([]byte(str1), &parsed1) == nil && json.Unmarshal([]byte(str2), &parsed2) == nil {
					body1 = parsed1
					body2 = parsed2
				}
			}
		}

		diffJSONValues("body", body1, body2, &rules)
	}

	// 3. Diff Stdout
	if out1.Stdout != out2.Stdout {
		// Check if stdout is JSON
		var parsed1, parsed2 any
		if json.Unmarshal([]byte(out1.Stdout), &parsed1) == nil && json.Unmarshal([]byte(out2.Stdout), &parsed2) == nil {
			diffJSONValues("stdout", parsed1, parsed2, &rules)
		} else {
			rule := classifyVolatileField("stdout", out1.Stdout, out2.Stdout)
			rules = append(rules, rule)
		}
	}

	return deduplicateRules(rules)
}

// isAlwaysVolatileHeader reports whether a response header is inherently
// run-dependent and must be wildcarded even when two recording runs happen to
// observe the same value (for example, both runs landing in the same second).
func isAlwaysVolatileHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "date", "age", "expires", "x-request-id", "x-trace-id", "traceparent":
		return true
	default:
		return false
	}
}

func diffJSONValues(path string, val1, val2 any, rules *[]adapterkit.WildcardRule) {
	if val1 == nil && val2 == nil {
		return
	}
	if val1 == nil || val2 == nil {
		*rules = append(*rules, classifyVolatileField(path, val1, val2))
		return
	}

	map1, isMap1 := val1.(map[string]any)
	map2, isMap2 := val2.(map[string]any)
	if isMap1 && isMap2 {
		allKeys := make(map[string]bool)
		for k := range map1 {
			allKeys[k] = true
		}
		for k := range map2 {
			allKeys[k] = true
		}

		for k := range allKeys {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			v1 := map1[k]
			v2 := map2[k]
			diffJSONValues(childPath, v1, v2, rules)
		}
		return
	}

	slice1, isSlice1 := val1.([]any)
	slice2, isSlice2 := val2.([]any)
	if isSlice1 && isSlice2 {
		maxLen := len(slice1)
		if len(slice2) > maxLen {
			maxLen = len(slice2)
		}
		for i := 0; i < maxLen; i++ {
			elemPath := fmt.Sprintf("%s[%d]", path, i)
			var v1, v2 any
			if i < len(slice1) {
				v1 = slice1[i]
			}
			if i < len(slice2) {
				v2 = slice2[i]
			}
			diffJSONValues(elemPath, v1, v2, rules)
		}
		return
	}

	// Primitive comparison
	str1 := fmt.Sprintf("%v", val1)
	str2 := fmt.Sprintf("%v", val2)
	if str1 != str2 {
		*rules = append(*rules, classifyVolatileField(path, val1, val2))
	}
}

func classifyVolatileField(path string, val1, val2 any) adapterkit.WildcardRule {
	s1 := fmt.Sprintf("%v", val1)
	s2 := fmt.Sprintf("%v", val2)

	// Check UUID
	if regexUUID.MatchString(s1) || regexUUID.MatchString(s2) {
		return adapterkit.WildcardRule{
			Path:        path,
			Type:        "uuid",
			Pattern:     "[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}",
			Description: "auto-detected UUID field from dual-run diff",
		}
	}

	// Check ISO8601 Timestamp
	if regexISO8601.MatchString(s1) || regexISO8601.MatchString(s2) {
		return adapterkit.WildcardRule{
			Path:        path,
			Type:        "timestamp",
			Pattern:     "iso8601",
			Description: "auto-detected ISO8601 timestamp from dual-run diff",
		}
	}

	// Check Epoch Timestamp
	if isEpochTimestamp(val1) || isEpochTimestamp(val2) {
		return adapterkit.WildcardRule{
			Path:        path,
			Type:        "timestamp",
			Pattern:     "epoch",
			Description: "auto-detected epoch timestamp from dual-run diff",
		}
	}

	// Check RFC 1123 Date (e.g., "Thu, 19 Nov 2026 15:00:00 GMT")
	if regexRFC1123Date.MatchString(s1) || regexRFC1123Date.MatchString(s2) {
		return adapterkit.WildcardRule{
			Path:        path,
			Type:        "timestamp",
			Pattern:     "rfc1123",
			Description: "auto-detected RFC 1123 date from dual-run diff",
		}
	}

	// Check Request / Trace ID
	if regexRequestID.MatchString(s1) || regexRequestID.MatchString(s2) {
		return adapterkit.WildcardRule{
			Path:        path,
			Type:        "request_id",
			Pattern:     "identifier",
			Description: "auto-detected request/trace identifier from dual-run diff",
		}
	}

	return adapterkit.WildcardRule{
		Path:        path,
		Type:        "dynamic",
		Description: "auto-detected volatile field from dual-run diff",
	}
}

func isEpochTimestamp(val any) bool {
	switch v := val.(type) {
	case int:
		return v >= 1000000000 && v <= 2500000000
	case int64:
		return (v >= 1000000000 && v <= 2500000000) || (v >= 1000000000000 && v <= 2500000000000)
	case float64:
		return (v >= 1000000000 && v <= 2500000000) || (v >= 1000000000000 && v <= 2500000000000)
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return (n >= 1000000000 && n <= 2500000000) || (n >= 1000000000000 && n <= 2500000000000)
		}
	}
	return false
}

func deduplicateRules(rules []adapterkit.WildcardRule) []adapterkit.WildcardRule {
	seen := make(map[string]bool)
	var deduped []adapterkit.WildcardRule
	for _, r := range rules {
		if !seen[r.Path] {
			seen[r.Path] = true
			deduped = append(deduped, r)
		}
	}
	return deduped
}

// ApplyWildcards applies wildcard masking to a parsed JSON value or string,
// replacing volatile values with normalized wildcard placeholders.
func ApplyWildcards(val any, wildcards []adapterkit.WildcardRule) any {
	if len(wildcards) == 0 || val == nil {
		return val
	}

	wildcardMap := make(map[string]string)
	for _, w := range wildcards {
		wildcardMap[w.Path] = w.Type
	}

	return maskValue(val, "", wildcardMap)
}

func maskValue(val any, path string, wildcards map[string]string) any {
	if maskType, ok := wildcards[path]; ok {
		return fmt.Sprintf("*[wildcard:%s]*", maskType)
	}
	if maskType, ok := wildcards["body."+path]; ok {
		return fmt.Sprintf("*[wildcard:%s]*", maskType)
	}
	if strings.HasPrefix(path, "body.") {
		if maskType, ok := wildcards[strings.TrimPrefix(path, "body.")]; ok {
			return fmt.Sprintf("*[wildcard:%s]*", maskType)
		}
	}

	switch v := val.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, child := range v {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			result[k] = maskValue(child, childPath, wildcards)
		}
		return result

	case []any:
		result := make([]any, len(v))
		for i, elem := range v {
			elemPath := fmt.Sprintf("%s[%d]", path, i)
			result[i] = maskValue(elem, elemPath, wildcards)
		}
		return result

	default:
		return val
	}
}
