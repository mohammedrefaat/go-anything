package fromtests_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/recorder/fromtests"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIngester_Normal(t *testing.T) {
	tmpDir := t.TempDir()
	captureDir := filepath.Join(tmpDir, "captures")
	require.NoError(t, os.MkdirAll(captureDir, 0755))

	// Write capture file
	rec1 := fromtests.CapturedRecord{
		UnitID:   "unit-test",
		TestName: "TestAdd",
		Input:    map[string]any{"argv": []any{"add", "1", "2"}},
		Output:   map[string]any{"stdout": "3\n", "exit_code": float64(0)},
	}
	rec2 := fromtests.CapturedRecord{
		UnitID:   "unit-test",
		TestName: "TestSub",
		Input:    map[string]any{"argv": []any{"sub", "5", "2"}},
		Output:   map[string]any{"stdout": "3\n", "exit_code": float64(0)},
	}

	capFile := filepath.Join(captureDir, "unit-test_observed.jsonl")
	data1, _ := json.Marshal(rec1)
	data2, _ := json.Marshal(rec2)
	require.NoError(t, os.WriteFile(capFile, []byte(string(data1)+"\n"+string(data2)+"\n"), 0644))

	ing := fromtests.NewIngester(fromtests.IngestConfig{
		UnitID:        "unit-test",
		CaptureDir:    captureDir,
		WorkspaceRoot: tmpDir,
		MaxCases:      100,
	})

	res, err := ing.Ingest(context.Background())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, 2, res.TotalIngested)
	assert.Equal(t, 0, res.Deduplicated)
	assert.Len(t, res.SavedCases, 2)
	assert.Equal(t, adapterkit.KindFromTest, res.SavedCases[0].Kind)
	assert.Equal(t, adapterkit.KindFromTest, res.SavedCases[1].Kind)

	// Verify cases file on disk
	assert.FileExists(t, res.CasesFile)
}

func TestIngester_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	captureDir := filepath.Join(tmpDir, "captures")
	require.NoError(t, os.MkdirAll(captureDir, 0755))

	rec := fromtests.CapturedRecord{
		UnitID:   "unit-dedup",
		TestName: "TestRepeated",
		Input:    map[string]any{"argv": []any{"repeat"}},
		Output:   map[string]any{"stdout": "done\n"},
	}

	capFile := filepath.Join(captureDir, "records.jsonl")
	data, _ := json.Marshal(rec)
	// Write duplicate lines
	content := fmt.Sprintf("%s\n%s\n%s\n", data, data, data)
	require.NoError(t, os.WriteFile(capFile, []byte(content), 0644))

	ing := fromtests.NewIngester(fromtests.IngestConfig{
		UnitID:        "unit-dedup",
		CaptureDir:    captureDir,
		WorkspaceRoot: tmpDir,
		MaxCases:      10,
	})

	res, err := ing.Ingest(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, res.TotalIngested)
	assert.Equal(t, 2, res.Deduplicated)
	assert.Len(t, res.SavedCases, 1)
}

func TestIngester_MaxCasesCapping(t *testing.T) {
	tmpDir := t.TempDir()
	captureDir := filepath.Join(tmpDir, "captures")
	require.NoError(t, os.MkdirAll(captureDir, 0755))

	var sb strings.Builder
	for i := 0; i < 10; i++ {
		rec := fromtests.CapturedRecord{
			UnitID:   "unit-cap",
			TestName: "TestIndex",
			Input:    map[string]any{"i": i},
			Output:   "ok",
		}
		data, _ := json.Marshal(rec)
		sb.WriteString(string(data) + "\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(captureDir, "data.jsonl"), []byte(sb.String()), 0644))

	ing := fromtests.NewIngester(fromtests.IngestConfig{
		UnitID:        "unit-cap",
		CaptureDir:    captureDir,
		WorkspaceRoot: tmpDir,
		MaxCases:      3,
	})

	res, err := ing.Ingest(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 10, res.TotalIngested)
	assert.Equal(t, 7, res.Capped)
	assert.Len(t, res.SavedCases, 3)
}

// TestIngester_SecretRedaction strictly verifies Rule R-4:
// Fail-closed secret redaction substitutes fake secrets before any disk write.
func TestIngester_SecretRedaction(t *testing.T) {
	tmpDir := t.TempDir()
	captureDir := filepath.Join(tmpDir, "captures")
	require.NoError(t, os.MkdirAll(captureDir, 0755))

	fakeSecret := "sk-ant-live-99999-secret-token"

	rec := fromtests.CapturedRecord{
		UnitID:   "unit-secrets",
		TestName: "TestAuthWithSecret",
		Input: map[string]any{
			"api_key": fakeSecret,
			"body":    "Bearer " + fakeSecret,
		},
		Output: map[string]any{
			"stdout": "authenticated with " + fakeSecret,
		},
	}

	capFile := filepath.Join(captureDir, "records.jsonl")
	data, _ := json.Marshal(rec)
	require.NoError(t, os.WriteFile(capFile, []byte(string(data)+"\n"), 0644))

	ing := fromtests.NewIngester(fromtests.IngestConfig{
		UnitID:        "unit-secrets",
		CaptureDir:    captureDir,
		WorkspaceRoot: tmpDir,
		MaxCases:      10,
	})

	res, err := ing.Ingest(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, res.SavedCases)

	// Read saved file from disk
	savedData, err := os.ReadFile(res.CasesFile)
	require.NoError(t, err)

	// CRITICAL ASSERTION: The plain-text fake secret MUST NOT exist anywhere on disk!
	assert.NotContains(t, string(savedData), fakeSecret,
		"Rule R-4 Violation: secret %s was written to disk in plain text!", fakeSecret)

	// Verify case is valid JSON and parseable
	var savedCase adapterkit.Case
	scanner := bufio.NewScanner(strings.NewReader(string(savedData)))
	require.True(t, scanner.Scan())
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &savedCase))
	assert.Equal(t, adapterkit.KindFromTest, savedCase.Kind)
}
