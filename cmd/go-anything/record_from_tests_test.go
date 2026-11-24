package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefat/go-anything/pkg/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_RecordHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"record", "--help"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "go-anything record")
	assert.Contains(t, out, "--from-tests")
	assert.Contains(t, out, "--max-cases")
	assert.Contains(t, out, "-- <test-command>")
	assert.Contains(t, out, "--unit, -u")
}

func setupRecordWorkspace(t *testing.T, unitID string) string {
	tempDir := t.TempDir()

	manifestDir := filepath.Join(tempDir, ".go-anything")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	manifest := adapterkit.Manifest{
		SchemaVersion:  adapterkit.SchemaVersionV1,
		ProjectName:    "record-test-ws",
		SourceLanguage: "go",
		TargetLanguage: "go",
		Units:          []string{unitID},
	}
	mData, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "manifest.json"), mData, 0644))

	return tempDir
}

// TestCopyRecordObservation runs as the recorder's test command in these fixtures.
func TestCopyRecordObservation(t *testing.T) {
	args := os.Args
	if len(args) != 4 || args[2] != "--" {
		t.Skip("only run as a recording subprocess")
	}
	captureDir := os.Getenv("GO_ANYTHING_RECORD")
	require.NotEmpty(t, captureDir)
	content, err := os.ReadFile(args[3])
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(captureDir, filepath.Base(args[3])), content, 0600))
}

func copyRecordObservationCommand(t *testing.T, observedFile string) []string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return []string{exe, "-test.run=^TestCopyRecordObservation$", "--", observedFile}
}

func TestCLI_RecordFromTests_GoObservation(t *testing.T) {
	unitID := "pkg/currency"
	tempDir := setupRecordWorkspace(t, unitID)

	var stdout, stderr bytes.Buffer
	captureDir := filepath.Join(tempDir, ".capture_test")
	require.NoError(t, os.MkdirAll(captureDir, 0755))

	// Directly test that when GO_ANYTHING_RECORD is active, Observe writes records
	t.Setenv("GO_ANYTHING_RECORD", captureDir)
	t.Setenv("GO_ANYTHING_UNIT", unitID)

	err := recorder.Observe(unitID, "Convert",
		map[string]any{"amount": 100, "from": "EUR", "to": "USD"},
		map[string]any{"result": 108.7},
	)
	require.NoError(t, err)

	observedFile := filepath.Join(captureDir, "pkg_currency_observed.jsonl")
	err = runCLI(append([]string{
		"record",
		"--unit=" + unitID,
		"--from-tests",
		"--dir=" + tempDir,
		"--",
	}, copyRecordObservationCommand(t, observedFile)...), &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Recorded 1 case(s) from tests")
}

func TestCLI_RecordFromTests_SecretRedaction(t *testing.T) {
	unitID := "pkg/auth"
	tempDir := setupRecordWorkspace(t, unitID)

	// Create temporary capture directory
	captureDir := filepath.Join(tempDir, "temp_capture")
	require.NoError(t, os.MkdirAll(captureDir, 0755))

	secretToken := "sk-ant-live-99999-secret-token"

	// Observe test with planted secret
	t.Setenv("GO_ANYTHING_RECORD", captureDir)
	t.Setenv("GO_ANYTHING_UNIT", unitID)

	err := recorder.Observe(unitID, "AuthenticateUser",
		map[string]any{
			"username": "admin",
			"api_key":  secretToken,
		},
		map[string]any{"authorized": true},
	)
	require.NoError(t, err)

	observedFile := filepath.Join(captureDir, "pkg_auth_observed.jsonl")
	require.FileExists(t, observedFile)

	var stdout, stderr bytes.Buffer
	err = runCLI(append([]string{
		"record",
		"--unit=" + unitID,
		"--from-tests",
		"--dir=" + tempDir,
		"--",
	}, copyRecordObservationCommand(t, observedFile)...), &stdout, &stderr)
	require.NoError(t, err)

	casesFile := filepath.Join(tempDir, ".go-anything", "cases", "pkg", "auth.jsonl")
	require.FileExists(t, casesFile)

	content, err := os.ReadFile(casesFile)
	require.NoError(t, err)
	rawText := string(content)

	// Rule R-4: Fail-closed secret redaction
	// The secret token string must NEVER appear on disk in .go-anything/cases/
	assert.NotContains(t, rawText, secretToken,
		"Security Violation (Rule R-4): Planted secret token %q was written to disk in plain text!", secretToken)

	var recordedCase adapterkit.Case
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(rawText)), &recordedCase))
	inputMap, ok := recordedCase.Input.Body.(map[string]any)
	require.True(t, ok)
	redactedValue := inputMap["api_key"].(string)
	assert.NotEqual(t, secretToken, redactedValue, "Secret must be substituted before disk write")
	assert.True(t, strings.HasPrefix(redactedValue, "sk-"), "Format-preserving substitution must maintain type format")
}

