package adapterkit_test

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRule8_NoInternalImports verifies that pkg/adapterkit never imports internal/ packages.
func TestRule8_NoInternalImports(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ImportsOnly)
	require.NoError(t, err)

	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			// Skip test files when inspecting adapterkit package imports
			if strings.HasSuffix(fileName, "_test.go") {
				continue
			}
			for _, imp := range file.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)
				assert.False(
					t,
					strings.Contains(importPath, "internal"),
					"file %s imports forbidden internal package %s",
					filepath.Base(fileName),
					importPath,
				)
			}
		}
	}
}

// TestCapabilitiesValidation tests capability declaration checking.
func TestCapabilitiesValidation(t *testing.T) {
	tests := []struct {
		name        string
		caps        []adapterkit.Capability
		expectError bool
		checkCap    adapterkit.Capability
		hasCap      bool
	}{
		{
			name: "valid standard capabilities",
			caps: []adapterkit.Capability{
				adapterkit.CapabilityReadRepo,
				adapterkit.CapabilityRunOriginal,
			},
			expectError: false,
			checkCap:    adapterkit.CapabilityReadRepo,
			hasCap:      true,
		},
		{
			name: "all valid capabilities",
			caps: []adapterkit.Capability{
				adapterkit.CapabilityReadRepo,
				adapterkit.CapabilityRunOriginal,
				adapterkit.CapabilityNetwork,
			},
			expectError: false,
			checkCap:    adapterkit.CapabilityNetwork,
			hasCap:      true,
		},
		{
			name: "unknown capability",
			caps: []adapterkit.Capability{
				adapterkit.CapabilityReadRepo,
				adapterkit.Capability("admin_write"),
			},
			expectError: true,
			checkCap:    adapterkit.CapabilityReadRepo,
			hasCap:      true,
		},
		{
			name: "duplicate capability",
			caps: []adapterkit.Capability{
				adapterkit.CapabilityReadRepo,
				adapterkit.CapabilityReadRepo,
			},
			expectError: true,
			checkCap:    adapterkit.CapabilityReadRepo,
			hasCap:      true,
		},
		{
			name: "missing capability query",
			caps: []adapterkit.Capability{
				adapterkit.CapabilityReadRepo,
			},
			expectError: false,
			checkCap:    adapterkit.CapabilityNetwork,
			hasCap:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := adapterkit.ValidateCapabilities(tt.caps)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.hasCap, adapterkit.HasCapability(tt.caps, tt.checkCap))
		})
	}
}

