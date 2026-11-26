package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestV1Alpha1Workspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dotDir := filepath.Join(dir, ".go-anything")
	require.NoError(t, os.MkdirAll(dotDir, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dotDir, "units"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dotDir, "cases"), 0755))

	manifestJSON := `{
  "schema_version": "v1alpha1",
  "project_name": "cli-test",
  "units": ["auth"],
  "waves": [{"wave": 0, "unit_ids": ["auth"]}]
}`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "manifest.json"), []byte(manifestJSON), 0644))

	unitJSON := `{
  "schema_version": "v1alpha1",
  "id": "auth",
  "name": "Auth",
  "state": "Planned",
  "evidence_profile": "replay"
}`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "units", "auth.json"), []byte(unitJSON), 0644))

	caseJSONL := `{"schema_version":"v1alpha1","case_id":"c1","unit_id":"auth","kind":"happy","source_revision":"abc","runtime":"go1.22","captured_at":"2026-11-20T10:00:00Z","input":{},"expected_output":{}}
`
	require.NoError(t, os.WriteFile(filepath.Join(dotDir, "cases", "auth.jsonl"), []byte(caseJSONL), 0644))

	return dir
}

func TestCLI_MigrateCommand_FlagDir(t *testing.T) {
	wsDir := createTestV1Alpha1Workspace(t)

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"migrate", "--dir", wsDir}, &stdout, &stderr)
	require.NoError(t, err, "stderr: %s", stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "Successfully upgraded workspace")
	assert.Contains(t, out, "manifest.json upgraded")
	assert.Contains(t, out, "units: 1 upgraded")
	assert.Contains(t, out, "cases: 1 case(s) upgraded")
	assert.Contains(t, out, "All files validated successfully against v1 schemas.")

	// Check files on disk
	manifestData, err := os.ReadFile(filepath.Join(wsDir, ".go-anything", "manifest.json"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(manifestData, &m))
	assert.Equal(t, adapterkit.SchemaVersionV1, m["schema_version"])
	assert.NoError(t, schemas.ValidateJSON(adapterkit.SchemaVersionV1, "manifest.schema.json", manifestData))

	// Second run must be idempotent
	var stdout2, stderr2 bytes.Buffer
	err = runCLI([]string{"migrate", "--dir", wsDir}, &stdout2, &stderr2)
	require.NoError(t, err)
	out2 := stdout2.String()
	assert.Contains(t, out2, "already up to date (v1)")
	assert.Contains(t, out2, "No files were modified.")
}

func TestCLI_MigrateCommand_PositionalArg(t *testing.T) {
	wsDir := createTestV1Alpha1Workspace(t)

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"migrate", wsDir}, &stdout, &stderr)
	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "Successfully upgraded workspace")
}

func TestCLI_MigrateCommand_NoWorkspace(t *testing.T) {
	emptyDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"migrate", "--dir", emptyDir}, &stdout, &stderr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no .go-anything workspace found")
}
