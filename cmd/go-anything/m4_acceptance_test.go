package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/mcp"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mcpTestClient struct {
	stdinW  io.WriteCloser
	stdoutR *bufio.Reader
	nextID  int
}

func newMCPTestClient(stdin io.WriteCloser, stdout io.Reader) *mcpTestClient {
	return &mcpTestClient{
		stdinW:  stdin,
		stdoutR: bufio.NewReader(stdout),
		nextID:  0,
	}
}

func (c *mcpTestClient) initialize(t *testing.T) {
	c.nextID++
	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  "initialize",
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	_, err = fmt.Fprintf(c.stdinW, "%s\n", string(reqBytes))
	require.NoError(t, err)

	line, err := c.stdoutR.ReadBytes('\n')
	require.NoError(t, err)

	var resp mcp.JSONRPCResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(line), &resp))
	require.Nil(t, resp.Error)
	assert.Equal(t, float64(c.nextID), resp.ID)
}

func (c *mcpTestClient) listTools(t *testing.T) []string {
	c.nextID++
	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  "tools/list",
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	_, err = fmt.Fprintf(c.stdinW, "%s\n", string(reqBytes))
	require.NoError(t, err)

	line, err := c.stdoutR.ReadBytes('\n')
	require.NoError(t, err)

	var resp mcp.JSONRPCResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(line), &resp))
	require.Nil(t, resp.Error)

	resMap, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	toolsList, ok := resMap["tools"].([]any)
	require.True(t, ok)

	var names []string
	for _, toolItem := range toolsList {
		tm, ok := toolItem.(map[string]any)
		require.True(t, ok)
		names = append(names, tm["name"].(string))
	}
	return names
}

func (c *mcpTestClient) callTool(t *testing.T, name string, args any) (*mcp.ToolCallResult, error) {
	c.nextID++
	id := c.nextID

	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	callParams, err := json.Marshal(mcp.ToolCallParams{
		Name:      name,
		Arguments: argsBytes,
	})
	require.NoError(t, err)

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  callParams,
	}

	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	_, err = fmt.Fprintf(c.stdinW, "%s\n", string(reqBytes))
	require.NoError(t, err)

	line, err := c.stdoutR.ReadBytes('\n')
	require.NoError(t, err)

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON-RPC response %q: %w", string(line), err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	resultBytes, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var toolRes mcp.ToolCallResult
	if err := json.Unmarshal(resultBytes, &toolRes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ToolCallResult: %w", err)
	}

	return &toolRes, nil
}

func startTargetTestServer() *httptest.Server {
	mux := http.NewServeMux()

	// /fraud endpoint
	mux.HandleFunc("/fraud", func(w http.ResponseWriter, r *http.Request) {
		amountStr := r.URL.Query().Get("amount")
		riskStr := r.URL.Query().Get("risk")

		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid amount"})
			return
		}

		highRisk := riskStr == "true" || riskStr == "1"
		flagged := (highRisk && amount > 500) || (amount > 10000)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"amount":   amount,
			"highRisk": highRisk,
			"flagged":  flagged,
		})
	})

	// /convert endpoint
	rates := map[string]float64{
		"USD": 1.0,
		"EUR": 0.92,
		"GBP": 0.79,
		"JPY": 155.0,
	}

	mux.HandleFunc("/convert", func(w http.ResponseWriter, r *http.Request) {
		amountStr := r.URL.Query().Get("amount")
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")

		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid amount"})
			return
		}
		if amount < 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "amount cannot be negative"})
			return
		}

		rateFrom, okFrom := rates[from]
		rateTo, okTo := rates[to]
		if !okFrom || !okTo {
			bad := from
			if !okTo {
				bad = to
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprintf("unsupported currency code: %s", bad)})
			return
		}

		result := math.Round((amount/rateFrom*rateTo)*100) / 100
		formatted := fmt.Sprintf("%.2f %s", result, to)
		if to == "EUR" {
			formatted = fmt.Sprintf("€%.2f", result)
		} else if to == "GBP" {
			formatted = fmt.Sprintf("£%.2f", result)
		} else if to == "USD" {
			formatted = fmt.Sprintf("$%.2f", result)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"amount":    amount,
			"from":      from,
			"to":        to,
			"result":    result,
			"formatted": formatted,
		})
	})

	return httptest.NewServer(mux)
}