func TestCLI_RecordFromTests_DeduplicationAndCapping(t *testing.T) {
	unitID := "pkg/dedup"
	tempDir := setupRecordWorkspace(t, unitID)

	captureDir := filepath.Join(tempDir, "temp_capture_dedup")
	require.NoError(t, os.MkdirAll(captureDir, 0755))

	t.Setenv("GO_ANYTHING_RECORD", captureDir)
	t.Setenv("GO_ANYTHING_UNIT", unitID)

	// Plant identical inputs 5 times
	for i := 0; i < 5; i++ {
		_ = recorder.Observe(unitID, "Calculate",
			map[string]any{"x": 10, "y": 20},
			map[string]any{"sum": 30},
		)
	}

	// Plant 5 unique inputs
	for i := 1; i <= 5; i++ {
		_ = recorder.Observe(unitID, "Calculate",
			map[string]any{"x": i * 100, "y": i * 200},
			map[string]any{"sum": i * 300},
		)
	}

	observedFile := filepath.Join(captureDir, "pkg_dedup_observed.jsonl")
	require.FileExists(t, observedFile)

	var stdout, stderr bytes.Buffer
	// Set max-cases to 3
	err := runCLI(append([]string{
		"record",
		"--unit=" + unitID,
		"--from-tests",
		"--max-cases=3",
		"--dir=" + tempDir,
		"--",
	}, copyRecordObservationCommand(t, observedFile)...), &stdout, &stderr)
	require.NoError(t, err)

	casesFile := filepath.Join(tempDir, ".go-anything", "cases", "pkg", "dedup.jsonl")
	require.FileExists(t, casesFile)

	content, err := os.ReadFile(casesFile)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// Total unique inputs before capping: 1 (the 5 identical ones) + 5 unique = 6
	// Capped to 3:
	assert.Equal(t, 3, len(lines), "must be strictly capped to 3 cases")

	out := stdout.String()
	assert.Contains(t, out, "deduplicated: 4", "expected 4 duplicates eliminated")
	assert.Contains(t, out, "capped: 3", "expected 3 cases capped")
}

func TestCLI_RecordFromTests_Python(t *testing.T) {
	unitID := "core/validator"
	tempDir := setupRecordWorkspace(t, unitID)

	captureDir := filepath.Join(tempDir, "py_capture")
	require.NoError(t, os.MkdirAll(captureDir, 0755))

	t.Setenv("GO_ANYTHING_RECORD", captureDir)
	t.Setenv("GO_ANYTHING_UNIT", unitID)

	// Simulate pytest hook observations
	_ = recorder.Observe(unitID, "validate_email",
		map[string]any{"email": "valid@example.com"},
		map[string]any{"valid": true},
	)
	_ = recorder.Observe(unitID, "validate_email",
		map[string]any{"email": "invalid-address"},
		map[string]any{"valid": false},
	)

	observedFile := filepath.Join(captureDir, "core_validator_observed.jsonl")
	require.FileExists(t, observedFile)

	var stdout, stderr bytes.Buffer
	err := runCLI(append([]string{
		"record",
		"--unit=" + unitID,
		"--from-tests",
		"--dir=" + tempDir,
		"--",
	}, copyRecordObservationCommand(t, observedFile)...), &stdout, &stderr)
	require.NoError(t, err)

	casesFile := filepath.Join(tempDir, ".go-anything", "cases", "core", "validator.jsonl")
	require.FileExists(t, casesFile)

	content, err := os.ReadFile(casesFile)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	assert.Equal(t, 2, len(lines))

	var firstCase adapterkit.Case
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &firstCase))
	assert.Equal(t, adapterkit.CaseKind("from-test"), firstCase.Kind)
	assert.Equal(t, unitID, firstCase.UnitID)
}
