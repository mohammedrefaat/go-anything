package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	cliadapter "github.com/mohammedrefat/go-anything/adapters/application/cli"
	httpadapter "github.com/mohammedrefat/go-anything/adapters/application/http"
	"github.com/mohammedrefat/go-anything/internal/gate"
	"github.com/mohammedrefat/go-anything/internal/recorder"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// StandardToolNames lists the exact 8 allowed MCP tools.
var StandardToolNames = []string{
	"status",
	"claim_unit",
	"inspect_unit",
	"record_cases",
	"verify_diff",
	"run_gate",
	"mark_verified",
	"handoff",
}

// GetToolDefinitions returns the schema for all 8 standard MCP tools.
func GetToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "status",
			Description: "Show migration progress, next unit in dependency wave order, active leases, and blockers.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir": {Type: "string", Description: "Path to workspace repository root (defaults to current dir)."},
				},
			},
		},
		{
			Name:        "claim_unit",
			Description: "Take a Ready migration unit with a time-bounded local lease in .go-anything/local/.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"unit_id":     {Type: "string", Description: "Identifier of the migration unit to claim."},
					"actor":       {Type: "string", Description: "Agent or worker identifier claiming the unit."},
					"profile":     {Type: "string", Description: "Target verification evidence profile: 'replay' or 'full'."},
					"ttl_seconds": {Type: "number", Description: "Lease TTL duration in seconds (default: 3600)."},
					"dir":         {Type: "string", Description: "Path to workspace repository root."},
				},
				Required: []string{"unit_id"},
			},
		},
		{
			Name:        "inspect_unit",
			Description: "View original source code, sanitized sample cases, detected traps, and constraints for a unit.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"unit_id": {Type: "string", Description: "Identifier of the migration unit to inspect."},
					"dir":     {Type: "string", Description: "Path to workspace repository root."},
				},
				Required: []string{"unit_id"},
			},
		},
		{
			Name:        "record_cases",
			Description: "Run the original application, apply fail-closed redaction, and save sanitized cases into .go-anything/cases/.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"unit_id": {Type: "string", Description: "Identifier of the migration unit."},
					"url":     {Type: "string", Description: "HTTP URL of original service (for HTTP endpoints)."},
					"command": {Type: "string", Description: "CLI command line string (for CLI binaries)."},
					"dir":     {Type: "string", Description: "Path to workspace repository root."},
				},
				Required: []string{"unit_id"},
			},
		},
		{
			Name:        "verify_diff",
			Description: "Replay recorded behavioral cases against the ported Go implementation and return PASS or top sanitized mismatches.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"unit_id": {Type: "string", Description: "Identifier of the migration unit."},
					"target":  {Type: "string", Description: "Target executable binary or HTTP base URL of ported implementation."},
					"mode":    {Type: "string", Description: "Comparator mode: exact, structural_json, or http_contract."},
					"dir":     {Type: "string", Description: "Path to workspace repository root."},
				},
				Required: []string{"unit_id", "target"},
			},
		},
		{
			Name:        "run_gate",
			Description: "Run the full quality gate pipeline (toolchain checks, error-paths, and API compatibility) and generate commit-tied evidence.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"unit_id":             {Type: "string", Description: "Optional migration unit identifier."},
					"allow_missing_tools": {Type: "boolean", Description: "Allow missing optional linters like staticcheck."},
					"profile":             {Type: "string", Description: "Evidence profile: 'replay' or 'full' (default: 'full')."},
					"dir":                 {Type: "string", Description: "Path to workspace repository root."},
				},
			},
		},
		{
			Name:        "mark_verified",
			Description: "Advance unit to Verified state. Strictly rejected unless valid commit-tied evidence exists for the target commit SHA.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"unit_id":    {Type: "string", Description: "Identifier of the migration unit."},
					"commit_sha": {Type: "string", Description: "Optional 40-character git commit SHA (defaults to HEAD)."},
					"actor":      {Type: "string", Description: "Agent identifier executing verification."},
					"dir":        {Type: "string", Description: "Path to workspace repository root."},
				},
				Required: []string{"unit_id"},
			},
		},
		{
			Name:        "handoff",
			Description: "Write HANDOFF.md with workspace state, progress, and next steps for the next session or reviewer.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"summary":    {Type: "string", Description: "Summary of changes made during current session."},
					"next_steps": {Type: "string", Description: "Recommended next tasks or instructions for next session."},
					"dir":        {Type: "string", Description: "Path to workspace repository root."},
				},
				Required: []string{"summary", "next_steps"},
			},
		},
	}
}

