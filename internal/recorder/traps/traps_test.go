package traps_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/recorder/traps"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findSchemaPath(t *testing.T) string {
	// Look up schemas/v1alpha1/cases.schema.json relative to repository root
	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		candidate := filepath.Join(dir, "schemas", "v1alpha1", "cases.schema.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("cases.schema.json not found in parent hierarchy")
	return ""
}

func TestTraps_AllCategoriesEmittedAndSchemaValid(t *testing.T) {
	schemaPath := findSchemaPath(t)
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	require.NoError(t, err, "failed to compile cases.schema.json")

	gen := traps.NewGenerator("commit-abc", "go1.22")
	unitID := "auth-service"

	allTraps := gen.GenerateAllTraps(unitID)
	require.NotEmpty(t, allTraps, "expected generated trap cases")

	categoryCounts := make(map[traps.TrapCategory]int)

	for _, tc := range allTraps {
		// 1. Mandatory metadata fields
		assert.Equal(t, adapterkit.SchemaVersionV1Alpha1, tc.SchemaVersion)
		assert.Equal(t, unitID, tc.UnitID)
		assert.Equal(t, adapterkit.KindTrap, tc.Kind)
		assert.NotEmpty(t, tc.CaseID)
		assert.NotEmpty(t, tc.TrapType)
		assert.NotEmpty(t, tc.CapturedAt)

		categoryCounts[traps.TrapCategory(tc.TrapType)]++

		// 2. Validate against JSON Schema (Exit Gate Requirement)
		data, err := json.Marshal(tc)
		require.NoError(t, err)

		var parsed any
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		err = schema.Validate(parsed)
		assert.NoError(t, err, "case %s failed schema validation: %v", tc.CaseID, err)
	}

	// 3. Verify all 7 categories are covered
	assert.Equal(t, 7, len(traps.AllTrapCategories), "expected exactly 7 semantic trap categories")
	for _, cat := range traps.AllTrapCategories {
		count := categoryCounts[cat]
		assert.Greater(t, count, 0, "category %s has no generated trap cases", cat)
	}
}

func TestTraps_SpecificCategoryPayloads(t *testing.T) {
	gen := traps.NewGenerator("head", "go1.22")
	unitID := "demo-unit"

	tests := []struct {
		category     traps.TrapCategory
		minCases     int
		verifyCaseFn func(t *testing.T, cases []*adapterkit.Case)
	}{
		{
			category: traps.TrapEmptyCollection,
			minCases: 2,
			verifyCaseFn: func(t *testing.T, cases []*adapterkit.Case) {
				hasEmpty := false
				hasNil := false
				for _, c := range cases {
					bodyMap, ok := c.Input.Body.(map[string]any)
					if ok {
						if val, exists := bodyMap["tags"]; exists {
							if val == nil {
								hasNil = true
							} else if sliceVal, ok := val.([]any); ok && len(sliceVal) == 0 {
								hasEmpty = true
							}
						}
					}
				}
				assert.True(t, hasEmpty, "should test empty slice []")
				assert.True(t, hasNil, "should test nil slice / null")
			},
		},
		{
			category: traps.TrapHTMLEscaping,
			minCases: 2,
			verifyCaseFn: func(t *testing.T, cases []*adapterkit.Case) {
				foundHTML := false
				for _, c := range cases {
					bodyMap, ok := c.Input.Body.(map[string]any)
					if ok {
						for _, v := range bodyMap {
							if s, ok := v.(string); ok && (strings.Contains(s, "<") || strings.Contains(s, "&")) {
								foundHTML = true
							}
						}
					}
				}
				assert.True(t, foundHTML, "should test HTML characters < and &")
			},
		},
		{
			category: traps.TrapMapKeyOrder,
			minCases: 1,
			verifyCaseFn: func(t *testing.T, cases []*adapterkit.Case) {
				bodyMap, ok := cases[0].Input.Body.(map[string]any)
				require.True(t, ok)
				assert.GreaterOrEqual(t, len(bodyMap), 3, "should test map with multiple keys")
			},
		},
		{
			category: traps.TrapNumberPrecision,
			minCases: 2,
			verifyCaseFn: func(t *testing.T, cases []*adapterkit.Case) {
				foundLargeInt := false
				for _, c := range cases {
					bodyMap, ok := c.Input.Body.(map[string]any)
					if ok {
						if _, exists := bodyMap["large_int"]; exists {
							foundLargeInt = true
						}
					}
				}
				assert.True(t, foundLargeInt, "should test large int > 2^53 - 1")
			},
		},
		{
			category: traps.TrapStringOffset,
			minCases: 2,
			verifyCaseFn: func(t *testing.T, cases []*adapterkit.Case) {
				foundEmoji := false
				foundCRLF := false
				for _, c := range cases {
					bodyMap, ok := c.Input.Body.(map[string]any)
					if ok {
						for _, v := range bodyMap {
							if s, ok := v.(string); ok {
								if strings.Contains(s, "🚀") || strings.Contains(s, "🌍") {
									foundEmoji = true
								}
								if strings.Contains(s, "\r\n") {
									foundCRLF = true
								}
							}
						}
					}
				}
				assert.True(t, foundEmoji, "should test multibyte/emoji offsets")
				assert.True(t, foundCRLF, "should test CRLF vs LF line endings")
			},
		},
		{
			category: traps.TrapTimeFormat,
			minCases: 2,
			verifyCaseFn: func(t *testing.T, cases []*adapterkit.Case) {
				foundSubsecond := false
				for _, c := range cases {
					bodyMap, ok := c.Input.Body.(map[string]any)
					if ok {
						for _, v := range bodyMap {
							if s, ok := v.(string); ok && strings.Contains(s, ".123456789") {
								foundSubsecond = true
							}
						}
					}
				}
				assert.True(t, foundSubsecond, "should test nanosecond/sub-second precision")
			},
		},
		{
			category: traps.TrapErrorPath,
			minCases: 2,
			verifyCaseFn: func(t *testing.T, cases []*adapterkit.Case) {
				foundNonZero := false
				for _, c := range cases {
					if c.ExpectedOutput.ExitCode != 0 || c.ExpectedOutput.StatusCode >= 400 {
						foundNonZero = true
					}
				}
				assert.True(t, foundNonZero, "should test non-zero exit code or error status code")
			},
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.category), func(t *testing.T) {
			cases, err := gen.GenerateCategoryTraps(unitID, tc.category)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, len(cases), tc.minCases)
			tc.verifyCaseFn(t, cases)
		})
	}
}

func TestTraps_UnknownCategory(t *testing.T) {
	gen := traps.NewGenerator("head", "go1.22")
	_, err := gen.GenerateCategoryTraps("unit", traps.TrapCategory("invalid_category"))
	assert.Error(t, err)
}
