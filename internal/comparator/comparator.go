package comparator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// Supported comparison modes.
const (
	ModeExact              = "exact"
	ModeStructuralJSON     = "structural_json"
	ModeNumericTolerance   = "numeric_tolerance"
	ModeErrorEquivalence   = "error_equivalence"
	ModeHTTPContract       = "http_contract"
	ModeFilesystemSnapshot = "filesystem_snapshot"
	ModeEffectMultiset     = "effect_multiset"
)

// ComparisonResult represents the outcome of comparing behavioral outputs.
type ComparisonResult struct {
	Mode       string                `json:"mode"`
	Passed     bool                  `json:"passed"`
	Mismatches []adapterkit.Mismatch `json:"mismatches"`
	Summary    string                `json:"summary"`
}

// Option configures the Comparator.
type Option func(*Comparator)

// WithRules applies dynamic wildcard and normalizer rules to the comparator.
func WithRules(rules *adapterkit.Rules) Option {
	return func(c *Comparator) {
		c.rules = rules
		c.ruleEngine = NewRuleEngine(rules)
	}
}

// WithNumericDelta sets the default delta for numeric tolerance comparisons.
func WithNumericDelta(delta float64) Option {
	return func(c *Comparator) {
		if delta > 0 {
			c.numericDelta = delta
		}
	}
}

// Comparator performs multi-mode structural diffing and comparison.
type Comparator struct {
	rules        *adapterkit.Rules
	ruleEngine   *RuleEngine
	numericDelta float64
}

// New returns a configured Comparator instance.
func New(opts ...Option) *Comparator {
	c := &Comparator{
		numericDelta: 1e-6,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.ruleEngine == nil {
		c.ruleEngine = NewRuleEngine(c.rules)
	}
	return c
}

// Compare evaluates differences between expected and actual according to mode.
func (c *Comparator) Compare(mode string, expected, actual any) (*ComparisonResult, error) {
	if mode == "" {
		mode = ModeStructuralJSON
	}

	var mismatches []adapterkit.Mismatch

	switch mode {
	case ModeExact:
		expBytes := toBytes(expected)
		actBytes := toBytes(actual)
		mismatches = c.compareExact(expBytes, actBytes)

	case ModeStructuralJSON:
		expBytes := toBytes(expected)
		actBytes := toBytes(actual)
		jc := NewJSONComparator(c.ruleEngine, c.numericDelta)
		var err error
		mismatches, err = jc.CompareJSON(expBytes, actBytes)
		if err != nil {
			return nil, fmt.Errorf("structural_json comparison failed: %w", err)
		}

	case ModeNumericTolerance:
		expStr := fmt.Sprintf("%v", expected)
		actStr := fmt.Sprintf("%v", actual)
		mismatches = c.compareNumericTolerance(expStr, actStr, c.numericDelta)

	case ModeFilesystemSnapshot:
		expMap, ok1 := expected.(map[string]string)
		actMap, ok2 := actual.(map[string]string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("filesystem_snapshot requires map[string]string inputs")
		}
		mismatches = c.compareFilesystemSnapshot(expMap, actMap)

	case ModeEffectMultiset:
		expEffects, err1 := toEffects(expected)
		actEffects, err2 := toEffects(actual)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("effect_multiset requires []adapterkit.Effect inputs: expErr=%v, actErr=%v", err1, err2)
		}
		mismatches = c.CompareEffectMultiset(expEffects, actEffects)

	default:
		return nil, fmt.Errorf("unsupported comparison mode: %q", mode)
	}

	// Deterministic ordering of mismatches (Rule 2)
	sort.Slice(mismatches, func(i, j int) bool {
		return mismatches[i].Path < mismatches[j].Path
	})

	passed := len(mismatches) == 0
	summary := "PASS: outputs are behaviorally equivalent"
	if !passed {
		summary = fmt.Sprintf("FAIL: %d mismatch(es) detected under mode %q", len(mismatches), mode)
	}

	return &ComparisonResult{
		Mode:       mode,
		Passed:     passed,
		Mismatches: mismatches,
		Summary:    summary,
	}, nil
}