func (s *Server) registerStandardTools() {
	s.tools["status"] = s.handleStatus
	s.tools["claim_unit"] = s.handleClaimUnit
	s.tools["inspect_unit"] = s.handleInspectUnit
	s.tools["record_cases"] = s.handleRecordCases
	s.tools["verify_diff"] = s.handleVerifyDiff
	s.tools["run_gate"] = s.handleRunGate
	s.tools["mark_verified"] = s.handleMarkVerified
	s.tools["handoff"] = s.handleHandoff
}

func resolveWorkspaceDir(rawDir string) string {
	if rawDir == "" {
		rawDir = "."
	}
	abs, err := filepath.Abs(rawDir)
	if err != nil {
		return rawDir
	}
	return abs
}

// 1. status
func (s *Server) handleStatus(ctx context.Context, args json.RawMessage) (*ToolCallResult, error) {
	var input struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(args, &input)
	dir := resolveWorkspaceDir(input.Dir)

	store := state.NewStore(dir)
	manifest, err := store.GetManifest()
	if err != nil {
		return nil, fmt.Errorf("failed to read workspace manifest: %w", err)
	}

	units, err := store.ListUnits()
	if err != nil {
		return nil, fmt.Errorf("failed to list units: %w", err)
	}

	// Sort by Wave ascending, then state priority (Ready -> Recorded -> Planned), then ID ascending (Rule 2)
	sort.Slice(units, func(i, j int) bool {
		if units[i].Wave != units[j].Wave {
			return units[i].Wave < units[j].Wave
		}
		pI := statePriorityOrder(units[i].State)
		pJ := statePriorityOrder(units[j].State)
		if pI != pJ {
			return pI < pJ
		}
		return units[i].ID < units[j].ID
	})

	counts := make(map[adapterkit.UnitState]int)
	var activeLeases []string
	var blockers []string
	var nextUnit *adapterkit.Unit

	for _, u := range units {
		counts[u.State]++
		if u.State == adapterkit.StateInProgress {
			lease, _ := store.GetLease(u.ID)
			if lease != nil && lease.IsValid() {
				activeLeases = append(activeLeases, fmt.Sprintf("%s (held by %s, expires %s)", u.ID, lease.Owner, lease.ExpiresAt.Format(time.RFC3339)))
			}
		}
		if u.State == adapterkit.StateBlocked {
			reason := "unspecified reason"
			if r, ok := u.Metadata["blocked_reason"].(string); ok && r != "" {
				reason = r
			}
			blockers = append(blockers, fmt.Sprintf("%s: %s", u.ID, reason))
		}
		if nextUnit == nil && (u.State == adapterkit.StateReady || u.State == adapterkit.StateRecorded || u.State == adapterkit.StatePlanned) {
			nextUnit = u
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Project: %s\n", manifest.ProjectName))
	sb.WriteString(fmt.Sprintf("Summary: %d Completed · %d Cutover · %d Approved · %d Shadow · %d Verified · %d In progress · %d Ready · %d Planned · %d Blocked\n\n",
		counts[adapterkit.StateCompleted], counts[adapterkit.StateCutover], counts[adapterkit.StateApproved],
		counts[adapterkit.StateShadow], counts[adapterkit.StateVerified], counts[adapterkit.StateInProgress],
		counts[adapterkit.StateReady], counts[adapterkit.StatePlanned], counts[adapterkit.StateBlocked]))

	if nextUnit != nil {
		sb.WriteString(fmt.Sprintf("Next Unit to Migrate: %s (State: %s, Wave: %d)\n", nextUnit.ID, nextUnit.State, nextUnit.Wave))
	} else {
		sb.WriteString("Next Unit to Migrate: None (all units active or completed)\n")
	}

	if len(activeLeases) > 0 {
		sb.WriteString(fmt.Sprintf("Active Leases:\n  - %s\n", strings.Join(activeLeases, "\n  - ")))
	} else {
		sb.WriteString("Active Leases: None\n")
	}

	if len(blockers) > 0 {
		sb.WriteString(fmt.Sprintf("Blockers:\n  - %s\n", strings.Join(blockers, "\n  - ")))
	}

	text := s.sanitizer.SanitizeString(sb.String())
	return &ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: text}},
	}, nil
}