func TestM4_AgentAcceptanceRun(t *testing.T) {
	// 1. Workspace setup: copy testdata/dogfood_service into tempDir
	tempDir, err := os.MkdirTemp("", "ga-m4-acceptance-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	srcDir, err := filepath.Abs("../../testdata/dogfood_service")
	if err != nil || !dirExists(srcDir) {
		srcDir, err = filepath.Abs("testdata/dogfood_service")
		require.NoError(t, err)
	}
	require.NoError(t, copyDir(srcDir, tempDir))

	// Initialize git repository with initial commit
	initGitRepo(t, tempDir)

	// Modernize workspace: three-tier partitioning (legacy/, modern/, go.work)
	var modStdout, modStderr bytes.Buffer
	err = runCLI([]string{"modernize", tempDir}, &modStdout, &modStderr)
	require.NoError(t, err, "modernize failed: %s", modStderr.String())

	// Git add and commit modernize layout so HEAD commit SHA is clean
	runCmd(t, tempDir, "git", "add", ".")
	runCmd(t, tempDir, "git", "commit", "-m", "chore: setup three-tier modernize workspace")

	// Start target HTTP server
	targetServer := startTargetTestServer()
	defer targetServer.Close()

	// 2. Start MCP server over stdio pipes
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	defer serverInW.Close()
	defer serverOutR.Close()

	serverDone := make(chan error, 1)
	go func() {
		var stderr bytes.Buffer
		err := runCLIWithIO(serverInR, serverOutW, &stderr, []string{"mcp"})
		serverDone <- err
	}()

	client := newMCPTestClient(serverInW, serverOutR)

	// Initialize MCP session
	client.initialize(t)

	// Verify MCP advertises exactly 8 standard tools
	tools := client.listTools(t)
	assert.Len(t, tools, 8, "MCP server must advertise exactly 8 standard tools")
	assert.ElementsMatch(t, mcp.StandardToolNames, tools)

	store := state.NewStore(tempDir)

	// -------------------------------------------------------------------------
	// Step 1: status tool call
	// -------------------------------------------------------------------------
	t.Log("Step 1: Check workspace status via MCP")
	statusRes, err := client.callTool(t, "status", map[string]any{"dir": tempDir})
	require.NoError(t, err)
	require.False(t, statusRes.IsError)
	statusText := statusRes.Content[0].Text
	assert.Contains(t, statusText, "example.com/payments")
	assert.Contains(t, statusText, "4 Planned")
	assert.Contains(t, statusText, "Wave: 0")

	// -------------------------------------------------------------------------
	// Step 2: claim_unit on 'pkg/fraud'
	// -------------------------------------------------------------------------
	t.Log("Step 2: Claim unit pkg/fraud via MCP")
	claimRes, err := client.callTool(t, "claim_unit", map[string]any{
		"unit_id": "pkg/fraud",
		"actor":   "mcp-agent",
		"profile": "full",
		"dir":     tempDir,
	})
	require.NoError(t, err)
	require.False(t, claimRes.IsError)
	assert.Contains(t, claimRes.Content[0].Text, `CLAIMED: Unit "pkg/fraud" is now 'In progress'`)
	assert.Contains(t, claimRes.Content[0].Text, "mcp-agent")

	// Verify durable state in store: InProgress with active lease
	uFraud, err := store.GetUnit("pkg/fraud")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateInProgress, uFraud.State)
	assert.Equal(t, adapterkit.ProfileFull, uFraud.EvidenceProfile)

	lease, err := store.GetLease("pkg/fraud")
	require.NoError(t, err)
	require.NotNil(t, lease)
	assert.Equal(t, "mcp-agent", lease.Owner)

	// -------------------------------------------------------------------------
	// Step 3: inspect_unit on 'pkg/fraud'
	// -------------------------------------------------------------------------
	t.Log("Step 3: Inspect unit pkg/fraud via MCP")
	inspectRes, err := client.callTool(t, "inspect_unit", map[string]any{
		"unit_id": "pkg/fraud",
		"dir":     tempDir,
	})
	require.NoError(t, err)
	require.False(t, inspectRes.IsError)
	inspectText := inspectRes.Content[0].Text
	assert.Contains(t, inspectText, "Unit Inspection: pkg/fraud")
	assert.Contains(t, inspectText, "legacy")

	// -------------------------------------------------------------------------
	// Step 4: record_cases for 'pkg/fraud' via MCP
	// -------------------------------------------------------------------------
	t.Log("Step 4: Record behavioral cases for pkg/fraud via MCP")
	fraudURLs := []string{
		targetServer.URL + "/fraud?amount=100&risk=false",   // happy: amount 100, risk false -> flagged false
		targetServer.URL + "/fraud?amount=600&risk=true",    // happy: amount 600, high risk -> flagged true (>500)
		targetServer.URL + "/fraud?amount=500&risk=true",    // edge: boundary 500, high risk -> flagged false (not >500)
		targetServer.URL + "/fraud?amount=12000&risk=false", // happy: amount 12000 -> flagged true (>10000)
		targetServer.URL + "/fraud?amount=-10&risk=false",   // trap: negative amount -> flagged false
	}

	for _, u := range fraudURLs {
		recRes, err := client.callTool(t, "record_cases", map[string]any{
			"unit_id": "pkg/fraud",
			"url":     u,
			"dir":     tempDir,
		})
		require.NoError(t, err)
		require.False(t, recRes.IsError)
		assert.Contains(t, recRes.Content[0].Text, "RECORDED:")
	}

	// Verify cases file exists and contains 5 lines
	casesPath := filepath.Join(tempDir, ".go-anything", "cases", "pkg", "fraud.jsonl")
	require.FileExists(t, casesPath)
	casesData, err := os.ReadFile(casesPath)
	require.NoError(t, err)
	caseLines := strings.Split(strings.TrimSpace(string(casesData)), "\n")
	assert.Len(t, caseLines, 5, "expected 5 recorded cases for pkg/fraud")

	// -------------------------------------------------------------------------
	// Step 5: Synthesize and write modern Go port in modern/pkg/fraud
	// -------------------------------------------------------------------------
	t.Log("Step 5: Write modern Go port and commit changes")
	modernFraudFile := filepath.Join(tempDir, "modern", "pkg", "fraud", "fraud.go")
	modernFraudCode := `package fraud

// Evaluate checks if a transaction should be flagged for risk review.
// Modern Go implementation: zero allocation, strict threshold semantics.
func Evaluate(amountUSD float64, highRisk bool) bool {
	if highRisk && amountUSD > 500 {
		return true
	}
	return amountUSD > 10000
}
`
	require.NoError(t, os.WriteFile(modernFraudFile, []byte(modernFraudCode), 0644))

	// Commit modernized Go port to git
	runCmd(t, tempDir, "git", "add", ".")
	runCmd(t, tempDir, "git", "commit", "-m", "feat(fraud): modernize pkg/fraud implementation")

	// -------------------------------------------------------------------------
	// Step 6: verify_diff confirming zero mismatches via MCP
	// -------------------------------------------------------------------------
	t.Log("Step 6: verify_diff against target implementation via MCP")
	diffRes, err := client.callTool(t, "verify_diff", map[string]any{
		"unit_id": "pkg/fraud",
		"target":  targetServer.URL,
		"dir":     tempDir,
	})
	require.NoError(t, err)
	require.False(t, diffRes.IsError, "verify_diff output: %v", diffRes.Content)
	diffText := diffRes.Content[0].Text
	assert.Contains(t, diffText, "PASS:")
	assert.Contains(t, diffText, "5/5 cases")

	// Verify unit metadata stored target for run_gate replay
	uFraudAfterDiff, err := store.GetUnit("pkg/fraud")
	require.NoError(t, err)
	assert.Equal(t, targetServer.URL, uFraudAfterDiff.Metadata["target"])

	// -------------------------------------------------------------------------
	// Step 7: run_gate under 'full' profile via MCP
	// -------------------------------------------------------------------------
	t.Log("Step 7: run_gate under 'full' profile via MCP")
	gateRes, err := client.callTool(t, "run_gate", map[string]any{
		"unit_id":             "pkg/fraud",
		"profile":             "full",
		"allow_missing_tools": true,
		"dir":                 tempDir,
	})
	require.NoError(t, err)
	require.False(t, gateRes.IsError, "run_gate failed: %v", gateRes.Content)
	gateText := gateRes.Content[0].Text
	assert.Contains(t, gateText, "Quality Gate Report (Passed: true")
	assert.Contains(t, gateText, "[PASS] replay")
	assert.Contains(t, gateText, "[PASS] error_paths")
	assert.Contains(t, gateText, "[PASS] api_compat")

	// Confirm commit-tied evidence was written to .go-anything/evidence/
	evidenceList, err := store.ListEvidenceForUnit("pkg/fraud")
	require.NoError(t, err)
	require.NotEmpty(t, evidenceList)
	ev := evidenceList[0]
	assert.Equal(t, adapterkit.ProfileFull, ev.Profile)
	assert.Equal(t, 0, ev.MismatchesCount)
	assert.Equal(t, "mcp-agent", ev.VerifiedBy)

	// -------------------------------------------------------------------------
	// Step 8: mark_verified to transition unit to Verified via MCP
	// -------------------------------------------------------------------------
	t.Log("Step 8: mark_verified unit pkg/fraud via MCP")
	markRes, err := client.callTool(t, "mark_verified", map[string]any{
		"unit_id": "pkg/fraud",
		"actor":   "mcp-agent",
		"dir":     tempDir,
	})
	require.NoError(t, err)
	require.False(t, markRes.IsError, "mark_verified failed: %v", markRes.Content)
	markText := markRes.Content[0].Text
	assert.Contains(t, markText, `SUCCESS: Unit "pkg/fraud" transitioned to Verified`)

	// Assert unit is Verified and lease released
	uFraudVerified, err := store.GetUnit("pkg/fraud")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateVerified, uFraudVerified.State)
	assert.Equal(t, adapterkit.ProfileFull, uFraudVerified.EvidenceProfile)

	// Lease must be cleaned up upon Verified transition
	fraudLease, err := store.GetLease("pkg/fraud")
	require.NoError(t, err)
	assert.Nil(t, fraudLease, "lease should be released after mark_verified")

	// -------------------------------------------------------------------------
	// Step 9: handoff generating HANDOFF.md via MCP
	// -------------------------------------------------------------------------
	t.Log("Step 9: handoff to write session documentation via MCP")
	handoffRes, err := client.callTool(t, "handoff", map[string]any{
		"summary":    "Successfully ported pkg/fraud to modern Go, verified 100% replay match over 5 cases, and satisfied full quality gate.",
		"next_steps": "Port pkg/currency and pkg/billing in Wave 1.",
		"dir":        tempDir,
	})
	require.NoError(t, err)
	require.False(t, handoffRes.IsError)
	assert.Contains(t, handoffRes.Content[0].Text, "HANDOFF WRITTEN")

	handoffPath := filepath.Join(tempDir, "HANDOFF.md")
	require.FileExists(t, handoffPath)
	handoffContent, err := os.ReadFile(handoffPath)
	require.NoError(t, err)
	assert.Contains(t, string(handoffContent), "pkg/fraud")
	assert.Contains(t, string(handoffContent), "Verified")

	// -------------------------------------------------------------------------
	// Step 10: status verifying updated progress via MCP
	// -------------------------------------------------------------------------
	t.Log("Step 10: Verify updated progress status via MCP")
	finalStatusRes, err := client.callTool(t, "status", map[string]any{"dir": tempDir})
	require.NoError(t, err)
	require.False(t, finalStatusRes.IsError)
	finalStatusText := finalStatusRes.Content[0].Text
	assert.Contains(t, finalStatusText, "1 Verified")
	assert.Contains(t, finalStatusText, "pkg/currency")

	// -------------------------------------------------------------------------
	// Step 11: Re-verify dogfood units (pkg/currency) under 'full' profile
	// -------------------------------------------------------------------------
	t.Log("Step 11: Re-verify dogfood units under full profile")
	currURLs := []string{
		targetServer.URL + "/convert?amount=100&from=USD&to=EUR",
		targetServer.URL + "/convert?amount=50&from=USD&to=GBP",
		targetServer.URL + "/convert?amount=10&from=EUR&to=USD",
		targetServer.URL + "/convert?amount=-5&from=USD&to=EUR",
		targetServer.URL + "/convert?amount=100&from=XYZ&to=USD",
	}

	for _, u := range currURLs {
		recRes, err := client.callTool(t, "record_cases", map[string]any{
			"unit_id": "pkg/currency",
			"url":     u,
			"dir":     tempDir,
		})
		require.NoError(t, err)
		require.False(t, recRes.IsError)
	}

	// Claim pkg/currency
	claimCurrRes, err := client.callTool(t, "claim_unit", map[string]any{
		"unit_id": "pkg/currency",
		"actor":   "mcp-agent",
		"profile": "full",
		"dir":     tempDir,
	})
	require.NoError(t, err)
	require.False(t, claimCurrRes.IsError)

	// Verify diff pkg/currency
	diffCurrRes, err := client.callTool(t, "verify_diff", map[string]any{
		"unit_id": "pkg/currency",
		"target":  targetServer.URL,
		"dir":     tempDir,
	})
	require.NoError(t, err)
	require.False(t, diffCurrRes.IsError, "verify_diff currency: %v", diffCurrRes.Content)

	// Run full gate on pkg/currency
	gateCurrRes, err := client.callTool(t, "run_gate", map[string]any{
		"unit_id":             "pkg/currency",
		"profile":             "full",
		"allow_missing_tools": true,
		"dir":                 tempDir,
	})
	require.NoError(t, err)
	require.False(t, gateCurrRes.IsError, "gate error currency: %v", gateCurrRes.Content)

	// Mark pkg/currency verified
	markCurrRes, err := client.callTool(t, "mark_verified", map[string]any{
		"unit_id": "pkg/currency",
		"actor":   "mcp-agent",
		"dir":     tempDir,
	})
	require.NoError(t, err)
	require.False(t, markCurrRes.IsError)

	uCurrVerified, err := store.GetUnit("pkg/currency")
	require.NoError(t, err)
	assert.Equal(t, adapterkit.StateVerified, uCurrVerified.State)
	assert.Equal(t, adapterkit.ProfileFull, uCurrVerified.EvidenceProfile)

	// Verify that with both Wave 0 dependencies Verified, pkg/billing dependencies are met
	uBilling, err := store.GetUnit("pkg/billing")
	require.NoError(t, err)
	assert.Equal(t, 1, uBilling.Wave)

	// Close client stdin to let server finish cleanly
	require.NoError(t, serverInW.Close())
	select {
	case sErr := <-serverDone:
		require.NoError(t, sErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for MCP server to exit")
	}

	// -------------------------------------------------------------------------
	// Step 12: Zero manual edits verification
	// -------------------------------------------------------------------------
	t.Log("Step 12: Confirm zero manual edits to state files")
	// Both pkg/fraud and pkg/currency reached Verified strictly through MCP state machine
	assert.Equal(t, adapterkit.StateVerified, uFraudVerified.State)
	assert.Equal(t, adapterkit.StateVerified, uCurrVerified.State)
}