// TestFieldSemanticsValidation tests field semantic declarations.
func TestFieldSemanticsValidation(t *testing.T) {
	tests := []struct {
		name        string
		semantics   []adapterkit.FieldSemantic
		expectError bool
	}{
		{
			name: "valid semantics",
			semantics: []adapterkit.FieldSemantic{
				{Path: "headers.authorization", Semantic: adapterkit.SemanticSecret, Description: "Bearer token"},
				{Path: "response.timestamp", Semantic: adapterkit.SemanticVolatile, Description: "Server response time"},
				{Path: "data.items", Semantic: adapterkit.SemanticOrdered, Description: "Preserves sequence order"},
			},
			expectError: false,
		},
		{
			name: "empty path rejected",
			semantics: []adapterkit.FieldSemantic{
				{Path: "", Semantic: adapterkit.SemanticSecret},
			},
			expectError: true,
		},
		{
			name: "invalid semantic kind rejected",
			semantics: []adapterkit.FieldSemantic{
				{Path: "field.name", Semantic: adapterkit.SemanticKind("invalid_semantic")},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := adapterkit.ValidateFieldSemantics(tt.semantics)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestTypesJSONRoundtrip tests serialization and deserialization of all contract types.
func TestTypesJSONRoundtrip(t *testing.T) {
	t.Run("Manifest", func(t *testing.T) {
		m := adapterkit.Manifest{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			ProjectName:    "demo-service",
			SourceLanguage: "python",
			TargetLanguage: "go",
			Units:          []string{"auth", "billing"},
			Waves: []adapterkit.Wave{
				{Wave: 0, UnitIDs: []string{"auth"}},
				{Wave: 1, UnitIDs: []string{"billing"}},
			},
		}

		data, err := json.Marshal(m)
		require.NoError(t, err)

		var parsed adapterkit.Manifest
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		assert.Equal(t, m, parsed)
	})

	t.Run("Unit", func(t *testing.T) {
		u := adapterkit.Unit{
			SchemaVersion:      adapterkit.SchemaVersionV1Alpha1,
			ID:                 "auth",
			Name:               "Authentication Service",
			PackagePath:        "app/services/auth",
			Wave:               0,
			State:              adapterkit.StateInProgress,
			Dependencies:       []string{"config"},
			Risk:               "high",
			EvidenceProfile:    adapterkit.ProfileReplay,
			UnreplayableFields: []string{"sig", "token_mac"},
			FallbackCount:      0,
			LastUpdated:        "2026-11-19T10:00:00Z",
		}

		data, err := json.Marshal(u)
		require.NoError(t, err)

		var parsed adapterkit.Unit
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		assert.Equal(t, u, parsed)
	})

	t.Run("Rules", func(t *testing.T) {
		r := adapterkit.Rules{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			Wildcards: []adapterkit.WildcardRule{
				{Path: "headers.x-request-id", Type: "request_id", Description: "UUID request tracer"},
			},
			Normalizers: []adapterkit.NormalizerRule{
				{Path: "created_at", Type: "date_format", Params: map[string]any{"format": "RFC3339"}},
			},
			FieldSemantics: []adapterkit.FieldSemantic{
				{Path: "password", Semantic: adapterkit.SemanticSecret},
			},
		}

		data, err := json.Marshal(r)
		require.NoError(t, err)

		var parsed adapterkit.Rules
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		assert.Equal(t, r.SchemaVersion, parsed.SchemaVersion)
		assert.Len(t, parsed.Wildcards, 1)
		assert.Len(t, parsed.Normalizers, 1)
	})

	t.Run("Case", func(t *testing.T) {
		c := adapterkit.Case{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "auth-login-happy-001",
			UnitID:         "auth",
			Kind:           adapterkit.KindHappy,
			SourceRevision: "a1b2c3d4e5f678901234567890abcdef12345678",
			Runtime:        "python3.11",
			CapturedAt:     "2026-11-19T10:00:00Z",
			Input: adapterkit.CaseInput{
				Method: "POST",
				Path:   "/login",
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: `{"username":"alice","password":"redacted-valid-pwd"}`,
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: 200,
				Body:       `{"status":"ok","user_id":"u_123"}`,
			},
		}

		data, err := json.Marshal(c)
		require.NoError(t, err)

		var parsed adapterkit.Case
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		assert.Equal(t, c.CaseID, parsed.CaseID)
		assert.Equal(t, c.ExpectedOutput.StatusCode, parsed.ExpectedOutput.StatusCode)
	})

	t.Run("Report", func(t *testing.T) {
		rep := adapterkit.Report{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			ReportID:      "rep-001",
			UnitID:        "auth",
			CaseID:        "auth-login-happy-001",
			ReportType:    adapterkit.ReportTypeDiff,
			Mode:          "structural_json",
			Status:        adapterkit.StatusFail,
			Mismatches: []adapterkit.Mismatch{
				{Path: "status", Expected: "ok", Actual: "error", Message: "status field mismatch"},
			},
			CreatedAt:  "2026-11-19T10:05:00Z",
			DurationMS: 14.2,
		}

		data, err := json.Marshal(rep)
		require.NoError(t, err)

		var parsed adapterkit.Report
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		assert.Equal(t, rep, parsed)
	})

	t.Run("Evidence", func(t *testing.T) {
		ev := adapterkit.Evidence{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			EvidenceID:    "evi-auth-001",
			UnitID:        "auth",
			CommitSHA:     "1234567890abcdef1234567890abcdef12345678",
			Profile:       adapterkit.ProfileReplay,
			VerifiedAt:    "2026-11-19T10:10:00Z",
			VerifiedBy:    "agent-verifier",
			Checks: []adapterkit.GateCheck{
				{Name: "replay", Status: adapterkit.StatusPass, DurationMS: 120.5},
			},
			MismatchesCount: 0,
		}

		data, err := json.Marshal(ev)
		require.NoError(t, err)

		var parsed adapterkit.Evidence
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		assert.Equal(t, ev, parsed)
	})
}

// TestV1ConstantsAndSchemas verifies that adapterkit exports frozen v1 schema constants and references.
func TestV1ConstantsAndSchemas(t *testing.T) {
	assert.Equal(t, "v1", adapterkit.Version)
	assert.Equal(t, "v1", adapterkit.SchemaVersionV1)
	assert.Equal(t, "v1alpha1", adapterkit.SchemaVersionV1Alpha1)
	assert.Equal(t, "v1", adapterkit.DefaultSchemaVersion)

	// Check stable v1 schema IDs
	assert.Contains(t, adapterkit.SchemaIDManifestV1, "/v1/manifest.schema.json")
	assert.Contains(t, adapterkit.SchemaIDUnitsV1, "/v1/units.schema.json")
	assert.Contains(t, adapterkit.SchemaIDRulesV1, "/v1/rules.schema.json")
	assert.Contains(t, adapterkit.SchemaIDCasesV1, "/v1/cases.schema.json")
	assert.Contains(t, adapterkit.SchemaIDReportsV1, "/v1/reports.schema.json")
	assert.Contains(t, adapterkit.SchemaIDEvidenceV1, "/v1/evidence.schema.json")

	// Check legacy v1alpha1 schema IDs
	assert.Contains(t, adapterkit.SchemaIDManifestV1Alpha1, "/v1alpha1/manifest.schema.json")
	assert.Contains(t, adapterkit.SchemaIDUnitsV1Alpha1, "/v1alpha1/units.schema.json")
	assert.Contains(t, adapterkit.SchemaIDRulesV1Alpha1, "/v1alpha1/rules.schema.json")
	assert.Contains(t, adapterkit.SchemaIDCasesV1Alpha1, "/v1alpha1/cases.schema.json")
	assert.Contains(t, adapterkit.SchemaIDReportsV1Alpha1, "/v1alpha1/reports.schema.json")
	assert.Contains(t, adapterkit.SchemaIDEvidenceV1Alpha1, "/v1alpha1/evidence.schema.json")
}