func statePriorityOrder(st adapterkit.UnitState) int {
	switch st {
	case adapterkit.StateReady:
		return 1
	case adapterkit.StateRecorded:
		return 2
	case adapterkit.StatePlanned:
		return 3
	default:
		return 4
	}
}

// 2. claim_unit
func (s *Server) handleClaimUnit(ctx context.Context, args json.RawMessage) (*ToolCallResult, error) {
	var input struct {
		UnitID     string  `json:"unit_id"`
		Actor      string  `json:"actor"`
		Profile    string  `json:"profile"`
		TTLSeconds float64 `json:"ttl_seconds"`
		Dir        string  `json:"dir"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.UnitID == "" {
		return nil, fmt.Errorf("unit_id is required")
	}
	if input.Actor == "" {
		input.Actor = "agent"
	}
	ttl := 1 * time.Hour
	if input.TTLSeconds > 0 {
		ttl = time.Duration(input.TTLSeconds) * time.Second
	}

	dir := resolveWorkspaceDir(input.Dir)
	store := state.NewStore(dir)

	unit, err := store.GetUnit(input.UnitID)
	if err != nil {
		return nil, fmt.Errorf("unit %q not found: %w", input.UnitID, err)
	}

	if _, err := validatedProfile(unit, input.Profile); err != nil {
		return nil, err
	}

	// If Planned, advance to Recorded first
	if unit.State == adapterkit.StatePlanned {
		unit, err = store.Transition(input.UnitID, adapterkit.StateRecorded, state.TransitionOptions{})
		if err != nil {
			return nil, fmt.Errorf("cannot advance unit to Recorded: %w", err)
		}
	}

	// If Recorded, advance to Ready first
	if unit.State == adapterkit.StateRecorded {
		unit, err = store.Transition(input.UnitID, adapterkit.StateReady, state.TransitionOptions{})
		if err != nil {
			return nil, fmt.Errorf("cannot advance unit to Ready: %w", err)
		}
	}

	// Advance to In progress with lease
	unit, err = store.Transition(input.UnitID, adapterkit.StateInProgress, state.TransitionOptions{
		Actor:    input.Actor,
		LeaseTTL: ttl,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to claim unit: %w", err)
	}

	lease, _ := store.GetLease(input.UnitID)
	expiresAt := ""
	if lease != nil {
		expiresAt = lease.ExpiresAt.Format(time.RFC3339)
	}

	msg := fmt.Sprintf("CLAIMED: Unit %q is now 'In progress'.\nOwner: %s\nLease Expires: %s\nLease File: .go-anything/local/%s.lease",
		unit.ID, input.Actor, expiresAt, unit.ID)

	return &ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: s.sanitizer.SanitizeString(msg)}},
	}, nil
}

// 3. inspect_unit
func (s *Server) handleInspectUnit(ctx context.Context, args json.RawMessage) (*ToolCallResult, error) {
	var input struct {
		UnitID string `json:"unit_id"`
		Dir    string `json:"dir"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.UnitID == "" {
		return nil, fmt.Errorf("unit_id is required")
	}

	dir := resolveWorkspaceDir(input.Dir)
	store := state.NewStore(dir)

	unit, err := store.GetUnit(input.UnitID)
	if err != nil {
		return nil, fmt.Errorf("unit %q not found: %w", input.UnitID, err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Unit Inspection: %s ===\n", unit.ID))
	sb.WriteString(fmt.Sprintf("Name: %s\nState: %s\nWave: %d\nDependencies: %v\n\n",
		unit.Name, unit.State, unit.Wave, unit.Dependencies))

	// Load source files if available
	legacyDir := filepath.Join(dir, "legacy", filepath.FromSlash(unit.ID))
	if _, err := os.Stat(legacyDir); err == nil {
		sb.WriteString("Original Source Files (legacy/):\n")
		_ = filepath.Walk(legacyDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
				rel, _ := filepath.Rel(dir, path)
				sb.WriteString(fmt.Sprintf("- %s (%d bytes)\n", rel, info.Size()))
			}
			return nil
		})
		sb.WriteString("\n")
	}

	// Load sample cases
	casesFile := filepath.Join(dir, ".go-anything", "cases", filepath.FromSlash(unit.ID)+".jsonl")
	if data, err := os.ReadFile(casesFile); err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		sb.WriteString(fmt.Sprintf("Recorded Cases (%d total):\n", len(lines)))
		maxSample := 3
		if len(lines) < maxSample {
			maxSample = len(lines)
		}
		for i := 0; i < maxSample; i++ {
			sb.WriteString(fmt.Sprintf("Sample Case %d: %s\n", i+1, lines[i]))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("No test cases recorded yet under .go-anything/cases/.\n\n")
	}

	text := s.sanitizer.WrapUntrusted(sb.String())
	return &ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: text}},
	}, nil
}