// CompareCaseOutput evaluates full case output structures (HTTP or CLI execution).
func (c *Comparator) CompareCaseOutput(mode string, expected, actual *adapterkit.CaseOutput) (*ComparisonResult, error) {
	if mode == "" {
		mode = ModeHTTPContract
	}

	var mismatches []adapterkit.Mismatch

	switch mode {
	case ModeHTTPContract:
		mismatches = c.compareHTTPContract(expected, actual)

	case ModeErrorEquivalence:
		mismatches = c.compareErrorEquivalence(expected.ErrorMessage, actual.ErrorMessage, expected.ExitCode, actual.ExitCode)

	case ModeExact:
		// Exact comparison of stdout/body
		mismatches = append(mismatches, c.compareExact(toBytes(expected.Body), toBytes(actual.Body))...)
		if expected.Stdout != actual.Stdout {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     "stdout",
				Expected: expected.Stdout,
				Actual:   actual.Stdout,
				Message:  "stdout mismatch",
			})
		}
		if expected.ExitCode != actual.ExitCode {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     "exit_code",
				Expected: expected.ExitCode,
				Actual:   actual.ExitCode,
				Message:  fmt.Sprintf("exit code mismatch: %d != %d", expected.ExitCode, actual.ExitCode),
			})
		}

	case ModeStructuralJSON:
		jc := NewJSONComparator(c.ruleEngine, c.numericDelta)
		diffs, err := jc.CompareJSON(toBytes(expected.Body), toBytes(actual.Body))
		if err != nil {
			return nil, err
		}
		mismatches = append(mismatches, diffs...)
		if expected.StatusCode != actual.StatusCode {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:     "status_code",
				Expected: expected.StatusCode,
				Actual:   actual.StatusCode,
				Message:  fmt.Sprintf("status code mismatch: %d != %d", expected.StatusCode, actual.StatusCode),
			})
		}

	case ModeEffectMultiset:
		if expected != nil && actual != nil {
			mismatches = c.CompareEffectMultiset(expected.Effects, actual.Effects)
		} else if expected != nil || actual != nil {
			mismatches = append(mismatches, adapterkit.Mismatch{
				Path:    "effects",
				Message: "missing CaseOutput payload for effect comparison",
			})
		}

	default:
		return nil, fmt.Errorf("unsupported mode for CaseOutput: %q", mode)
	}

	// When not in ModeEffectMultiset, if either expected or actual produced side effects,
	// also validate effect equivalence to ensure no unexpected or missing side effects.
	if mode != ModeEffectMultiset && expected != nil && actual != nil && (len(expected.Effects) > 0 || len(actual.Effects) > 0) {
		mismatches = append(mismatches, c.CompareEffectMultiset(expected.Effects, actual.Effects)...)
	}

	// Deterministic sorting (Rule 2)
	sort.Slice(mismatches, func(i, j int) bool {
		return mismatches[i].Path < mismatches[j].Path
	})

	passed := len(mismatches) == 0
	summary := "PASS"
	if !passed {
		summary = fmt.Sprintf("FAIL: %d mismatch(es) detected", len(mismatches))
	}

	return &ComparisonResult{
		Mode:       mode,
		Passed:     passed,
		Mismatches: mismatches,
		Summary:    summary,
	}, nil
}

// BuildReport constructs an adapterkit.Report from the comparison result.
func (c *Comparator) BuildReport(unitID, caseID, mode string, result *ComparisonResult) *adapterkit.Report {
	status := adapterkit.StatusPass
	if !result.Passed {
		status = adapterkit.StatusFail
	}

	return &adapterkit.Report{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		ReportID:      fmt.Sprintf("rep-%s-%d", caseID, time.Now().UnixNano()),
		UnitID:        unitID,
		CaseID:        caseID,
		ReportType:    adapterkit.ReportTypeDiff,
		Mode:          mode,
		Status:        status,
		Mismatches:    result.Mismatches,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Summary:       result.Summary,
	}
}

// SaveMismatchCase saves a reproducible mismatch artifact to disk for debugging.
func (c *Comparator) SaveMismatchCase(targetDir, unitID, caseID string, input *adapterkit.CaseInput, exp, act *adapterkit.CaseOutput, mismatches []adapterkit.Mismatch) (string, error) {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", err
	}

	mismatchRecord := map[string]any{
		"schema_version": adapterkit.SchemaVersionV1Alpha1,
		"case_id":        caseID,
		"unit_id":        unitID,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"input":          input,
		"expected":       exp,
		"actual":         act,
		"mismatches":     mismatches,
	}

	data, err := json.MarshalIndent(mismatchRecord, "", "  ")
	if err != nil {
		return "", err
	}

	filename := fmt.Sprintf("mismatch_%s_%s.json", unitID, caseID)
	fullPath := filepath.Join(targetDir, filename)
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return "", err
	}

	return fullPath, nil
}

func toEffects(v any) ([]adapterkit.Effect, error) {
	if v == nil {
		return nil, nil
	}
	switch val := v.(type) {
	case []adapterkit.Effect:
		return val, nil
	case adapterkit.Effect:
		return []adapterkit.Effect{val}, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var effs []adapterkit.Effect
		if err := json.Unmarshal(data, &effs); err != nil {
			return nil, err
		}
		return effs, nil
	}
}
