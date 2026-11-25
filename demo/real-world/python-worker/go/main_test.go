package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPythonWorker_CasesConformToSchema(t *testing.T) {
	casesPath := filepath.Join("..", "cases.jsonl")
	file, err := os.Open(casesPath)
	require.NoError(t, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	caseCount := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		caseCount++
		err := schemas.ValidateJSON(schemas.VersionV1, "cases.schema.json", line)
		assert.NoError(t, err, "case line must conform to v1 cases schema")
	}
	require.NoError(t, scanner.Err())
	assert.Equal(t, 8, caseCount, "must have 8 recorded cases")
}

func TestPythonWorker_GoPortReplaysCleanly(t *testing.T) {
	casesPath := filepath.Join("..", "cases.jsonl")
	file, err := os.Open(casesPath)
	require.NoError(t, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var c struct {
			CaseID string `json:"case_id"`
			Input  struct {
				Argv []string `json:"argv"`
			} `json:"input"`
			ExpectedOutput struct {
				Body any `json:"body"`
			} `json:"expected_output"`
		}
		require.NoError(t, json.Unmarshal(line, &c))
		require.GreaterOrEqual(t, len(c.Input.Argv), 2)

		taskName := c.Input.Argv[0]
		rawJSON := c.Input.Argv[1]

		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(rawJSON), &payload))

		actual := ProcessJob(taskName, payload)

		actualJSON, err := json.Marshal(actual)
		require.NoError(t, err)
		expectedJSON, err := json.Marshal(c.ExpectedOutput.Body)
		require.NoError(t, err)

		assert.JSONEq(t, string(expectedJSON), string(actualJSON), "mismatch for case %s", c.CaseID)
	}
}