// 4. record_cases
func (s *Server) handleRecordCases(ctx context.Context, args json.RawMessage) (*ToolCallResult, error) {
	var input struct {
		UnitID  string `json:"unit_id"`
		URL     string `json:"url"`
		Command string `json:"command"`
		Dir     string `json:"dir"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.UnitID == "" {
		return nil, fmt.Errorf("unit_id is required")
	}
	if input.URL == "" && input.Command == "" {
		return nil, fmt.Errorf("either 'url' or 'command' must be specified to record cases")
	}

	dir := resolveWorkspaceDir(input.Dir)
	store := state.NewStore(dir)

	unit, err := store.GetUnit(input.UnitID)
	if err != nil {
		return nil, fmt.Errorf("unit %q not found: %w", input.UnitID, err)
	}

	rec := recorder.New(recorder.Config{
		WorkspaceRoot: dir,
		Store:         store,
	})

	var recordedCount int
	if input.URL != "" {
		parsedURL, parseErr := url.Parse(input.URL)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid url %q: %w", input.URL, parseErr)
		}
		// Store a relative path+query so replay can be pointed at the ported
		// implementation instead of silently re-calling the original host.
		caseIn := adapterkit.CaseInput{
			Method: "GET",
			Path:   parsedURL.RequestURI(),
		}
		execFn := createHTTPExecutionFunc(input.URL, s.AllowNetwork)
		res, err := rec.RecordCase(ctx, input.UnitID, caseIn, adapterkit.KindHappy, execFn)
		if err != nil {
			return nil, fmt.Errorf("recording failed: %w", err)
		}
		if res != nil {
			recordedCount = 1
		}
	} else {
		tokens, err := recorder.SplitTokens(input.Command)
		if err != nil || len(tokens) == 0 {
			return nil, fmt.Errorf("failed to parse command: %v", err)
		}
		cmdName := tokens[0]
		cmdArgs := tokens[1:]
		res, err := rec.RecordCommand(ctx, input.UnitID, cmdName, cmdArgs, adapterkit.KindHappy)
		if err != nil {
			return nil, fmt.Errorf("command recording failed: %w", err)
		}
		if res != nil {
			recordedCount = 1
		}
	}

	// Advance unit to Recorded
	if unit.State == adapterkit.StatePlanned {
		_, _ = store.Transition(input.UnitID, adapterkit.StateRecorded, state.TransitionOptions{})
	}

	msg := fmt.Sprintf("RECORDED: Successfully captured %d case(s) with fail-closed in-memory redaction.\nCases saved to .go-anything/cases/%s.jsonl\nUnit state advanced to Recorded.",
		recordedCount, input.UnitID)

	return &ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: s.sanitizer.WrapUntrusted(msg)}},
	}, nil
}

// 5. verify_diff
func (s *Server) handleVerifyDiff(ctx context.Context, args json.RawMessage) (*ToolCallResult, error) {
	var input struct {
		UnitID string `json:"unit_id"`
		Target string `json:"target"`
		Mode   string `json:"mode"`
		Dir    string `json:"dir"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.UnitID == "" || input.Target == "" {
		return nil, fmt.Errorf("unit_id and target are required")
	}

	dir := resolveWorkspaceDir(input.Dir)
	store := state.NewStore(dir)
	casesFile := filepath.Join(dir, ".go-anything", "cases", filepath.FromSlash(input.UnitID)+".jsonl")
	cases, err := loadCasesFromJSONL(casesFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load cases for unit %q: %w", input.UnitID, err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("no test cases recorded for unit %q", input.UnitID)
	}

	// Remember the ported target so run_gate can replay the same implementation
	// when it assembles full-profile evidence.
	if unit, unitErr := store.GetUnit(input.UnitID); unitErr == nil {
		if unit.Metadata == nil {
			unit.Metadata = make(map[string]any)
		}
		unit.Metadata["target"] = input.Target
		if input.Mode != "" {
			unit.Metadata["comparison_mode"] = input.Mode
		} else {
			delete(unit.Metadata, "comparison_mode")
		}
		if saveErr := store.SaveUnit(unit); saveErr != nil {
			return nil, fmt.Errorf("failed to persist replay target for unit %q: %w", input.UnitID, saveErr)
		}
	}

	driver, err := buildTargetReplayDriver(input.Target, s.AllowNetwork)
	if err != nil {
		return nil, fmt.Errorf("failed to build replay driver: %w", err)
	}
	defer driver.Close()

	rules, _ := store.GetRules()
	checker := &gate.ReplayChecker{
		UnitID: input.UnitID,
		Cases:  cases,
		Driver: driver,
		Rules:  rules,
		Jobs:   runtime.NumCPU(),
		Mode:   input.Mode,
	}

	out, issues, passed, err := checker.Check(ctx)
	if err != nil {
		return nil, fmt.Errorf("replay check error: %w", err)
	}

	var sb strings.Builder
	if passed {
		sb.WriteString(fmt.Sprintf("PASS: %s\n", out))
	} else {
		sb.WriteString(fmt.Sprintf("MISMATCH DETECTED: Unit %q failed replay verification.\n\nIssues:\n", input.UnitID))
		for _, iss := range issues {
			sb.WriteString(fmt.Sprintf("- %s\n", iss))
		}
		sb.WriteString("\nDetailed Report:\n")
		sb.WriteString(out)
	}

	return &ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: s.sanitizer.WrapUntrusted(sb.String())}},
		IsError: !passed,
	}, nil
}

