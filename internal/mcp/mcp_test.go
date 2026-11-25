package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/gate"
	"github.com/mohammedrefaat/go-anything/internal/mcp"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestMCPWorkspace(t *testing.T) (string, *state.Store) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "ga-mcp-test-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})

	store, err := state.InitializeWorkspace(tempDir, "test-mcp-service", "go")
	require.NoError(t, err)

	// Minimal go.mod and source file for toolchain checks (go build, go vet)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module example.com/mcp-test\n\ngo 1.22\n"), 0644))
	pkgDir := filepath.Join(tempDir, "pkg", "auth")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "auth.go"), []byte("package auth\n\n// Login authenticates user.\nfunc Login() bool {\n\treturn true\n}\n"), 0644))

	unit := &adapterkit.Unit{
		ID:              "pkg/auth",
		Name:            "Authentication Service",
		Wave:            0,
		State:           adapterkit.StatePlanned,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(unit))

	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = tempDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	return tempDir, store
}

func sendMCPRequest(t *testing.T, s *mcp.Server, req *mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	t.Helper()
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	var in bytes.Buffer
	in.Write(reqBytes)
	in.WriteString("\n")

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = s.Serve(ctx, &in, &out)
	require.NoError(t, err)

	var resp mcp.JSONRPCResponse
	err = json.Unmarshal(out.Bytes(), &resp)
	require.NoError(t, err, "raw response: %s", out.String())
	return &resp
}

func TestMCP_ToolListExact8(t *testing.T) {
	server := mcp.NewServer()

	req := &mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := sendMCPRequest(t, server, req)
	assert.Nil(t, resp.Error)

	resultMap, ok := resp.Result.(map[string]any)
	require.True(t, ok)

	rawTools, ok := resultMap["tools"].([]any)
	require.True(t, ok)

	assert.Len(t, rawTools, 14, "MCP server must expose exactly 14 tools")

	var toolNames []string
	for _, raw := range rawTools {
		toolMap := raw.(map[string]any)
		toolNames = append(toolNames, toolMap["name"].(string))
	}
	sort.Strings(toolNames)

	expectedTools := []string{
		"analyze",
		"bridge",
		"claim_unit",
		"fuzz_bench",
		"handoff",
		"hotpath",
		"inspect_unit",
		"mark_verified",
		"profile",
		"record_cases",
		"run_gate",
		"shadow_status",
		"status",
		"verify_diff",
	}
	assert.Equal(t, expectedTools, toolNames)
}

func TestMCPDoesNotExposeHumanOnlyTools(t *testing.T) {
	humanOnlyForbidden := []string{
		"approve",
		"promote",
		"lift",
		"cutover",
		"cutover_kill",
		"kill_switch",
		"routing",
		"toggle_shadow",
	}

	defs := mcp.GetToolDefinitions()
	for _, tool := range defs {
		for _, forbidden := range humanOnlyForbidden {
			assert.NotEqual(t, forbidden, tool.Name, "MCP tool %q violates Rule R-1 (human-only command exposed via MCP)", tool.Name)
			assert.False(t, strings.HasPrefix(tool.Name, forbidden), "MCP tool %q starts with forbidden prefix %q", tool.Name, forbidden)
		}
	}

	for _, toolName := range mcp.StandardToolNames {
		for _, forbidden := range humanOnlyForbidden {
			assert.NotEqual(t, forbidden, toolName, "StandardToolNames contains forbidden human-only tool %q", toolName)
		}
	}
}

func TestMCP_Rule4_HumanOnlyControlsForbidden(t *testing.T) {
	server := mcp.NewServer()

	forbiddenActions := []string{"approve", "promote", "lift", "routing", "toggle_shadow", "kill_switch"}

	for _, action := range forbiddenActions {
		t.Run("forbidden_"+action, func(t *testing.T) {
			params, err := json.Marshal(mcp.ToolCallParams{
				Name:      action,
				Arguments: json.RawMessage(`{}`),
			})
			require.NoError(t, err)

			req := &mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      42,
				Method:  "tools/call",
				Params:  params,
			}

			resp := sendMCPRequest(t, server, req)
			assert.Nil(t, resp.Error)

			resBytes, err := json.Marshal(resp.Result)
			require.NoError(t, err)

			var toolRes mcp.ToolCallResult
			require.NoError(t, json.Unmarshal(resBytes, &toolRes))

			assert.True(t, toolRes.IsError)
			require.NotEmpty(t, toolRes.Content)
			assert.Contains(t, toolRes.Content[0].Text, "FORBIDDEN")
			assert.Contains(t, toolRes.Content[0].Text, "Execution Rule 4")
		})
	}
}

