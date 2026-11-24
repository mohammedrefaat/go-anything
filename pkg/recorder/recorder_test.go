package recorder_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObserve_DisabledByDefault(t *testing.T) {
	// Without GO_ANYTHING_RECORD, Observe does nothing
	err := recorder.Observe("unit-test", "MyFunc", map[string]int{"a": 1}, 2)
	assert.NoError(t, err)
}

func TestObserve_EnabledFile(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "records.jsonl")

	t.Setenv("GO_ANYTHING_RECORD", outFile)
	t.Setenv("GO_ANYTHING_UNIT", "unit-test")

	err := recorder.Observe("unit-test", "Add", []int{1, 2}, 3)
	require.NoError(t, err)

	err = recorder.Observe("unit-test", "Add", []int{3, 4}, 7)
	require.NoError(t, err)

	// Different unit should be filtered
	err = recorder.Observe("other-unit", "Sub", []int{5, 2}, 3)
	require.NoError(t, err)

	f, err := os.Open(outFile)
	require.NoError(t, err)
	defer f.Close()

	var records []recorder.ObservationRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var rec recorder.ObservationRecord
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &rec))
		records = append(records, rec)
	}

	require.Len(t, records, 2)
	assert.Equal(t, "unit-test", records[0].UnitID)
	assert.Equal(t, "Add", records[0].FnName)
	assert.Equal(t, "TestObserve_EnabledFile", records[0].TestName)
	assert.Equal(t, "unit-test", records[1].UnitID)
}

func TestObserve_EnabledDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("GO_ANYTHING_RECORD", tmpDir)

	err := recorder.Observe("unit-calc", "Multiply", []int{2, 5}, 10)
	require.NoError(t, err)

	expectedFile := filepath.Join(tmpDir, "unit-calc_observed.jsonl")
	assert.FileExists(t, expectedFile)
}