// 6. run_gate
func (s *Server) handleRunGate(ctx context.Context, args json.RawMessage) (*ToolCallResult, error) {
	var input struct {
		UnitID            string `json:"unit_id"`
		AllowMissingTools bool   `json:"allow_missing_tools"`
		Profile           string `json:"profile"`
		Dir               string `json:"dir"`
	}
	_ = json.Unmarshal(args, &input)
	dir := resolveWorkspaceDir(input.Dir)
	store := state.NewStore(dir)

	checks := gate.AllChecks()
	executors := make(map[string]gate.CheckExecutor)

	var unit *adapterkit.Unit
	if input.UnitID != "" {
		loaded, err := store.GetUnit(input.UnitID)
		if err != nil {
			return nil, fmt.Errorf("unit %q not found: %w", input.UnitID, err)
		}
		unit = loaded
		if _, err := validatedProfile(unit, input.Profile); err != nil {
			return nil, err
		}

		casesFile := filepath.Join(dir, ".go-anything", "cases", filepath.FromSlash(unit.ID)+".jsonl")
		cases, err := loadCasesFromJSONL(casesFile)
		if err != nil || len(cases) == 0 {
			return nil, fmt.Errorf("unit %q has no recorded cases; run record_cases first", unit.ID)
		}

		target, _ := unit.Metadata["target"].(string)
		if target == "" {
			return nil, fmt.Errorf("unit %q has no replay target; run verify_diff first", unit.ID)
		}
		driver, err := buildTargetReplayDriver(target, s.AllowNetwork)
		if err != nil {
			return nil, fmt.Errorf("failed to build replay driver for %q: %w", target, err)
		}
		defer driver.Close()

		rules, _ := store.GetRules()
		mode, _ := unit.Metadata["comparison_mode"].(string)
		replayChecker := &gate.ReplayChecker{
			UnitID: unit.ID,
			Cases:  cases,
			Driver: driver,
			Rules:  rules,
			Jobs:   runtime.NumCPU(),
			Mode:   mode,
		}
		executors[gate.CheckReplay] = func(checkCtx context.Context, _ string, _ []string) (string, []string, bool, error) {
			return replayChecker.Check(checkCtx)
		}

		sourceDir := filepath.Join(dir, "legacy", filepath.FromSlash(unit.ID))
		if fi, statErr := os.Stat(sourceDir); statErr != nil || !fi.IsDir() {
			sourceDir = filepath.Join(dir, filepath.FromSlash(unit.ID))
		}
		executors[gate.CheckErrorPaths] = gate.CheckErrorPathsExecutor(sourceDir, cases)

		legacyDir := filepath.Join(dir, "legacy", filepath.FromSlash(unit.ID))
		modernDir := filepath.Join(dir, "modern", filepath.FromSlash(unit.ID))
		if isDir(legacyDir) && isDir(modernDir) {
			executors[gate.CheckAPICompat] = gate.CheckAPICompatExecutor(legacyDir, modernDir)
		} else {
			executors[gate.CheckAPICompat] = func(context.Context, string, []string) (string, []string, bool, error) {
				return "api compatibility clean (no separate legacy/modern split)", nil, true, nil
			}
		}

		checks = append(checks, gate.CheckReplay, gate.CheckErrorPaths, gate.CheckAPICompat)
	}

	commitSHA, err := resolveCommitSHA(ctx, dir)
	if err != nil {
		return nil, err
	}
	if err := requireCleanTree(ctx, dir); err != nil {
		return nil, err
	}
	runner := gate.New(gate.Config{
		Dir:               dir,
		Checks:            checks,
		AllowMissingTools: input.AllowMissingTools,
		Executors:         executors,
	})

	report, err := runner.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("gate execution error: %w", err)
	}

	afterSHA, err := resolveCommitSHA(ctx, dir)
	if err != nil {
		return nil, err
	}
	if afterSHA != commitSHA {
		return nil, fmt.Errorf("HEAD changed during gate run")
	}
	if err := requireCleanTree(ctx, dir); err != nil {
		return nil, err
	}

	// Persist evidence only when every check passed. mark_verified will then
	// accept the transition for the unit's declared evidence profile.
	if unit != nil && report.Passed {
		// Reload to validate the current declaration; do not overwrite unit state.
		fresh, err := store.GetUnit(unit.ID)
		if err != nil {
			return nil, err
		}
		profile, err := validatedProfile(fresh, input.Profile)
		if err != nil {
			return nil, err
		}

		if !requiredChecksPassed(report, profile) {
			return &ToolCallResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Required checks were skipped; no passing evidence saved"}}}, nil
		}
		ev := &adapterkit.Evidence{
			SchemaVersion:   adapterkit.DefaultSchemaVersion,
			EvidenceID:      fmt.Sprintf("evi-%s-%s-%d", sanitizeUnitID(unit.ID), commitSHA[:8], time.Now().UnixNano()),
			UnitID:          unit.ID,
			CommitSHA:       commitSHA,
			Profile:         profile,
			VerifiedAt:      time.Now().UTC().Format(time.RFC3339),
			VerifiedBy:      "mcp-agent",
			Checks:          report.ToGateChecks(),
			MismatchesCount: 0,
		}
		if saveErr := store.SaveEvidence(ev); saveErr != nil {
			return nil, fmt.Errorf("failed to save gate evidence: %w", saveErr)
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Quality Gate Report (Passed: %t, %d passed, %d failed, %d skipped):\n",
		report.Passed, report.PassedCount, report.FailedCount, report.SkippedCount))
	for _, c := range report.Checks {
		statusStr := strings.ToUpper(string(c.Status))
		sb.WriteString(fmt.Sprintf("- [%s] %s (%.1f ms)\n", statusStr, c.Name, c.DurationMS))
		if !c.Passed && len(c.Issues) > 0 {
			sb.WriteString(fmt.Sprintf("    issues: %s\n", strings.Join(c.Issues, "; ")))
		}
	}
	sb.WriteString(fmt.Sprintf("\nCommit SHA: %s\n", commitSHA))

	return &ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: s.sanitizer.SanitizeString(sb.String())}},
		IsError: !report.Passed,
	}, nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// 7. mark_verified