func TestMCP_MarkVerified_RequiresCommitTiedEvidence(t *testing.T) {
	tempDir, store := setupTestMCPWorkspace(t)
	server := mcp.NewServer()

	// Put unit into In progress state
	_, err := store.Transition("pkg/auth", adapterkit.StateRecorded, state.TransitionOptions{})
	require.NoError(t, err)
	_, err = store.Transition("pkg/auth", adapterkit.StateReady, state.TransitionOptions{})
	require.NoError(t, err)
	_, err = store.Transition("pkg/auth", adapterkit.StateInProgress, state.TransitionOptions{
		Actor: "agent-alice",
	})
	require.NoError(t, err)

	commitSHA := "1234567890abcdef1234567890abcdef12345678"

	t.Run("rejected without evidence", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"unit_id":    "pkg/auth",
			"commit_sha": commitSHA,
			"actor":      "agent-alice",
			"dir":        tempDir,
		})
		params, _ := json.Marshal(mcp.ToolCallParams{
			Name:      "mark_verified",
			Arguments: args,
		})
		req := &mcp.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      10,
			Method:  "tools/call",
			Params:  params,
		}

		resp := sendMCPRequest(t, server, req)
		resBytes, _ := json.Marshal(resp.Result)
		var toolRes mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &toolRes)

		assert.True(t, toolRes.IsError)
		assert.Contains(t, toolRes.Content[0].Text, "TRANSITION REJECTED")
		assert.Contains(t, toolRes.Content[0].Text, "no evidence exists for commit SHA")
	})

	t.Run("succeeds with valid commit-tied evidence", func(t *testing.T) {
		// Save valid evidence
		validEvidence := &adapterkit.Evidence{
			EvidenceID: fmt.Sprintf("evi-auth-%s", commitSHA[:8]),
			UnitID:     "pkg/auth",
			CommitSHA:  commitSHA,
			Profile:    adapterkit.ProfileFull,
			VerifiedAt: time.Now().UTC().Format(time.RFC3339),
			VerifiedBy: "agent-alice",
			Checks: []adapterkit.GateCheck{
				{Name: gate.CheckBuild, Status: adapterkit.StatusPass},
				{Name: gate.CheckFormat, Status: adapterkit.StatusPass},
				{Name: gate.CheckVet, Status: adapterkit.StatusPass},
				{Name: gate.CheckRace, Status: adapterkit.StatusPass},
				{Name: gate.CheckGovulncheck, Status: adapterkit.StatusPass},
				{Name: gate.CheckReplay, Status: adapterkit.StatusPass},
				{Name: gate.CheckErrorPaths, Status: adapterkit.StatusPass},
				{Name: gate.CheckAPICompat, Status: adapterkit.StatusPass},
			},
			MismatchesCount: 0,
		}
		require.NoError(t, store.SaveEvidence(validEvidence))

		args, _ := json.Marshal(map[string]any{
			"unit_id":    "pkg/auth",
			"commit_sha": commitSHA,
			"actor":      "agent-alice",
			"dir":        tempDir,
		})
		params, _ := json.Marshal(mcp.ToolCallParams{
			Name:      "mark_verified",
			Arguments: args,
		})
		req := &mcp.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      11,
			Method:  "tools/call",
			Params:  params,
		}

		resp := sendMCPRequest(t, server, req)
		resBytes, _ := json.Marshal(resp.Result)
		var toolRes mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &toolRes)

		assert.False(t, toolRes.IsError)
		assert.Contains(t, toolRes.Content[0].Text, "SUCCESS: Unit \"pkg/auth\" transitioned to Verified.")
		assert.Contains(t, toolRes.Content[0].Text, commitSHA)

		u, err := store.GetUnit("pkg/auth")
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateVerified, u.State)
	})
}

