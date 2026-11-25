package comparator

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// RuleEngine evaluates wildcards and normalizers against field paths.
type RuleEngine struct {
	wildcards      []adapterkit.WildcardRule
	normalizers    []adapterkit.NormalizerRule
	fieldSemantics []adapterkit.FieldSemantic
}

// NewRuleEngine creates a new RuleEngine from rules.
func NewRuleEngine(rules *adapterkit.Rules) *RuleEngine {
	re := &RuleEngine{}
	if rules != nil {
		re.wildcards = rules.Wildcards
		re.normalizers = rules.Normalizers
		re.fieldSemantics = rules.FieldSemantics
	}
	return re
}

// IsWildcard checks whether the given path matches any configured wildcard rule.
func (re *RuleEngine) IsWildcard(currentPath string, value any) bool {
	normPath := normalizePath(currentPath)
	for _, w := range re.wildcards {
		if pathMatches(w.Path, normPath) {
			switch w.Type {
			case "regex":
				if w.Pattern != "" {
					if strVal, ok := value.(string); ok {
						if rePat, err := regexp.Compile(w.Pattern); err == nil && rePat.MatchString(strVal) {
							return true
						}
					}
				}
				return false
			default:
				// Any volatility detection type (uuid, timestamp, request_id, dynamic, volatile, random) matches unconditionally
				return true
			}
		}
	}

	// Also check volatile field semantics declared by adapters
	for _, fs := range re.fieldSemantics {
		if fs.Semantic == adapterkit.SemanticVolatile && pathMatches(fs.Path, normPath) {
			return true
		}
	}

	return false
}

// ShouldSortArray checks whether array elements at the given path should be sorted before comparison.
func (re *RuleEngine) ShouldSortArray(currentPath string) bool {
	normPath := normalizePath(currentPath)
	for _, n := range re.normalizers {
		if n.Type == "sort_array" && pathMatches(n.Path, normPath) {
			return true
		}
	}
	// If marked ordered explicitly in fieldSemantics, do NOT sort
	for _, fs := range re.fieldSemantics {
		if fs.Semantic == adapterkit.SemanticOrdered && pathMatches(fs.Path, normPath) {
			return false
		}
	}
	return false
}

// GetFloatTolerance returns the numeric tolerance configured for the path, if any.
func (re *RuleEngine) GetFloatTolerance(currentPath string) (float64, bool) {
	normPath := normalizePath(currentPath)
	for _, n := range re.normalizers {
		if n.Type == "float_tolerance" && pathMatches(n.Path, normPath) {
			if deltaVal, ok := n.Params["delta"]; ok {
				switch v := deltaVal.(type) {
				case float64:
					return v, true
				case int:
					return float64(v), true
				case string:
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						return f, true
					}
				}
			}
			return 1e-6, true
		}
	}
	return 0, false
}

// NormalizeString applies string transformations (trim_whitespace, lower_case, date_format).
func (re *RuleEngine) NormalizeString(currentPath string, input string) string {
	normPath := normalizePath(currentPath)
	res := input
	for _, n := range re.normalizers {
		if pathMatches(n.Path, normPath) {
			switch n.Type {
			case "trim_whitespace":
				res = strings.TrimSpace(res)
			case "lower_case":
				res = strings.ToLower(res)
			case "date_format":
				// Normalize RFC3339 variants to standard RFC3339 format
				if t, err := time.Parse(time.RFC3339Nano, res); err == nil {
					res = t.UTC().Format(time.RFC3339)
				}
			}
		}
	}
	return res
}

func normalizePath(p string) string {
	p = strings.TrimPrefix(p, "$.")
	p = strings.TrimPrefix(p, ".")
	p = strings.TrimPrefix(p, "output.body.")
	p = strings.TrimPrefix(p, "input.body.")
	p = strings.TrimPrefix(p, "body.")
	return strings.ToLower(p)
}

func pathMatches(pattern, target string) bool {
	pattern = normalizePath(pattern)
	target = normalizePath(target)
	if pattern == target {
		return true
	}
	matched, err := filepath.Match(pattern, target)
	if err == nil && matched {
		return true
	}
	return false
}