func (s *Server) handleMarkVerified(ctx context.Context, args json.RawMessage) (*ToolCallResult, error) {
	var input struct {
		UnitID    string `json:"unit_id"`
		CommitSHA string `json:"commit_sha"`
		Actor     string `json:"actor"`
		Dir       string `json:"dir"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.UnitID == "" {
		return nil, fmt.Errorf("unit_id is required")
	}
	if input.Actor == "" {
		input.Actor = "agent"
	}

	dir := resolveWorkspaceDir(input.Dir)
	store := state.NewStore(dir)

	if unit, err := store.GetUnit(input.UnitID); err != nil {
		return nil, fmt.Errorf("unit %q not found: %w", input.UnitID, err)
	} else if _, err := validatedProfile(unit, ""); err != nil {
		return nil, err
	}

	// Resolve target commit SHA
	targetSHA := input.CommitSHA
	if targetSHA == "" {
		var err error
		targetSHA, err = resolveCommitSHA(ctx, dir)
		if err != nil {
			return nil, err
		}
		if err := requireCleanTree(ctx, dir); err != nil {
			return nil, err
		}
	}

	// Strict Guard: Check if valid evidence exists for this commit SHA
	evidence, err := store.GetEvidenceForCommit(input.UnitID, targetSHA)
	if err != nil || evidence == nil {
		return &ToolCallResult{
			Content: []ContentItem{{
				Type: "text",
				Text: fmt.Sprintf("TRANSITION REJECTED: Unit %q cannot be marked Verified because no evidence exists for commit SHA %s. Run 'run_gate' or verify tests first.",
					input.UnitID, targetSHA),
			}},
			IsError: true,
		}, nil
	}

	// Advance state to Verified
	verifiedUnit, err := store.Transition(input.UnitID, adapterkit.StateVerified, state.TransitionOptions{
		Actor:    input.Actor,
		Evidence: evidence,
	})
	if err != nil {
		return &ToolCallResult{
			Content: []ContentItem{{
				Type: "text",
				Text: fmt.Sprintf("TRANSITION REJECTED: State guard failed: %v", err),
			}},
			IsError: true,
		}, nil
	}

	msg := fmt.Sprintf("SUCCESS: Unit %q transitioned to %s.\nCommit SHA: %s\nEvidence ID: %s",
		verifiedUnit.ID, verifiedUnit.State, evidence.CommitSHA, evidence.EvidenceID)

	return &ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: s.sanitizer.SanitizeString(msg)}},
	}, nil
}

// 8. handoff
func (s *Server) handleHandoff(ctx context.Context, args json.RawMessage) (*ToolCallResult, error) {
	var input struct {
		Summary   string `json:"summary"`
		NextSteps string `json:"next_steps"`
		Dir       string `json:"dir"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.Summary == "" || input.NextSteps == "" {
		return nil, fmt.Errorf("both 'summary' and 'next_steps' are required for handoff")
	}

	dir, err := filepath.EvalSymlinks(resolveWorkspaceDir(input.Dir))
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(s.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("handoff directory must be within server workspace")
	}
	store := state.NewStore(dir)

	units, _ := store.ListUnits()
	var unitSummaries []string
	for _, u := range units {
		unitSummaries = append(unitSummaries, fmt.Sprintf("- **%s**: %s (Wave %d)", u.ID, u.State, u.Wave))
	}

	var sb strings.Builder
	sb.WriteString("# HANDOFF.md — Migration Session Handoff\n\n")
	sb.WriteString(fmt.Sprintf("> Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString("## Session Summary\n\n")
	sb.WriteString(input.Summary)
	sb.WriteString("\n\n")

	sb.WriteString("## Migration Unit Status\n\n")
	if len(unitSummaries) > 0 {
		sb.WriteString(strings.Join(unitSummaries, "\n"))
	} else {
		sb.WriteString("No migration units registered.")
	}
	sb.WriteString("\n\n")

	sb.WriteString("## Recommended Next Steps\n\n")
	sb.WriteString(input.NextSteps)
	sb.WriteString("\n\n")

	sb.WriteString("## AI Agent Instructions for Next Session\n\n")
	sb.WriteString("1. Inspect current status via `go-anything mcp` `status` tool or `go-anything status`.\n")
	sb.WriteString("2. Pick the next Ready unit in dependency wave order and call `claim_unit`.\n")
	sb.WriteString("3. Run `verify_diff` and iterate until replay passes with zero mismatches.\n")
	sb.WriteString("4. Run `run_gate` and ensure commit-tied evidence is generated.\n")
	sb.WriteString("5. Call `mark_verified` once all quality checks are clean.\n")

	handoffPath := filepath.Join(dir, "HANDOFF.md")
	tmp, err := os.CreateTemp(dir, ".handoff-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(sb.String()); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), handoffPath); err != nil {
		return nil, fmt.Errorf("failed to write HANDOFF.md: %w", err)
	}

	msg := fmt.Sprintf("HANDOFF WRITTEN: Successfully generated %s with state summary and next steps.", handoffPath)
	return &ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: s.sanitizer.SanitizeString(msg)}},
	}, nil
}