func TestMCP_All8Tools_EndToEnd(t *testing.T) {
	tempDir, store := setupTestMCPWorkspace(t)
	server := mcp.NewServer()
	server.AllowNetwork = true
	server.WorkspaceRoot = tempDir

	// 1. status
	t.Run("tool_1_status", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"dir": tempDir})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "status", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "Project: test-mcp-service")
		assert.Contains(t, res.Content[0].Text, "Next Unit to Migrate: pkg/auth")
	})

	// Setup mock HTTP server for recording and replay
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"authorized","token":"tok-abc"}`))
	}))
	defer mockServer.Close()

	// 2. record_cases
	t.Run("tool_2_record_cases", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"unit_id": "pkg/auth",
			"url":     mockServer.URL + "/auth",
			"dir":     tempDir,
		})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "record_cases", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		require.False(t, res.IsError, "error: %v", res.Content)
		assert.Contains(t, res.Content[0].Text, "RECORDED")
		assert.Contains(t, res.Content[0].Text, "pkg/auth.jsonl")

		// Verify state is Recorded
		u, err := store.GetUnit("pkg/auth")
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateRecorded, u.State)
	})

	// 3. inspect_unit
	t.Run("tool_3_inspect_unit", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"unit_id": "pkg/auth", "dir": tempDir})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "inspect_unit", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 3, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "=== Unit Inspection: pkg/auth ===")
		assert.Contains(t, res.Content[0].Text, "Recorded Cases (1 total)")
	})

	// 4. claim_unit
	t.Run("tool_4_claim_unit", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"unit_id":     "pkg/auth",
			"actor":       "agent-tester",
			"ttl_seconds": 1800,
			"dir":         tempDir,
		})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "claim_unit", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 4, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "CLAIMED: Unit \"pkg/auth\" is now 'In progress'.")
		assert.Contains(t, res.Content[0].Text, "Owner: agent-tester")

		u, err := store.GetUnit("pkg/auth")
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateInProgress, u.State)
	})

	// 5. verify_diff
	t.Run("tool_5_verify_diff", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"unit_id": "pkg/auth",
			"target":  mockServer.URL,
			"dir":     tempDir,
		})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "verify_diff", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 5, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "PASS: 100% replay matched")
	})

	// 6. run_gate
	t.Run("tool_6_run_gate", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"unit_id":             "pkg/auth",
			"allow_missing_tools": true,
			"dir":                 tempDir,
		})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "run_gate", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 6, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		require.False(t, res.IsError, "tool_6_run_gate output: %s", res.Content[0].Text)
		assert.Contains(t, res.Content[0].Text, "Quality Gate Report")
		assert.Contains(t, res.Content[0].Text, "Commit SHA:")
	})

	// 7. mark_verified
	t.Run("tool_7_mark_verified", func(t *testing.T) {
		// Provide valid commit evidence first
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = tempDir
		sha, err := cmd.Output()
		require.NoError(t, err)
		validSHA := strings.TrimSpace(string(sha))
		ev := &adapterkit.Evidence{
			EvidenceID: "evi-auth-test-verified",
			UnitID:     "pkg/auth",
			CommitSHA:  validSHA,
			Profile:    adapterkit.ProfileFull,
			VerifiedAt: time.Now().UTC().Format(time.RFC3339),
			VerifiedBy: "agent-tester",
			Checks: []adapterkit.GateCheck{
				{Name: gate.CheckBuild, Status: adapterkit.StatusPass},
				{Name: gate.CheckFormat, Status: adapterkit.StatusPass},
				{Name: gate.CheckVet, Status: adapterkit.StatusPass},
				{Name: gate.CheckRace, Status: adapterkit.StatusPass},
				{Name: gate.CheckGovulncheck, Status: adapterkit.StatusPass},
				{Name: gate.CheckReplay, Status: adapterkit.StatusPass},
				{Name: gate.CheckErrorPaths, Status: adapterkit.StatusPass},
				{Name: gate.CheckAPICompat, Status: adapterkit.StatusPass},
			},
			MismatchesCount: 0,
		}
		require.NoError(t, store.SaveEvidence(ev))

		args, _ := json.Marshal(map[string]any{
			"unit_id":    "pkg/auth",
			"commit_sha": validSHA,
			"actor":      "agent-tester",
			"dir":        tempDir,
		})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "mark_verified", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 7, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "SUCCESS: Unit \"pkg/auth\" transitioned to Verified.")

		u, err := store.GetUnit("pkg/auth")
		require.NoError(t, err)
		assert.Equal(t, adapterkit.StateVerified, u.State)
	})

	// 8. handoff
	t.Run("tool_8_handoff", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"summary":    "Migrated pkg/auth, recorded cases, replayed with 0 mismatches and verified.",
			"next_steps": "Migrate next wave unit pkg/billing.",
			"dir":        tempDir,
		})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "handoff", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 8, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "HANDOFF WRITTEN")

		handoffFile := filepath.Join(tempDir, "HANDOFF.md")
		assert.FileExists(t, handoffFile)
		data, err := os.ReadFile(handoffFile)
		require.NoError(t, err)
		content := string(data)
		assert.Contains(t, content, "# HANDOFF.md — Migration Session Handoff")
		assert.Contains(t, content, "Migrated pkg/auth")
		assert.Contains(t, content, "Migrate next wave unit pkg/billing")
	})
}

func TestMCP_Protocol_InitializeAndPing(t *testing.T) {
	server := mcp.NewServer()

	// Initialize
	initReq := &mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}
	initResp := sendMCPRequest(t, server, initReq)
	assert.Nil(t, initResp.Error)
	resMap := initResp.Result.(map[string]any)
	assert.Equal(t, "2024-11-05", resMap["protocolVersion"])

	// Ping
	pingReq := &mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "ping",
	}
	pingResp := sendMCPRequest(t, server, pingReq)
	assert.Nil(t, pingResp.Error)
}

func TestMCP_NewV2Tools_EndToEnd(t *testing.T) {
	tempDir, _ := setupTestMCPWorkspace(t)
	server := mcp.NewServer()
	server.WorkspaceRoot = tempDir

	// 1. analyze
	t.Run("tool_analyze", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"dir": tempDir})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "analyze", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 10, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "total_units")
	})

	// 2. profile
	t.Run("tool_profile", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"unit_id": "pkg/auth", "dir": tempDir, "top": 3})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "profile", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 11, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "candidates")
	})

	// 3. bridge
	t.Run("tool_bridge", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"unit_id": "pkg/auth", "function": "login", "dir": tempDir})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "bridge", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 12, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "generated_files")
	})

	// 4. hotpath
	t.Run("tool_hotpath", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"unit_id": "pkg/auth", "function": "login", "dir": tempDir})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "hotpath", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 13, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "paused_for_implementation")
		assert.Contains(t, res.Content[0].Text, "human_action_required")
	})

	// 5. fuzz_bench
	t.Run("tool_fuzz_bench", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"unit_id": "pkg/auth", "mode": "fuzz", "time": "1s", "dir": tempDir})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "fuzz_bench", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 14, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "fuzz")

		benchArgs, _ := json.Marshal(map[string]any{"unit_id": "pkg/auth", "mode": "bench", "iterations": 5, "dir": tempDir})
		benchParams, _ := json.Marshal(mcp.ToolCallParams{Name: "fuzz_bench", Arguments: benchArgs})
		benchResp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 15, Method: "tools/call", Params: benchParams})

		benchBytes, _ := json.Marshal(benchResp.Result)
		var benchRes mcp.ToolCallResult
		_ = json.Unmarshal(benchBytes, &benchRes)
		assert.False(t, benchRes.IsError)
		assert.Contains(t, benchRes.Content[0].Text, "bench")
	})

	// 6. shadow_status
	t.Run("tool_shadow_status", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"unit_id": "pkg/auth", "dir": tempDir})
		params, _ := json.Marshal(mcp.ToolCallParams{Name: "shadow_status", Arguments: args})
		resp := sendMCPRequest(t, server, &mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 16, Method: "tools/call", Params: params})

		resBytes, _ := json.Marshal(resp.Result)
		var res mcp.ToolCallResult
		_ = json.Unmarshal(resBytes, &res)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content[0].Text, "shadow_active")
	})
}
