package schemas_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileSchema(t *testing.T, schemaFilename string) *jsonschema.Schema {
	t.Helper()
	schemaPath := filepath.Join("v1alpha1", schemaFilename)
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020

	f, err := os.Open(schemaPath)
	require.NoError(t, err, "failed to open schema file %s", schemaPath)
	defer f.Close()

	err = compiler.AddResource(schemaFilename, f)
	require.NoError(t, err, "failed to add resource %s", schemaFilename)

	schema, err := compiler.Compile(schemaFilename)
	require.NoError(t, err, "failed to compile schema %s", schemaFilename)

	return schema
}

func validateJSON(t *testing.T, schema *jsonschema.Schema, jsonPayload string) error {
	t.Helper()
	var val any
	err := json.Unmarshal([]byte(jsonPayload), &val)
	require.NoError(t, err, "invalid test JSON input: %s", jsonPayload)
	return schema.Validate(val)
}

func TestManifestSchema(t *testing.T) {
	schema := compileSchema(t, "manifest.schema.json")

	tests := []struct {
		name        string
		payload     string
		expectError bool
	}{
		{
			name: "valid manifest",
			payload: `{
				"schema_version": "v1alpha1",
				"project_name": "example-app",
				"source_language": "python",
				"target_language": "go",
				"units": ["auth", "billing"],
				"waves": [
					{"wave": 0, "unit_ids": ["auth"]},
					{"wave": 1, "unit_ids": ["billing"]}
				]
			}`,
			expectError: false,
		},
		{
			name: "missing schema_version",
			payload: `{
				"units": ["auth"],
				"waves": [{"wave": 0, "unit_ids": ["auth"]}]
			}`,
			expectError: true,
		},
		{
			name: "invalid schema_version",
			payload: `{
				"schema_version": "v2",
				"units": ["auth"],
				"waves": [{"wave": 0, "unit_ids": ["auth"]}]
			}`,
			expectError: true,
		},
		{
			name: "missing units",
			payload: `{
				"schema_version": "v1alpha1",
				"waves": [{"wave": 0, "unit_ids": ["auth"]}]
			}`,
			expectError: true,
		},
		{
			name: "missing waves",
			payload: `{
				"schema_version": "v1alpha1",
				"units": ["auth"]
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSON(t, schema, tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("adapterkit struct roundtrip validation", func(t *testing.T) {
		m := adapterkit.Manifest{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			ProjectName:    "my-project",
			SourceLanguage: "typescript",
			TargetLanguage: "go",
			Units:          []string{"api", "db"},
			Waves: []adapterkit.Wave{
				{Wave: 0, UnitIDs: []string{"db"}},
				{Wave: 1, UnitIDs: []string{"api"}},
			},
		}
		data, err := json.Marshal(m)
		require.NoError(t, err)
		assert.NoError(t, validateJSON(t, schema, string(data)))
	})
}

func TestUnitsSchema(t *testing.T) {
	schema := compileSchema(t, "units.schema.json")

	tests := []struct {
		name        string
		payload     string
		expectError bool
	}{
		{
			name: "valid unit",
			payload: `{
				"schema_version": "v1alpha1",
				"id": "auth",
				"name": "Auth Unit",
				"wave": 0,
				"state": "Planned",
				"evidence_profile": "replay"
			}`,
			expectError: false,
		},
		{
			name: "missing schema_version",
			payload: `{
				"id": "auth",
				"name": "Auth Unit",
				"state": "Planned",
				"evidence_profile": "replay"
			}`,
			expectError: true,
		},
		{
			name: "invalid state",
			payload: `{
				"schema_version": "v1alpha1",
				"id": "auth",
				"name": "Auth Unit",
				"state": "UnknownState",
				"evidence_profile": "replay"
			}`,
			expectError: true,
		},
		{
			name: "invalid evidence profile",
			payload: `{
				"schema_version": "v1alpha1",
				"id": "auth",
				"name": "Auth Unit",
				"state": "Ready",
				"evidence_profile": "custom"
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSON(t, schema, tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("adapterkit struct roundtrip validation", func(t *testing.T) {
		u := adapterkit.Unit{
			SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
			ID:              "auth",
			Name:            "Authentication",
			State:           adapterkit.StateVerified,
			EvidenceProfile: adapterkit.ProfileFull,
			FallbackCount:   0,
		}
		data, err := json.Marshal(u)
		require.NoError(t, err)
		assert.NoError(t, validateJSON(t, schema, string(data)))
	})
}

func TestRulesSchema(t *testing.T) {
	schema := compileSchema(t, "rules.schema.json")

	tests := []struct {
		name        string
		payload     string
		expectError bool
	}{
		{
			name: "valid rules",
			payload: `{
				"schema_version": "v1alpha1",
				"wildcards": [
					{"path": "headers.x-request-id", "type": "uuid"}
				],
				"normalizers": [
					{"path": "body.timestamp", "type": "date_format"}
				],
				"field_semantics": [
					{"path": "headers.authorization", "semantic": "secret"}
				]
			}`,
			expectError: false,
		},
		{
			name: "missing schema_version",
			payload: `{
				"wildcards": [],
				"normalizers": []
			}`,
			expectError: true,
		},
		{
			name: "invalid wildcard type",
			payload: `{
				"schema_version": "v1alpha1",
				"wildcards": [
					{"path": "x", "type": "not_a_type"}
				],
				"normalizers": []
			}`,
			expectError: true,
		},
		{
			name: "invalid field semantic kind",
			payload: `{
				"schema_version": "v1alpha1",
				"wildcards": [],
				"normalizers": [],
				"field_semantics": [
					{"path": "token", "semantic": "unrecognized"}
				]
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSON(t, schema, tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("adapterkit struct roundtrip validation", func(t *testing.T) {
		r := adapterkit.Rules{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			Wildcards: []adapterkit.WildcardRule{
				{Path: "res.id", Type: "uuid"},
			},
			Normalizers: []adapterkit.NormalizerRule{
				{Path: "res.created", Type: "date_format"},
			},
			FieldSemantics: []adapterkit.FieldSemantic{
				{Path: "body.password", Semantic: adapterkit.SemanticSecret},
			},
		}
		data, err := json.Marshal(r)
		require.NoError(t, err)
		assert.NoError(t, validateJSON(t, schema, string(data)))
	})
}

func TestCasesSchema(t *testing.T) {
	schema := compileSchema(t, "cases.schema.json")

	tests := []struct {
		name        string
		payload     string
		expectError bool
	}{
		{
			name: "valid case",
			payload: `{
				"schema_version": "v1alpha1",
				"case_id": "c-001",
				"unit_id": "auth",
				"kind": "happy",
				"source_revision": "deadbeef",
				"runtime": "go1.22",
				"captured_at": "2026-11-19T10:00:00Z",
				"input": {"method": "GET", "path": "/health"},
				"expected_output": {"status_code": 200}
			}`,
			expectError: false,
		},
		{
			name: "invalid kind",
			payload: `{
				"schema_version": "v1alpha1",
				"case_id": "c-001",
				"unit_id": "auth",
				"kind": "unknown_kind",
				"source_revision": "deadbeef",
				"runtime": "go1.22",
				"captured_at": "2026-11-19T10:00:00Z",
				"input": {},
				"expected_output": {}
			}`,
			expectError: true,
		},
		{
			name: "missing required input",
			payload: `{
				"schema_version": "v1alpha1",
				"case_id": "c-001",
				"unit_id": "auth",
				"kind": "trap",
				"source_revision": "deadbeef",
				"runtime": "go1.22",
				"captured_at": "2026-11-19T10:00:00Z",
				"expected_output": {}
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSON(t, schema, tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("adapterkit struct roundtrip validation", func(t *testing.T) {
		c := adapterkit.Case{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "c-100",
			UnitID:         "checkout",
			Kind:           adapterkit.KindTrap,
			SourceRevision: "abcdef123456",
			Runtime:        "python3.11",
			CapturedAt:     "2026-11-19T10:00:00Z",
			TrapType:       "empty_collection",
			Input: adapterkit.CaseInput{
				Method: "POST",
				Path:   "/items",
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: 200,
			},
		}
		data, err := json.Marshal(c)
		require.NoError(t, err)
		assert.NoError(t, validateJSON(t, schema, string(data)))
	})
}

func TestReportsSchema(t *testing.T) {
	schema := compileSchema(t, "reports.schema.json")

	tests := []struct {
		name        string
		payload     string
		expectError bool
	}{
		{
			name: "valid report",
			payload: `{
				"schema_version": "v1alpha1",
				"report_id": "rep-001",
				"unit_id": "auth",
				"report_type": "diff",
				"status": "pass",
				"created_at": "2026-11-19T10:00:00Z"
			}`,
			expectError: false,
		},
		{
			name: "invalid report_type",
			payload: `{
				"schema_version": "v1alpha1",
				"report_id": "rep-001",
				"unit_id": "auth",
				"report_type": "arbitrary",
				"status": "pass",
				"created_at": "2026-11-19T10:00:00Z"
			}`,
			expectError: true,
		},
		{
			name: "invalid status",
			payload: `{
				"schema_version": "v1alpha1",
				"report_id": "rep-001",
				"unit_id": "auth",
				"report_type": "diff",
				"status": "pending",
				"created_at": "2026-11-19T10:00:00Z"
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSON(t, schema, tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("adapterkit struct roundtrip validation", func(t *testing.T) {
		r := adapterkit.Report{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			ReportID:      "rep-diff-1",
			UnitID:        "auth",
			ReportType:    adapterkit.ReportTypeDiff,
			Status:        adapterkit.StatusPass,
			CreatedAt:     "2026-11-19T10:00:00Z",
		}
		data, err := json.Marshal(r)
		require.NoError(t, err)
		assert.NoError(t, validateJSON(t, schema, string(data)))
	})
}

func TestEvidenceSchema(t *testing.T) {
	schema := compileSchema(t, "evidence.schema.json")

	tests := []struct {
		name        string
		payload     string
		expectError bool
	}{
		{
			name: "valid evidence",
			payload: `{
				"schema_version": "v1alpha1",
				"evidence_id": "evi-001",
				"unit_id": "auth",
				"commit_sha": "0123456789abcdef0123456789abcdef01234567",
				"profile": "replay",
				"verified_at": "2026-11-19T10:00:00Z",
				"verified_by": "coordinator",
				"checks": [
					{"name": "replay", "status": "pass"}
				],
				"mismatches_count": 0
			}`,
			expectError: false,
		},
		{
			name: "invalid commit_sha pattern",
			payload: `{
				"schema_version": "v1alpha1",
				"evidence_id": "evi-001",
				"unit_id": "auth",
				"commit_sha": "short-sha",
				"profile": "replay",
				"verified_at": "2026-11-19T10:00:00Z",
				"verified_by": "coordinator",
				"checks": [],
				"mismatches_count": 0
			}`,
			expectError: true,
		},
		{
			name: "negative mismatches_count",
			payload: `{
				"schema_version": "v1alpha1",
				"evidence_id": "evi-001",
				"unit_id": "auth",
				"commit_sha": "0123456789abcdef0123456789abcdef01234567",
				"profile": "replay",
				"verified_at": "2026-11-19T10:00:00Z",
				"verified_by": "coordinator",
				"checks": [],
				"mismatches_count": -1
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSON(t, schema, tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("adapterkit struct roundtrip validation", func(t *testing.T) {
		ev := adapterkit.Evidence{
			SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
			EvidenceID:      "evi-123",
			UnitID:          "auth",
			CommitSHA:       "abcdef0123456789abcdef0123456789abcdef01",
			Profile:         adapterkit.ProfileFull,
			VerifiedAt:      "2026-11-19T10:00:00Z",
			VerifiedBy:      "verifier-agent",
			Checks:          []adapterkit.GateCheck{{Name: "race", Status: adapterkit.StatusPass}},
			MismatchesCount: 0,
		}
		data, err := json.Marshal(ev)
		require.NoError(t, err)
		assert.NoError(t, validateJSON(t, schema, string(data)))
	})
}