func sanitizeUnitID(unitID string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
	return r.Replace(unitID)
}

func resolveCommitSHA(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "HEAD^{commit}")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	sha := strings.TrimSpace(string(out))
	if !regexp.MustCompile(`^[0-9a-fA-F]{40}$`).MatchString(sha) {
		return "", fmt.Errorf("invalid commit SHA %q", sha)
	}
	return sha, nil
}

// Evidence tests committed source. Migration bookkeeping is excluded because
// recording evidence and transitioning units necessarily updates that directory.
func requireCleanTree(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=all", "--", ".", ":(exclude).go-anything")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("check working tree: %w", err)
	}
	if len(bytes.TrimSpace(out)) != 0 {
		return fmt.Errorf("working tree must be clean before saving or accepting gate evidence; commit source changes first")
	}
	return nil
}

func validatedProfile(unit *adapterkit.Unit, requested string) (adapterkit.EvidenceProfile, error) {
	profile := unit.EvidenceProfile
	if profile == "" {
		profile = adapterkit.ProfileFull
	}
	if profile != adapterkit.ProfileReplay && profile != adapterkit.ProfileFull {
		return "", fmt.Errorf("invalid unit evidence profile %q", profile)
	}
	if requested != "" && requested != string(profile) {
		return "", fmt.Errorf("requested profile %q does not match unit profile %q", requested, profile)
	}
	return profile, nil
}

