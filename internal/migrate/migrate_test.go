package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefat/go-anything/internal/migrate"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupV1Alpha1Workspace creates a realistic v1alpha1 workspace for testing.
func setupV1Alpha1Workspace(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	dotDir := filepath.Join(tmpDir, ".go-anything")
	require.NoError(t, os.MkdirAll(dotDir, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dotDir, "units"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dotDir, "cases"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dotDir, "reports"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dotDir, "evidence"), 0755))

	// 1. manifest.json
	manifestJSON := `{
  "schema_version": "v1alpha1",
  "project_name": "ecommerce-api",
  "source_language": "python",
  "target_language": "go",
  "units": [
    "auth",
    "billing"
  ],
  "waves": [
    {
      "wave": 0,
      "unit_ids": ["auth"]
    },
    {
      "wave": 1,
      "unit_ids": ["billing"]
    }
  ],
  "metadata": {
    "framework": "fastapi",
    "team": "core"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "manifest.json"), []byte(manifestJSON), 0644))

	// 2. rules.json
	rulesJSON := `{
  "schema_version": "v1alpha1",
  "wildcards": [
    {
      "path": "headers.x-request-id",
      "type": "uuid",
      "description": "Unique request identifier"
    }
  ],
  "normalizers": [
    {
      "path": "body.timestamp",
      "type": "date_format",
      "params": {
        "format": "RFC3339"
      }
    }
  ],
  "field_semantics": [
    {
      "path": "headers.authorization",
      "semantic": "secret",
      "description": "Bearer token"
    }
  ],
  "metadata": {
    "strict_mode": true
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "rules.json"), []byte(rulesJSON), 0644))

	// 3. units/auth.json and units/billing.json
	unitAuthJSON := `{
  "schema_version": "v1alpha1",
  "id": "auth",
  "name": "Authentication Unit",
  "package_path": "app/services/auth",
  "wave": 0,
  "state": "Verified",
  "evidence_profile": "full",
  "dependencies": [],
  "risk": "medium",
  "fallback_count": 0,
  "last_updated": "2026-11-20T10:00:00Z",
  "metadata": {
    "has_jwt": true
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "units", "auth.json"), []byte(unitAuthJSON), 0644))

	unitBillingJSON := `{
  "schema_version": "v1alpha1",
  "id": "billing",
  "name": "Billing Unit",
  "package_path": "app/services/billing",
  "wave": 1,
  "state": "In progress",
  "evidence_profile": "replay",
  "dependencies": ["auth"],
  "risk": "high",
  "fallback_count": 2,
  "last_updated": "2026-11-20T11:00:00Z"
}`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "units", "billing.json"), []byte(unitBillingJSON), 0644))

	// 4. cases/auth.jsonl
	caseAuthJSONL := `{"schema_version":"v1alpha1","case_id":"c-auth-001","unit_id":"auth","kind":"happy","source_revision":"abcdef1234567890abcdef1234567890abcdef12","runtime":"python3.11","captured_at":"2026-11-20T10:05:00Z","input":{"method":"POST","path":"/login","body":{"user":"alice","pass":"[REDACTED]"}},"expected_output":{"status_code":200,"body":{"token":"jwt-token-123"}}}
{"schema_version":"v1alpha1","case_id":"c-auth-002","unit_id":"auth","kind":"trap","source_revision":"abcdef1234567890abcdef1234567890abcdef12","runtime":"python3.11","captured_at":"2026-11-20T10:06:00Z","trap_type":"empty_collection","input":{"method":"GET","path":"/roles"},"expected_output":{"status_code":200,"body":{"roles":[]}}}
`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "cases", "auth.jsonl"), []byte(caseAuthJSONL), 0644))

	// 5. reports/diff_auth.json and reports/shadow_mismatches.jsonl
	reportDiffJSON := `{
  "schema_version": "v1alpha1",
  "report_id": "rep-diff-001",
  "unit_id": "auth",
  "case_id": "c-auth-001",
  "report_type": "diff",
  "mode": "structural_json",
  "status": "pass",
  "created_at": "2026-11-20T10:10:00Z",
  "duration_ms": 12.5,
  "summary": "100% field parity",
  "details": {
    "checks_run": 5
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "reports", "diff_auth.json"), []byte(reportDiffJSON), 0644))

	reportShadowJSONL := `{"schema_version":"v1alpha1","report_id":"rep-shadow-001","unit_id":"auth","report_type":"diff","status":"pass","created_at":"2026-11-20T10:15:00Z"}
`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "reports", "shadow_mismatches.jsonl"), []byte(reportShadowJSONL), 0644))

	// 6. evidence/evi-auth.json
	evidenceJSON := `{
  "schema_version": "v1alpha1",
  "evidence_id": "evi-auth-001",
  "unit_id": "auth",
  "commit_sha": "0123456789abcdef0123456789abcdef01234567",
  "profile": "full",
  "verified_at": "2026-11-20T10:20:00Z",
  "verified_by": "coordinator",
  "checks": [
    {"name": "build", "status": "pass", "duration_ms": 150},
    {"name": "race", "status": "pass", "duration_ms": 320}
  ],
  "mismatches_count": 0,
  "metadata": {
    "git_branch": "main"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "evidence", "evi-auth.json"), []byte(evidenceJSON), 0644))

	return tmpDir
}

func TestMigrateWorkspace_FullUpgrade(t *testing.T) {
	workspaceDir := setupV1Alpha1Workspace(t)

	// Run migration
	result, err := migrate.MigrateWorkspace(workspaceDir)
	require.NoError(t, err)

	assert.Equal(t, workspaceDir, result.WorkspaceRoot)
	assert.Equal(t, adapterkit.SchemaVersionV1Alpha1, result.FromVersion)
	assert.Equal(t, adapterkit.SchemaVersionV1, result.ToVersion)
	assert.True(t, result.ManifestUpgraded)
	assert.True(t, result.RulesUpgraded)
	assert.Equal(t, 2, result.UnitsTotal)
	assert.Equal(t, 2, result.UnitsUpgraded)
	assert.Equal(t, 1, result.CasesFilesTotal)
	assert.Equal(t, 2, result.CasesCount)
	assert.Equal(t, 2, result.CasesUpgraded)
	assert.Equal(t, 2, result.ReportsFilesTotal)
	assert.Equal(t, 2, result.ReportsUpgraded)
	assert.Equal(t, 1, result.EvidenceFilesTotal)
	assert.Equal(t, 1, result.EvidenceUpgraded)
	assert.Equal(t, 8, result.TotalFilesModified)

	dotDir := filepath.Join(workspaceDir, ".go-anything")

	// Validate manifest.json is now v1 and valid
	manifestBytes, err := os.ReadFile(filepath.Join(dotDir, "manifest.json"))
	require.NoError(t, err)
	assert.NoError(t, schemas.ValidateJSON("v1", "manifest.schema.json", manifestBytes))
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(manifestBytes, &manifest))
	assert.Equal(t, "v1", manifest["schema_version"])
	assert.Equal(t, "ecommerce-api", manifest["project_name"])
	assert.Equal(t, "python", manifest["source_language"])
	assert.Equal(t, "fastapi", manifest["metadata"].(map[string]any)["framework"])

	// Validate rules.json is v1 and valid
	rulesBytes, err := os.ReadFile(filepath.Join(dotDir, "rules.json"))
	require.NoError(t, err)
	assert.NoError(t, schemas.ValidateJSON("v1", "rules.schema.json", rulesBytes))
	var rules map[string]any
	require.NoError(t, json.Unmarshal(rulesBytes, &rules))
	assert.Equal(t, "v1", rules["schema_version"])
	assert.True(t, rules["metadata"].(map[string]any)["strict_mode"].(bool))

	// Validate units are v1 and valid
	authUnitBytes, err := os.ReadFile(filepath.Join(dotDir, "units", "auth.json"))
	require.NoError(t, err)
	assert.NoError(t, schemas.ValidateJSON("v1", "units.schema.json", authUnitBytes))
	var authUnit map[string]any
	require.NoError(t, json.Unmarshal(authUnitBytes, &authUnit))
	assert.Equal(t, "v1", authUnit["schema_version"])
	assert.Equal(t, "auth", authUnit["id"])
	assert.Equal(t, "Verified", authUnit["state"])

	billingUnitBytes, err := os.ReadFile(filepath.Join(dotDir, "units", "billing.json"))
	require.NoError(t, err)
	assert.NoError(t, schemas.ValidateJSON("v1", "units.schema.json", billingUnitBytes))

	// Validate cases are v1 and valid
	casesBytes, err := os.ReadFile(filepath.Join(dotDir, "cases", "auth.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(casesBytes), `"schema_version":"v1"`)
	assert.NotContains(t, string(casesBytes), "v1alpha1")

	// Validate reports are v1 and valid
	reportBytes, err := os.ReadFile(filepath.Join(dotDir, "reports", "diff_auth.json"))
	require.NoError(t, err)
	assert.NoError(t, schemas.ValidateJSON("v1", "reports.schema.json", reportBytes))

	// Validate evidence is v1 and valid
	evidenceBytes, err := os.ReadFile(filepath.Join(dotDir, "evidence", "evi-auth.json"))
	require.NoError(t, err)
	assert.NoError(t, schemas.ValidateJSON("v1", "evidence.schema.json", evidenceBytes))
}

func TestMigrateWorkspace_StrictIdempotency(t *testing.T) {
	workspaceDir := setupV1Alpha1Workspace(t)

	// First migration run
	res1, err := migrate.MigrateWorkspace(workspaceDir)
	require.NoError(t, err)
	require.Equal(t, 8, res1.TotalFilesModified)

	dotDir := filepath.Join(workspaceDir, ".go-anything")

	// Snapshot all files after first run
	manifestBefore, err := os.ReadFile(filepath.Join(dotDir, "manifest.json"))
	require.NoError(t, err)
	rulesBefore, err := os.ReadFile(filepath.Join(dotDir, "rules.json"))
	require.NoError(t, err)
	authBefore, err := os.ReadFile(filepath.Join(dotDir, "units", "auth.json"))
	require.NoError(t, err)
	casesBefore, err := os.ReadFile(filepath.Join(dotDir, "cases", "auth.jsonl"))
	require.NoError(t, err)

	// Second migration run (MUST be strictly idempotent: 0 modifications)
	res2, err := migrate.MigrateWorkspace(workspaceDir)
	require.NoError(t, err)
	assert.Equal(t, 0, res2.TotalFilesModified)
	assert.False(t, res2.ManifestUpgraded)
	assert.False(t, res2.RulesUpgraded)
	assert.Equal(t, 0, res2.UnitsUpgraded)
	assert.Equal(t, 0, res2.CasesUpgraded)
	assert.Equal(t, 0, res2.ReportsUpgraded)
	assert.Equal(t, 0, res2.EvidenceUpgraded)

	// Files on disk must be byte-for-byte identical
	manifestAfter, err := os.ReadFile(filepath.Join(dotDir, "manifest.json"))
	require.NoError(t, err)
	assert.Equal(t, manifestBefore, manifestAfter)

	rulesAfter, err := os.ReadFile(filepath.Join(dotDir, "rules.json"))
	require.NoError(t, err)
	assert.Equal(t, rulesBefore, rulesAfter)

	authAfter, err := os.ReadFile(filepath.Join(dotDir, "units", "auth.json"))
	require.NoError(t, err)
	assert.Equal(t, authBefore, authAfter)

	casesAfter, err := os.ReadFile(filepath.Join(dotDir, "cases", "auth.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, casesBefore, casesAfter)
}

func TestMigrateWorkspace_DotDirDirectPath(t *testing.T) {
	workspaceDir := setupV1Alpha1Workspace(t)
	dotDir := filepath.Join(workspaceDir, ".go-anything")

	// Calling MigrateWorkspace directly with the .go-anything folder path
	result, err := migrate.MigrateWorkspace(dotDir)
	require.NoError(t, err)
	assert.Equal(t, 8, result.TotalFilesModified)
	assert.Equal(t, workspaceDir, result.WorkspaceRoot)
}

func TestMigrateWorkspace_NoWorkspaceFound(t *testing.T) {
	emptyDir := t.TempDir()
	_, err := migrate.MigrateWorkspace(emptyDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no .go-anything workspace found")
}

func TestMigrateWorkspace_InvalidJSONFileFails(t *testing.T) {
	tmpDir := t.TempDir()
	dotDir := filepath.Join(tmpDir, ".go-anything")
	require.NoError(t, os.MkdirAll(dotDir, 0755))
	// Corrupt manifest
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "manifest.json"), []byte("{corrupt-json"), 0644))

	_, err := migrate.MigrateWorkspace(tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to migrate manifest.json")
}
