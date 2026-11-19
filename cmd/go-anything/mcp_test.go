package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/mohammedrefat/go-anything/internal/mcp"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_MCPCommand_StdioServer(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-cli-mcp-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	_, err = state.InitializeWorkspace(tempDir, "cli-mcp-test", "go")
	require.NoError(t, err)

	// Prepare requests: initialize -> tools/list -> tools/call (status) -> tools/call (approve forbidden)
	initReq := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"}
	listReq := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list"}

	statusArgs, _ := json.Marshal(map[string]any{"dir": tempDir})
	statusParams, _ := json.Marshal(mcp.ToolCallParams{Name: "status", Arguments: statusArgs})
	statusReq := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 3, Method: "tools/call", Params: statusParams}

	approveParams, _ := json.Marshal(mcp.ToolCallParams{Name: "approve", Arguments: json.RawMessage(`{}`)})
	approveReq := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 4, Method: "tools/call", Params: approveParams}

	var stdin bytes.Buffer
	for _, req := range []mcp.JSONRPCRequest{initReq, listReq, statusReq, approveReq} {
		data, err := json.Marshal(req)
		require.NoError(t, err)
		stdin.Write(data)
		stdin.WriteString("\n")
	}

	var stdout, stderr bytes.Buffer
	err = runCLIWithIO(&stdin, &stdout, &stderr, []string{"mcp"})
	require.NoError(t, err, "stderr: %s", stderr.String())

	lines := bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n"))
	require.Len(t, lines, 4, "expected 4 JSON-RPC responses")

	// 1. initialize response
	var initResp mcp.JSONRPCResponse
	require.NoError(t, json.Unmarshal(lines[0], &initResp))
	assert.Nil(t, initResp.Error)
	assert.Equal(t, float64(1), initResp.ID)

	// 2. tools/list response
	var listResp mcp.JSONRPCResponse
	require.NoError(t, json.Unmarshal(lines[1], &listResp))
	assert.Nil(t, listResp.Error)
	listMap, ok := listResp.Result.(map[string]any)
	require.True(t, ok)
	toolsList, ok := listMap["tools"].([]any)
	require.True(t, ok)
	assert.Len(t, toolsList, 8, "CLI MCP server must advertise exactly 8 tools")

	// 3. status response
	var statusResp mcp.JSONRPCResponse
	require.NoError(t, json.Unmarshal(lines[2], &statusResp))
	assert.Nil(t, statusResp.Error)
	resBytes, _ := json.Marshal(statusResp.Result)
	var toolRes mcp.ToolCallResult
	require.NoError(t, json.Unmarshal(resBytes, &toolRes))
	assert.False(t, toolRes.IsError)
	assert.Contains(t, toolRes.Content[0].Text, "cli-mcp-test")

	// 4. approve response (forbidden)
	var approveResp mcp.JSONRPCResponse
	require.NoError(t, json.Unmarshal(lines[3], &approveResp))
	assert.Nil(t, approveResp.Error)
	resBytes, _ = json.Marshal(approveResp.Result)
	var approveToolRes mcp.ToolCallResult
	require.NoError(t, json.Unmarshal(resBytes, &approveToolRes))
	assert.True(t, approveToolRes.IsError)
	assert.Contains(t, approveToolRes.Content[0].Text, "FORBIDDEN")
	assert.Contains(t, approveToolRes.Content[0].Text, "Execution Rule 4")
}
