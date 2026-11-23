package schemas_test

import (
	"testing"

	"github.com/mohammedrefat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateV2_AnalyzeOutput(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid analyze output with string units and edges array",
			schemaName: "analyze.output.schema.json",
			payload: `{
				"schema_version": "v2",
				"project": "example.com/demo",
				"source_language": "go",
				"total_units": 2,
				"max_wave": 1,
				"units": ["unit_auth", "unit_billing"],
				"waves": [
					{
						"wave": 0,
						"unit_ids": ["unit_auth"]
					},
					{
						"wave": 1,
						"unit_ids": ["unit_billing"]
					}
				],
				"cycles": [],
				"edges": [
					{
						"from": "unit_billing",
						"to": "unit_auth"
					}
				]
			}`,
			expectError: false,
		},
		{
			name:       "valid analyze output with unit objects and graph adjacency map",
			schemaName: "analyze.output",
			payload: `{
				"project": "my-project",
				"source_language": "python",
				"units": [
					{
						"id": "auth",
						"name": "Auth Service",
						"package_path": "services/auth",
						"dir": "/src/services/auth",
						"wave": 0,
						"dependencies": [],
						"risk": "low"
					}
				],
				"waves": [
					{
						"wave": 0,
						"units": ["auth"]
					}
				],
				"cycles": [["auth", "billing", "auth"]],
				"graph": {
					"billing": ["auth"],
					"auth": []
				}
			}`,
			expectError: false,
		},
		{
			name:       "missing project field",
			schemaName: "analyze.output.schema.json",
			payload: `{
				"source_language": "go",
				"units": ["auth"],
				"waves": [{"wave": 0, "unit_ids": ["auth"]}],
				"cycles": [],
				"edges": []
			}`,
			expectError: true,
		},
		{
			name:       "missing source_language field",
			schemaName: "analyze.output.schema.json",
			payload: `{
				"project": "my-project",
				"units": ["auth"],
				"waves": [{"wave": 0, "unit_ids": ["auth"]}],
				"cycles": [],
				"edges": []
			}`,
			expectError: true,
		},
		{
			name:       "missing both edges and graph",
			schemaName: "analyze.output.schema.json",
			payload: `{
				"project": "my-project",
				"source_language": "go",
				"units": ["auth"],
				"waves": [{"wave": 0, "unit_ids": ["auth"]}],
				"cycles": []
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schemas.ValidateV2(tt.schemaName, []byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateV2_DiffOutput(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid diff output with matched_cases and mismatched_cases",
			schemaName: "diff.output.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_auth",
				"target": "modern",
				"mode": "structural_json",
				"total_cases": 10,
				"matched_cases": 9,
				"mismatched_cases": 1,
				"passed": false,
				"duration_ms": 142.5,
				"mismatches": [
					{
						"case_id": "case_login_expired_token",
						"diffs": [
							{
								"path": "response.status_code",
								"expected": 401,
								"actual": 403,
								"message": "status code mismatch"
							}
						],
						"inputs": {
							"method": "POST",
							"path": "/login"
						},
						"outputs": {
							"expected": {"status": 401},
							"actual": {"status": 403}
						},
						"message": "one mismatch found"
					}
				]
			}`,
			expectError: false,
		},
		{
			name:       "valid diff output with matched and mismatched alias fields",
			schemaName: "diff.output",
			payload: `{
				"unit_id": "unit_billing",
				"target": "go",
				"total_cases": 5,
				"matched": 5,
				"mismatched": 0,
				"passed": true,
				"mismatch_list": []
			}`,
			expectError: false,
		},
		{
			name:       "missing target",
			schemaName: "diff.output.schema.json",
			payload: `{
				"unit_id": "unit_billing",
				"total_cases": 5,
				"matched_cases": 5,
				"mismatched_cases": 0
			}`,
			expectError: true,
		},
		{
			name:       "negative total_cases",
			schemaName: "diff.output.schema.json",
			payload: `{
				"unit_id": "unit_billing",
				"target": "go",
				"total_cases": -1,
				"matched_cases": 0,
				"mismatched_cases": 0
			}`,
			expectError: true,
		},
		{
			name:       "missing matched_cases and matched",
			schemaName: "diff.output.schema.json",
			payload: `{
				"unit_id": "unit_billing",
				"target": "go",
				"total_cases": 5
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schemas.ValidateV2(tt.schemaName, []byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateV2_GateOutput(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid gate output with scorecard string summary",
			schemaName: "gate.output.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_payments",
				"profile": "full",
				"passed": true,
				"commit_sha": "a1b2c3d4e5f67890123456789012345678901234",
				"duration_ms": 1250.0,
				"checks": [
					{
						"name": "build",
						"status": "pass",
						"duration_ms": 350.0
					},
					{
						"name": "vet",
						"status": "pass",
						"duration_ms": 120.0
					},
					{
						"name": "staticcheck",
						"status": "skipped",
						"issues": ["tool not installed in PATH"]
					}
				],
				"scorecard": "100% parity across 25 cases",
				"scorecard_path": ".go-anything/reports/scorecard.json",
				"evidence_saved": true
			}`,
			expectError: false,
		},
		{
			name:       "valid gate output with scorecard object",
			schemaName: "gate.output",
			payload: `{
				"unit_id": "unit_orders",
				"profile": "replay",
				"passed": false,
				"checks": [
					{
						"name": "replay",
						"status": "fail",
						"duration_ms": 500.0,
						"issues": ["case_001 failed: expected 200 got 500"]
					}
				],
				"scorecard": {
					"summary": "1 failed check",
					"total_units": 1,
					"verified_units": 0,
					"total_cases": 10,
					"total_mismatches": 1
				}
			}`,
			expectError: false,
		},
		{
			name:       "invalid check status",
			schemaName: "gate.output.schema.json",
			payload: `{
				"unit_id": "unit_orders",
				"profile": "full",
				"passed": false,
				"checks": [
					{
						"name": "build",
						"status": "unknown"
					}
				]
			}`,
			expectError: true,
		},
		{
			name:       "missing profile",
			schemaName: "gate.output.schema.json",
			payload: `{
				"unit_id": "unit_orders",
				"passed": true,
				"checks": []
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schemas.ValidateV2(tt.schemaName, []byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateV2_ShadowOutput(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid shadow start output",
			schemaName: "shadow.output.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_catalog",
				"action": "start",
				"active": true,
				"port": 8080,
				"sample_rate": 0.25,
				"requests_total": 1000,
				"requests_shadowed": 250,
				"overflow_dropped": 0,
				"mismatches_count": 0,
				"reports_path": ".go-anything/reports/shadow_mismatches.jsonl"
			}`,
			expectError: false,
		},
		{
			name:       "valid shadow status output with alias fields",
			schemaName: "shadow.output",
			payload: `{
				"unit_id": "unit_catalog",
				"action": "status",
				"active": false,
				"port": 0,
				"sample_rate": 0.0,
				"total_requests": 50,
				"shadowed": 0,
				"overflow_dropped": 0,
				"mismatches": 0
			}`,
			expectError: false,
		},
		{
			name:       "invalid action",
			schemaName: "shadow.output.schema.json",
			payload: `{
				"unit_id": "unit_catalog",
				"action": "restart",
				"active": true,
				"port": 8080,
				"sample_rate": 0.5,
				"requests_total": 0,
				"requests_shadowed": 0,
				"overflow_dropped": 0,
				"mismatches_count": 0
			}`,
			expectError: true,
		},
		{
			name:       "sample_rate out of range (> 1.0)",
			schemaName: "shadow.output.schema.json",
			payload: `{
				"unit_id": "unit_catalog",
				"action": "start",
				"active": true,
				"port": 8080,
				"sample_rate": 1.5,
				"requests_total": 0,
				"requests_shadowed": 0,
				"overflow_dropped": 0,
				"mismatches_count": 0
			}`,
			expectError: true,
		},
		{
			name:       "invalid port number (> 65535)",
			schemaName: "shadow.output.schema.json",
			payload: `{
				"unit_id": "unit_catalog",
				"action": "start",
				"active": true,
				"port": 70000,
				"sample_rate": 0.5,
				"requests_total": 0,
				"requests_shadowed": 0,
				"overflow_dropped": 0,
				"mismatches_count": 0
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schemas.ValidateV2(tt.schemaName, []byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateV2_CutoverOutput(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid cutover plan output",
			schemaName: "cutover.output.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_inventory",
				"action": "plan",
				"route_state": "modern",
				"kill_switch_active": false,
				"target_service": "inventory-v2",
				"fallback_count": 0,
				"warnings": []
			}`,
			expectError: false,
		},
		{
			name:       "valid cutover kill output with warnings and fallbacks alias",
			schemaName: "cutover.output",
			payload: `{
				"unit_id": "unit_inventory",
				"action": "kill",
				"route_state": "original",
				"kill_switch_active": true,
				"fallbacks": 12,
				"warnings": [
					"traffic routed back to original legacy implementation"
				]
			}`,
			expectError: false,
		},
		{
			name:       "invalid action",
			schemaName: "cutover.output.schema.json",
			payload: `{
				"unit_id": "unit_inventory",
				"action": "promote_now",
				"route_state": "modern",
				"kill_switch_active": false,
				"fallback_count": 0
			}`,
			expectError: true,
		},
		{
			name:       "missing kill_switch_active",
			schemaName: "cutover.output.schema.json",
			payload: `{
				"unit_id": "unit_inventory",
				"action": "status",
				"route_state": "modern",
				"fallback_count": 0
			}`,
			expectError: true,
		},
		{
			name:       "missing route_state",
			schemaName: "cutover.output.schema.json",
			payload: `{
				"unit_id": "unit_inventory",
				"action": "status",
				"kill_switch_active": false,
				"fallback_count": 0
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schemas.ValidateV2(tt.schemaName, []byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateV2_FuzzReport(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid fuzz report with zero divergences",
			schemaName: "fuzz.report.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_math",
				"duration_ms": 2500.5,
				"total_runs": 10000,
				"divergences_count": 0,
				"divergences": [],
				"promoted_count": 0,
				"promoted_cases": [],
				"corpus_path": ".go-anything/cases/unit_math.jsonl",
				"passed": true,
				"summary": "10000 fuzz runs completed with 0 divergences"
			}`,
			expectError: false,
		},
		{
			name:       "valid fuzz report with divergences and promoted cases (shorthand)",
			schemaName: "fuzz.report",
			payload: `{
				"unit_id": "unit_parser",
				"duration_ms": 5000.0,
				"total_runs": 50000,
				"divergences_count": 1,
				"divergences": [
					{
						"case_id": "fuzz_case_001",
						"input": {"payload": "invalid-utf8-\ufffd"},
						"legacy_output": {"error": "syntax error"},
						"modern_output": {"status": "ok"},
						"diff": "- error: syntax error\n+ status: ok"
					}
				],
				"promoted_count": 1,
				"promoted_cases": ["fuzz_case_001"],
				"corpus_path": ".go-anything/cases/unit_parser.jsonl",
				"passed": false
			}`,
			expectError: false,
		},
		{
			name:       "missing unit_id",
			schemaName: "fuzz.report.schema.json",
			payload: `{
				"duration_ms": 100.0,
				"total_runs": 100,
				"divergences_count": 0,
				"divergences": [],
				"promoted_count": 0,
				"promoted_cases": [],
				"corpus_path": ".go-anything/cases/unit_test.jsonl"
			}`,
			expectError: true,
		},
		{
			name:       "missing corpus_path",
			schemaName: "fuzz.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"duration_ms": 100.0,
				"total_runs": 100,
				"divergences_count": 0,
				"divergences": [],
				"promoted_count": 0,
				"promoted_cases": []
			}`,
			expectError: true,
		},
		{
			name:       "negative total_runs",
			schemaName: "fuzz.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"duration_ms": 100.0,
				"total_runs": -5,
				"divergences_count": 0,
				"divergences": [],
				"promoted_count": 0,
				"promoted_cases": [],
				"corpus_path": ".go-anything/cases/unit_test.jsonl"
			}`,
			expectError: true,
		},
		{
			name:       "divergence item missing case_id",
			schemaName: "fuzz.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"duration_ms": 100.0,
				"total_runs": 100,
				"divergences_count": 1,
				"divergences": [
					{
						"input": "test"
					}
				],
				"promoted_count": 0,
				"promoted_cases": [],
				"corpus_path": ".go-anything/cases/unit_test.jsonl"
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schemas.ValidateV2(tt.schemaName, []byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateV2_UnknownSchema(t *testing.T) {
	err := schemas.ValidateV2("does_not_exist.schema.json", []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in version v2")
}
