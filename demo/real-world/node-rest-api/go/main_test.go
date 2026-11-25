package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeRestAPI_CasesConformToSchema(t *testing.T) {
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
	assert.Equal(t, 9, caseCount, "must have 9 recorded cases")
}

func TestNodeRestAPI_GoPortReplaysCleanly(t *testing.T) {
	casesPath := filepath.Join("..", "cases.jsonl")
	file, err := os.Open(casesPath)
	require.NoError(t, err)
	defer file.Close()

	store := NewItemStore()
	handler := SetupHandler(store)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var c struct {
			CaseID string `json:"case_id"`
			Input  struct {
				Method string `json:"method"`
				Path   string `json:"path"`
				Body   any    `json:"body"`
			} `json:"input"`
			ExpectedOutput struct {
				StatusCode int `json:"status_code"`
				Body       any `json:"body"`
			} `json:"expected_output"`
		}
		require.NoError(t, json.Unmarshal(line, &c))

		var bodyReader *bytes.Reader
		if c.Input.Body != nil {
			data, _ := json.Marshal(c.Input.Body)
			bodyReader = bytes.NewReader(data)
		} else {
			bodyReader = bytes.NewReader(nil)
		}

		req := httptest.NewRequest(c.Input.Method, c.Input.Path, bodyReader)
		if c.Input.Body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, c.ExpectedOutput.StatusCode, w.Code, "status code mismatch for case %s", c.CaseID)
		if c.ExpectedOutput.Body != nil {
			var actualBody any
			err := json.Unmarshal(w.Body.Bytes(), &actualBody)
			require.NoError(t, err, "response body must be valid JSON for case %s", c.CaseID)

			expectedJSON, _ := json.Marshal(c.ExpectedOutput.Body)
			actualJSON, _ := json.Marshal(actualBody)
			assert.JSONEq(t, string(expectedJSON), string(actualJSON), "body mismatch for case %s", c.CaseID)
		}
	}
}