func requiredChecksPassed(report *gate.GateReport, profile adapterkit.EvidenceProfile) bool {
	checks := gate.FullProfileChecks()
	if profile == adapterkit.ProfileReplay {
		checks = []string{gate.CheckReplay}
	}
	for _, name := range checks {
		found := false
		for _, check := range report.Checks {
			if check.Name == name && check.Status == gate.StatusPass {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// buildTargetReplayDriver resolves an HTTP or CLI driver for replay.
func buildTargetReplayDriver(target string, allowNetwork ...bool) (adapterkit.Replay, error) {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return httpadapter.NewAdapter(httpadapter.WithBaseURL(target), httpadapter.WithClient(outboundClient(allowNetwork...))), nil
	}
	if strings.Contains(target, "://") {
		return nil, fmt.Errorf("unsupported target scheme")
	}
	return cliadapter.NewAdapter(target), nil
}

func createHTTPExecutionFunc(urlTarget string, allowNetwork ...bool) recorder.ExecutionFunc {
	client := outboundClient(allowNetwork...)

	return func(ctx context.Context, in adapterkit.CaseInput) (*adapterkit.CaseOutput, error) {
		method := in.Method
		if method == "" {
			method = "GET"
		}
		urlToCall := urlTarget
		if in.Path != "" && strings.HasPrefix(in.Path, "http") {
			urlToCall = in.Path
		}

		var bodyReader io.Reader
		if in.Body != nil {
			switch b := in.Body.(type) {
			case string:
				bodyReader = strings.NewReader(b)
			case []byte:
				bodyReader = bytes.NewReader(b)
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, urlToCall, bodyReader)
		if err != nil {
			return nil, err
		}
		for k, v := range in.Headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		headers := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		out := &adapterkit.CaseOutput{
			StatusCode: resp.StatusCode,
			Headers:    headers,
			Body:       string(respBytes),
		}
		return out, nil
	}
}

func loadCasesFromJSONL(path string) ([]*adapterkit.Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cases []*adapterkit.Case
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(f)
	maxSize := int64(int(^uint(0) >> 1))
	if info.Size() < maxSize {
		maxSize = info.Size() + 1
	}
	scanner.Buffer(nil, int(maxSize))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}
		var c adapterkit.Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("failed to parse case line: %w", err)
		}
		cases = append(cases, &c)
	}
	return cases, scanner.Err()
}
