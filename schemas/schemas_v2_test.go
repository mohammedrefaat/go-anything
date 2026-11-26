package schemas_test

import (
	"testing"

	"github.com/mohammedrefaat/go-anything/schemas"
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

func TestValidateV2_BenchReport(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid bench report with full schema name",
			schemaName: "bench.report.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_hasher",
				"original": {
					"p50_ns": 120000.0,
					"p95_ns": 150000.0,
					"p99_ns": 180000.0,
					"throughput_ops_sec": 8200.0,
					"mean_ns": 121951.0
				},
				"go": {
					"p50_ns": 30000.0,
					"p95_ns": 35000.0,
					"p99_ns": 42000.0,
					"throughput_ops_sec": 32000.0,
					"mean_ns": 31250.0,
					"allocs_per_op": 2,
					"bytes_per_op": 128
				},
				"speedup_ratio": 3.9,
				"iterations": 10000,
				"warmup": 1000,
				"environment": {
					"cpu_model": "AMD Ryzen 9 5950X",
					"os": "windows/amd64",
					"go_version": "go1.26",
					"original_runtime": "python 3.12",
					"cores": 16
				},
				"summary": "Go implementation is 3.9x faster with 128 B/op allocation",
				"timestamp": "2026-11-23T11:00:00Z"
			}`,
			expectError: false,
		},
		{
			name:       "valid bench report using shorthand schema name",
			schemaName: "bench.report",
			payload: `{
				"unit_id": "unit_crypto",
				"original": {
					"p50_ns": 50000.0,
					"p95_ns": 60000.0,
					"p99_ns": 70000.0,
					"throughput_ops_sec": 20000.0,
					"mean_ns": 50000.0
				},
				"go": {
					"p50_ns": 25000.0,
					"p95_ns": 30000.0,
					"p99_ns": 35000.0,
					"throughput_ops_sec": 40000.0,
					"mean_ns": 25000.0,
					"allocs_per_op": 0,
					"bytes_per_op": 0
				},
				"speedup_ratio": 2.0,
				"iterations": 5000,
				"warmup": 500,
				"environment": {
					"cpu_model": "Apple M2 Pro",
					"os": "darwin/arm64",
					"go_version": "go1.26",
					"original_runtime": "node 20",
					"cores": 12
				},
				"summary": "2.0x faster with zero allocations",
				"timestamp": "2026-11-23T11:15:00Z"
			}`,
			expectError: false,
		},
		{
			name:       "missing unit_id",
			schemaName: "bench.report.schema.json",
			payload: `{
				"original": {
					"p50_ns": 100.0, "p95_ns": 150.0, "p99_ns": 200.0,
					"throughput_ops_sec": 10000.0, "mean_ns": 100.0
				},
				"go": {
					"p50_ns": 50.0, "p95_ns": 75.0, "p99_ns": 100.0,
					"throughput_ops_sec": 20000.0, "mean_ns": 50.0,
					"allocs_per_op": 0, "bytes_per_op": 0
				},
				"speedup_ratio": 2.0,
				"iterations": 1000,
				"warmup": 100,
				"environment": {
					"cpu_model": "CPU", "os": "linux", "go_version": "go1.26",
					"original_runtime": "python", "cores": 4
				},
				"summary": "ok",
				"timestamp": "now"
			}`,
			expectError: true,
		},
		{
			name:       "missing allocs_per_op in go metrics",
			schemaName: "bench.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"original": {
					"p50_ns": 100.0, "p95_ns": 150.0, "p99_ns": 200.0,
					"throughput_ops_sec": 10000.0, "mean_ns": 100.0
				},
				"go": {
					"p50_ns": 50.0, "p95_ns": 75.0, "p99_ns": 100.0,
					"throughput_ops_sec": 20000.0, "mean_ns": 50.0,
					"bytes_per_op": 0
				},
				"speedup_ratio": 2.0,
				"iterations": 1000,
				"warmup": 100,
				"environment": {
					"cpu_model": "CPU", "os": "linux", "go_version": "go1.26",
					"original_runtime": "python", "cores": 4
				},
				"summary": "ok",
				"timestamp": "now"
			}`,
			expectError: true,
		},
		{
			name:       "missing environment",
			schemaName: "bench.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"original": {
					"p50_ns": 100.0, "p95_ns": 150.0, "p99_ns": 200.0,
					"throughput_ops_sec": 10000.0, "mean_ns": 100.0
				},
				"go": {
					"p50_ns": 50.0, "p95_ns": 75.0, "p99_ns": 100.0,
					"throughput_ops_sec": 20000.0, "mean_ns": 50.0,
					"allocs_per_op": 0, "bytes_per_op": 0
				},
				"speedup_ratio": 2.0,
				"iterations": 1000,
				"warmup": 100,
				"summary": "ok",
				"timestamp": "now"
			}`,
			expectError: true,
		},
		{
			name:       "negative iterations",
			schemaName: "bench.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"original": {
					"p50_ns": 100.0, "p95_ns": 150.0, "p99_ns": 200.0,
					"throughput_ops_sec": 10000.0, "mean_ns": 100.0
				},
				"go": {
					"p50_ns": 50.0, "p95_ns": 75.0, "p99_ns": 100.0,
					"throughput_ops_sec": 20000.0, "mean_ns": 50.0,
					"allocs_per_op": 0, "bytes_per_op": 0
				},
				"speedup_ratio": 2.0,
				"iterations": -10,
				"warmup": 100,
				"environment": {
					"cpu_model": "CPU", "os": "linux", "go_version": "go1.26",
					"original_runtime": "python", "cores": 4
				},
				"summary": "ok",
				"timestamp": "now"
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

func TestValidateV2_ProfileReport(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid profile report with full schema name",
			schemaName: "profile.report.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_math_py",
				"language": "python",
				"profiler": "cProfile",
				"total_duration_ms": 12500.0,
				"top": 5,
				"candidates": [
					{
						"rank": 1,
						"function_name": "compute_factors",
						"file": "math_service/factors.py",
						"line": 42,
						"self_percent": 68.5,
						"cumulative_percent": 72.0,
						"call_count": 50000,
						"is_pure_heuristic": true,
						"purity_reason": "no global mutations, no I/O calls identified in AST"
					}
				],
				"summary": "Top hotspot compute_factors consumes 68.5% self CPU time",
				"timestamp": "2026-11-23T11:20:00Z"
			}`,
			expectError: false,
		},
		{
			name:       "valid profile report using shorthand schema name",
			schemaName: "profile.report",
			payload: `{
				"unit_id": "unit_tokenizer_ts",
				"language": "typescript",
				"profiler": "v8_cpuprofile",
				"total_duration_ms": 8000.0,
				"top": 3,
				"candidates": [
					{
						"rank": 1,
						"function_name": "tokenizeWord",
						"file": "src/tokenizer.ts",
						"self_percent": 55.0,
						"cumulative_percent": 55.0,
						"call_count": 120000,
						"is_pure_heuristic": true,
						"purity_reason": "pure string-to-token transformation without side effects"
					}
				],
				"summary": "tokenizeWord is primary hotspot",
				"timestamp": "2026-11-23T11:25:00Z"
			}`,
			expectError: false,
		},
		{
			name:       "missing unit_id",
			schemaName: "profile.report.schema.json",
			payload: `{
				"language": "python",
				"profiler": "cProfile",
				"total_duration_ms": 1000.0,
				"top": 1,
				"candidates": [],
				"summary": "ok",
				"timestamp": "now"
			}`,
			expectError: true,
		},
		{
			name:       "missing candidates field",
			schemaName: "profile.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"language": "python",
				"profiler": "cProfile",
				"total_duration_ms": 1000.0,
				"top": 1,
				"summary": "ok",
				"timestamp": "now"
			}`,
			expectError: true,
		},
		{
			name:       "candidate missing required function_name",
			schemaName: "profile.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"language": "python",
				"profiler": "cProfile",
				"total_duration_ms": 1000.0,
				"top": 1,
				"candidates": [
					{
						"rank": 1,
						"file": "foo.py",
						"self_percent": 50.0,
						"cumulative_percent": 50.0,
						"call_count": 10,
						"is_pure_heuristic": false,
						"purity_reason": "none"
					}
				],
				"summary": "ok",
				"timestamp": "now"
			}`,
			expectError: true,
		},
		{
			name:       "self_percent out of range (> 100)",
			schemaName: "profile.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"language": "python",
				"profiler": "cProfile",
				"total_duration_ms": 1000.0,
				"top": 1,
				"candidates": [
					{
						"rank": 1,
						"function_name": "fn",
						"file": "foo.py",
						"self_percent": 105.0,
						"cumulative_percent": 105.0,
						"call_count": 10,
						"is_pure_heuristic": false,
						"purity_reason": "none"
					}
				],
				"summary": "ok",
				"timestamp": "now"
			}`,
			expectError: true,
		},
		{
			name:       "top less than 1",
			schemaName: "profile.report.schema.json",
			payload: `{
				"unit_id": "unit_test",
				"language": "python",
				"profiler": "cProfile",
				"total_duration_ms": 1000.0,
				"top": 0,
				"candidates": [],
				"summary": "ok",
				"timestamp": "now"
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

func TestValidateV2_Effects(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid effects container with http, smtp, and generic effects",
			schemaName: "effects.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_checkout",
				"case_id": "case_001",
				"total_effects": 3,
				"effects": [
					{
						"type": "http",
						"order": 0,
						"target": "https://api.stripe.com/v1/charges",
						"method": "POST",
						"headers": {
							"Authorization": "Bearer REDACTED"
						},
						"body_hash": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
						"count": 1
					},
					{
						"type": "smtp",
						"order": 1,
						"target": "smtp.sendgrid.net:587",
						"envelope_from": "receipts@example.com",
						"envelope_to": ["user@example.com"],
						"subject": "Payment Receipt",
						"count": 1
					},
					{
						"type": "generic",
						"order": 2,
						"target": "redis://cache:6379",
						"count": 1
					}
				],
				"summary": "3 recorded side effects"
			}`,
			expectError: false,
		},
		{
			name:       "valid direct array of effects using shorthand schema name",
			schemaName: "effects",
			payload: `[
				{
					"type": "http",
					"order": 1,
					"target": "https://api.example.com/v1/notify",
					"method": "POST",
					"headers": {
						"Content-Type": "application/json"
					},
					"body_hash": "a1b2c3d4e5f67890123456789012345678901234567890123456789012345678",
					"count": 1
				}
			]`,
			expectError: false,
		},
		{
			name:       "invalid effect type",
			schemaName: "effects.schema.json",
			payload: `[
				{
					"type": "ftp",
					"order": 0,
					"target": "ftp://files.example.com"
				}
			]`,
			expectError: true,
		},
		{
			name:       "negative order",
			schemaName: "effects.schema.json",
			payload: `[
				{
					"type": "http",
					"order": -1,
					"target": "https://example.com"
				}
			]`,
			expectError: true,
		},
		{
			name:       "missing target in effect object",
			schemaName: "effects.schema.json",
			payload: `[
				{
					"type": "http",
					"order": 0
				}
			]`,
			expectError: true,
		},
		{
			name:       "container object missing effects array",
			schemaName: "effects.schema.json",
			payload: `{
				"unit_id": "unit_checkout",
				"total_effects": 0
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

func TestValidateV2_GoOutput(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid go hotpath pipeline output with full filename",
			schemaName: "go.output.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_billing",
				"function": "calculate_tax",
				"unit_state": "In progress",
				"current_step": "implement",
				"status": "paused_for_implementation",
				"steps": [
					{
						"name": "target",
						"status": "completed",
						"summary": "found candidate calculate_tax"
					},
					{
						"name": "record",
						"status": "completed",
						"artifacts": [".go-anything/cases/unit_billing.jsonl"]
					},
					{
						"name": "implement",
						"status": "waiting",
						"summary": "implement calculate_tax in handlers_impl.go"
					}
				],
				"human_action_required": "go-anything approve --unit unit_billing",
				"timestamp": "2026-11-24T10:00:00Z"
			}`,
			expectError: false,
		},
		{
			name:       "valid go pipeline output with shorthand",
			schemaName: "go.output",
			payload: `{
				"unit_id": "unit_auth",
				"function": "hash_password",
				"current_step": "completed",
				"status": "completed",
				"steps": []
			}`,
			expectError: false,
		},
		{
			name:       "missing function",
			schemaName: "go.output.schema.json",
			payload: `{
				"unit_id": "unit_auth",
				"current_step": "target",
				"status": "in_progress",
				"steps": []
			}`,
			expectError: true,
		},
		{
			name:       "invalid pipeline status",
			schemaName: "go.output.schema.json",
			payload: `{
				"unit_id": "unit_auth",
				"function": "hash_password",
				"current_step": "target",
				"status": "unknown_status",
				"steps": []
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

func TestValidateV2_ReportOutput(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid unit migration report output with full filename",
			schemaName: "report.output.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_billing",
				"state": "Verified",
				"evidence_profile": "full",
				"cases_count": 42,
				"replay_pass_rate": 100.0,
				"diff_status": "clean",
				"gate_status": "passed",
				"benchmark": {
					"speedup_ratio": 3.5,
					"original_p50_ms": 12.0,
					"go_p50_ms": 3.4,
					"allocs_per_op": 2
				},
				"markdown": "# Migration Report\n\nAll checks passed.",
				"generated_at": "2026-11-24T10:30:00Z"
			}`,
			expectError: false,
		},
		{
			name:       "valid unit migration report output with shorthand",
			schemaName: "report.output",
			payload: `{
				"unit_id": "unit_auth",
				"state": "Planned",
				"cases_count": 0,
				"generated_at": "2026-11-24T10:30:00Z"
			}`,
			expectError: false,
		},
		{
			name:       "missing state",
			schemaName: "report.output.schema.json",
			payload: `{
				"unit_id": "unit_auth",
				"cases_count": 10,
				"generated_at": "2026-11-24T10:30:00Z"
			}`,
			expectError: true,
		},
		{
			name:       "missing generated_at",
			schemaName: "report.output.schema.json",
			payload: `{
				"unit_id": "unit_auth",
				"state": "Verified",
				"cases_count": 10
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

func TestValidateV2_MapOutput(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:       "valid unit function map output with full filename",
			schemaName: "map.output.schema.json",
			payload: `{
				"schema_version": "v2",
				"unit_id": "unit_billing",
				"total_functions": 2,
				"functions": [
					{
						"name": "calculate_tax",
						"status": "scaffolded",
						"routing": "go",
						"idempotent": true,
						"cases_count": 5
					},
					{
						"name": "apply_discount",
						"status": "original",
						"routing": "original",
						"idempotent": true,
						"cases_count": 2
					}
				],
				"timestamp": "2026-11-24T11:00:00Z"
			}`,
			expectError: false,
		},
		{
			name:       "valid unit function map output with shorthand",
			schemaName: "map.output",
			payload: `{
				"unit_id": "unit_auth",
				"total_functions": 0,
				"functions": []
			}`,
			expectError: false,
		},
		{
			name:       "missing unit_id",
			schemaName: "map.output.schema.json",
			payload: `{
				"total_functions": 0,
				"functions": []
			}`,
			expectError: true,
		},
		{
			name:       "missing functions",
			schemaName: "map.output.schema.json",
			payload: `{
				"unit_id": "unit_auth",
				"total_functions": 0
			}`,
			expectError: true,
		},
		{
			name:       "function item missing required status",
			schemaName: "map.output.schema.json",
			payload: `{
				"unit_id": "unit_auth",
				"total_functions": 1,
				"functions": [
					{
						"name": "login"
					}
				]
			}`,
			expectError: true,
		},
		{
			name:       "invalid function status enum",
			schemaName: "map.output.schema.json",
			payload: `{
				"unit_id": "unit_auth",
				"total_functions": 1,
				"functions": [
					{
						"name": "login",
						"status": "unknown_state"
					}
				]
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

func TestValidateV2_MCPSchemas(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		payload     string
		expectError bool
	}{
		{
			name:        "analyze input schema",
			schemaName:  "mcp/analyze.input.schema.json",
			payload:     `{"dir": "/workspace", "unit_id": "auth"}`,
			expectError: false,
		},
		{
			name:        "analyze output schema",
			schemaName:  "mcp/analyze.output.schema.json",
			payload:     `{"total_units": 1, "waves": [{"wave": 0, "units": ["auth"]}]}`,
			expectError: false,
		},
		{
			name:        "bridge input schema",
			schemaName:  "mcp/bridge.input.schema.json",
			payload:     `{"unit_id": "auth", "target_language": "go"}`,
			expectError: false,
		},
		{
			name:        "bridge output schema",
			schemaName:  "mcp/bridge.output.schema.json",
			payload:     `{"unit_id": "auth", "generated_files": ["sidecar.go", "bridge.py"], "handlers_impl_preserved": true}`,
			expectError: false,
		},
		{
			name:        "fuzz_bench input schema",
			schemaName:  "mcp/fuzz_bench.input.schema.json",
			payload:     `{"unit_id": "auth", "mode": "fuzz", "time": "10s", "iterations": 50}`,
			expectError: false,
		},
		{
			name:        "fuzz_bench output schema",
			schemaName:  "mcp/fuzz_bench.output.schema.json",
			payload:     `{"unit_id": "auth", "mode": "fuzz", "status": "completed", "divergence_count": 0, "speedup_ratio": 2.5}`,
			expectError: false,
		},
		{
			name:        "hotpath input schema",
			schemaName:  "mcp/hotpath.input.schema.json",
			payload:     `{"unit_id": "auth", "auto_advance": true}`,
			expectError: false,
		},
		{
			name:        "hotpath output schema",
			schemaName:  "mcp/hotpath.output.schema.json",
			payload:     `{"unit_id": "auth", "current_step": "record", "status": "in_progress"}`,
			expectError: false,
		},
		{
			name:        "profile input schema",
			schemaName:  "mcp/profile.input.schema.json",
			payload:     `{"unit_id": "auth", "top": 5}`,
			expectError: false,
		},
		{
			name:        "profile output schema",
			schemaName:  "mcp/profile.output.schema.json",
			payload:     `{"unit_id": "auth", "candidates": [{"rank": 1, "function_name": "calc", "self_percent": 60.0, "cumulative_percent": 65.0}]}`,
			expectError: false,
		},
		{
			name:        "shadow_status input schema",
			schemaName:  "mcp/shadow_status.input.schema.json",
			payload:     `{"unit_id": "auth"}`,
			expectError: false,
		},
		{
			name:        "shadow_status output schema",
			schemaName:  "mcp/shadow_status.output.schema.json",
			payload:     `{"shadow_active": true, "units": [{"unit_id": "auth", "status": "active", "requests_count": 100, "mismatches_count": 0}]}`,
			expectError: false,
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

func TestValidateV2_Evidence(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		expectError bool
	}{
		{
			name: "valid v2 evidence",
			payload: `{
				"schema_version": "v2",
				"evidence_id": "evi-auth-001",
				"unit_id": "pkg/auth",
				"commit_sha": "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
				"profile": "full",
				"profile_version": "2.0.1",
				"profile_checks": ["build", "format", "vet", "staticcheck", "race", "govulncheck", "replay", "error_paths", "api_compat"],
				"verified_at": "2026-11-25T10:00:00Z",
				"verified_by": "mcp-agent",
				"checks": [{"name": "build", "status": "pass"}],
				"mismatches_count": 0
			}`,
			expectError: false,
		},
		{
			name: "rejects v1 schema_version",
			payload: `{
				"schema_version": "v1",
				"evidence_id": "evi-auth-001",
				"unit_id": "pkg/auth",
				"commit_sha": "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
				"profile": "full",
				"profile_version": "2.0.1",
				"profile_checks": ["build"],
				"verified_at": "2026-11-25T10:00:00Z",
				"verified_by": "mcp-agent",
				"checks": [{"name": "build", "status": "pass"}],
				"mismatches_count": 0
			}`,
			expectError: true,
		},
		{
			name: "rejects missing profile_version",
			payload: `{
				"schema_version": "v2",
				"evidence_id": "evi-auth-001",
				"unit_id": "pkg/auth",
				"commit_sha": "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
				"profile": "full",
				"profile_checks": ["build"],
				"verified_at": "2026-11-25T10:00:00Z",
				"verified_by": "mcp-agent",
				"checks": [{"name": "build", "status": "pass"}],
				"mismatches_count": 0
			}`,
			expectError: true,
		},
		{
			name: "rejects missing profile_checks",
			payload: `{
				"schema_version": "v2",
				"evidence_id": "evi-auth-001",
				"unit_id": "pkg/auth",
				"commit_sha": "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
				"profile": "full",
				"profile_version": "2.0.1",
				"verified_at": "2026-11-25T10:00:00Z",
				"verified_by": "mcp-agent",
				"checks": [{"name": "build", "status": "pass"}],
				"mismatches_count": 0
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schemas.ValidateV2("evidence.schema.json", []byte(tt.payload))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
