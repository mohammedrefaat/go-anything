package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammedrefaat/go-anything/internal/mcp"
)

// CommandMetadata details the wiring status, testing coverage, and MCP mappings for a CLI command.
type CommandMetadata struct {
	Name           string
	WiringStatus   string
	E2ECovered     bool
	E2ETestFiles   string
	MCPCounterpart string
	SinceVersion   string
	Milestone      string
	Description    string
}

// AllRegisteredCommands lists all 32 CLI commands in go-anything.
var AllRegisteredCommands = []CommandMetadata{
	{
		Name:           "init",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/cli_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v0.1.0",
		Milestone:      "M0",
		Description:    "Initialize .go-anything/ workspace layout in target directory",
	},
	{
		Name:           "modernize",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/modernize_test.go, cmd/go-anything/dogfood_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v0.1.0",
		Milestone:      "M0",
		Description:    "Initialize Go-to-Go modernization workspace (alias for init --source=go)",
	},
	{
		Name:           "status",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/cli_test.go, cmd/go-anything/dogfood_test.go",
		MCPCounterpart: "`status`",
		SinceVersion:   "v0.1.0",
		Milestone:      "M0",
		Description:    "Show migration progress, units, waves, and active leases",
	},
	{
		Name:           "doctor",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/cli_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v0.1.0",
		Milestone:      "M0",
		Description:    "Diagnose workspace health, check .gitignore, and scan for leaked secrets",
	},
	{
		Name:           "analyze",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/h1_cli_test.go",
		MCPCounterpart: "`status`",
		SinceVersion:   "v0.1.0",
		Milestone:      "M1 / H1",
		Description:    "Analyze dependencies and generate topological migration waves",
	},
	{
		Name:           "record",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/e2e_m1_test.go, cmd/go-anything/dogfood_test.go",
		MCPCounterpart: "`record_cases`",
		SinceVersion:   "v0.1.0",
		Milestone:      "M1",
		Description:    "Record baseline behavioral test cases in restricted sandbox",
	},
	{
		Name:           "replay",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/e2e_m1_test.go, cmd/go-anything/dogfood_test.go",
		MCPCounterpart: "`verify_diff`",
		SinceVersion:   "v0.1.0",
		Milestone:      "M1",
		Description:    "Execute concurrent deterministic replay of recorded cases",
	},
	{
		Name:           "verify",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/dogfood_test.go, cmd/go-anything/verify_approve_promote_test.go",
		MCPCounterpart: "`run_gate`, `mark_verified`",
		SinceVersion:   "v0.1.0",
		Milestone:      "M2",
		Description:    "Replay cases, store commit-tied evidence, and advance state to Verified",
	},
	{
		Name:           "approve",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/verify_approve_promote_test.go",
		MCPCounterpart: "`human-only` (forbidden per Rule 4)",
		SinceVersion:   "v0.1.0",
		Milestone:      "M2",
		Description:    "Record human sign-off and advance state to Approved (--skip-shadow)",
	},
	{
		Name:           "promote",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/e2e_m2_test.go, cmd/go-anything/verify_approve_promote_test.go",
		MCPCounterpart: "`human-only` (forbidden per Rule 4)",
		SinceVersion:   "v0.1.0",
		Milestone:      "M2",
		Description:    "Switch front door routing to Go, test kill switch, and advance state to Cutover",
	},
	{
		Name:           "diff",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/h1_cli_test.go",
		MCPCounterpart: "`verify_diff`",
		SinceVersion:   "v0.1.0",
		Milestone:      "M1 / H1",
		Description:    "Replay cases against Go implementation and show structural diffs",
	},
	{
		Name:           "fuzz",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/fuzz_test.go",
		MCPCounterpart: "`fuzz_bench`",
		SinceVersion:   "v2.0.0-alpha",
		Milestone:      "v2.0 / R1",
		Description:    "Execute differential fuzzing against recorded cases and target implementation",
	},
	{
		Name:           "bench",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/bench_test.go",
		MCPCounterpart: "`fuzz_bench`",
		SinceVersion:   "v2.0.0-alpha",
		Milestone:      "v2.0 / R2",
		Description:    "Execute comparative latency and memory allocation benchmarks",
	},
	{
		Name:           "profile",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/profile_test.go",
		MCPCounterpart: "`profile`",
		SinceVersion:   "v2.0.0-alpha",
		Milestone:      "v2.0 / R3",
		Description:    "Profile legacy execution and rank candidate hot functions for migration",
	},
	{
		Name:           "bridge",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/bridge_test.go",
		MCPCounterpart: "`bridge`",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / B2",
		Description:    "Generate and manage hot-path strangler sidecar bridges",
	},
	{
		Name:           "go",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/go_test.go",
		MCPCounterpart: "`hotpath`",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / B4",
		Description:    "Execute guided end-to-end hot-path porting pipeline",
	},
	{
		Name:           "enable",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/companion_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / B5",
		Description:    "Enable Go strangler bridge routing for a unit or function",
	},
	{
		Name:           "disable",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/companion_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / B5",
		Description:    "Disable Go strangler bridge routing for a unit or function",
	},
	{
		Name:           "refresh",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/companion_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / B5",
		Description:    "Re-record behavioral cases and evaluate updated diffs",
	},
	{
		Name:           "clean",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/companion_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / B5",
		Description:    "Remove generated bridge artifacts while preserving developer logic",
	},
	{
		Name:           "report",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/companion_test.go",
		MCPCounterpart: "`effects_report`",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / B5",
		Description:    "Generate comprehensive Markdown/JSON scorecard report for a unit",
	},
	{
		Name:           "map",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/companion_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / B5",
		Description:    "List functions in a unit and display porting and routing status",
	},
	{
		Name:           "gate",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/h1_cli_test.go",
		MCPCounterpart: "`run_gate`",
		SinceVersion:   "v0.1.0",
		Milestone:      "M4 / H1",
		Description:    "Execute Quality Gate pipeline (linters, race detector, replay)",
	},
	{
		Name:           "shadow",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/h1_cli_test.go, cmd/go-anything/m5_acceptance_test.go",
		MCPCounterpart: "`human-only` (forbidden per Rule 4)",
		SinceVersion:   "v0.1.0",
		Milestone:      "M5 / H1",
		Description:    "Manage and run front door in shadow mode on real traffic",
	},
	{
		Name:           "cutover",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/h1_cli_test.go",
		MCPCounterpart: "`human-only` (forbidden per Rule 4)",
		SinceVersion:   "v0.1.0",
		Milestone:      "M2 / H1",
		Description:    "Switch primary traffic to verified Go unit and manage kill switch (Rule R-1)",
	},
	{
		Name:           "lift",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/e2e_m2_test.go",
		MCPCounterpart: "`human-only` (forbidden per Rule 4)",
		SinceVersion:   "v0.1.0",
		Milestone:      "M2",
		Description:    "Remove fallback wrappers and finalize modernization (Rule 4 human-only)",
	},
	{
		Name:           "migrate",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/migrate_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v0.1.0",
		Milestone:      "M7",
		Description:    "Upgrade .go-anything/ workspace from v1alpha1 to stable v1 schemas",
	},
	{
		Name:           "template",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/template_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v0.1.0",
		Milestone:      "M0",
		Description:    "Render agent instruction files (CLAUDE.md, .cursorrules, AGENTS.md, GEMINI.md)",
	},
	{
		Name:           "setup",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/setup_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v2.0.0-beta",
		Milestone:      "v2.0 / M2",
		Description:    "Configure AI agent tools and MCP server registration for Claude Code, Codex, Cursor, Gemini, OpenCode",
	},
	{
		Name:           "mcp",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/mcp_test.go",
		MCPCounterpart: fmt.Sprintf("Hosts all %d MCP tools", len(mcp.StandardToolNames)),
		SinceVersion:   "v0.1.0",
		Milestone:      "M4",
		Description:    "Run MCP server over stdio for AI agent orchestration",
	},
	{
		Name:           "version",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/cli_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v0.1.0",
		Milestone:      "M0",
		Description:    "Show go-anything version, profile, and jobs configuration",
	},
	{
		Name:           "help",
		WiringStatus:   "Fully Wired",
		E2ECovered:     true,
		E2ETestFiles:   "cmd/go-anything/cli_test.go, cmd/go-anything/stubs_guard_test.go",
		MCPCounterpart: "none",
		SinceVersion:   "v0.1.0",
		Milestone:      "M0",
		Description:    "Show go-anything usage and available commands",
	},
}

// GenerateStatusMarkdown produces the complete STATUS.md document.
func GenerateStatusMarkdown() string {
	var buf bytes.Buffer

	total := len(AllRegisteredCommands)
	wired := 0
	e2e := 0
	for _, c := range AllRegisteredCommands {
		if c.WiringStatus == "Fully Wired" {
			wired++
		}
		if c.E2ECovered {
			e2e++
		}
	}

	buf.WriteString("<!-- Code generated by tools/genstatus. DO NOT EDIT. -->\n\n")
	buf.WriteString("# go-anything Command Status & Traceability Matrix\n\n")
	buf.WriteString("> Real-time declaration of registered CLI commands, implementation wiring status, E2E test coverage, and MCP mappings.\n\n")

	buf.WriteString("## Summary\n\n")
	buf.WriteString(fmt.Sprintf("- **Total Commands:** %d\n", total))
	buf.WriteString(fmt.Sprintf("- **Wiring Status:** %d / %d Fully Wired (%.0f%%)\n", wired, total, float64(wired)/float64(total)*100))
	buf.WriteString(fmt.Sprintf("- **Stubbed Commands:** %d (0%%)\n", total-wired))
	buf.WriteString(fmt.Sprintf("- **E2E Test Coverage:** %d / %d Commands Covered (%.0f%%)\n\n", e2e, total, float64(e2e)/float64(total)*100))

	buf.WriteString("## Command Traceability Matrix\n\n")
	buf.WriteString("| Command | Wiring Status | E2E Covered | MCP Counterpart | Since | Description |\n")
	buf.WriteString("| :--- | :--- | :---: | :--- | :---: | :--- |\n")

	for _, c := range AllRegisteredCommands {
		e2eStr := "✅ Yes"
		if !c.E2ECovered {
			e2eStr = "❌ No"
		}
		statusBadge := "**" + c.WiringStatus + "**"
		buf.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | `%s` (%s) | %s |\n",
			c.Name, statusBadge, e2eStr, c.MCPCounterpart, c.SinceVersion, c.Milestone, c.Description))
	}

	buf.WriteString("\n## MCP Server Tools & Security Boundary (Execution Rule 4)\n\n")
	buf.WriteString(GenerateMCPToolsSection())
	buf.WriteString("\n\nAny invocation of the human-only commands via MCP is rejected fail-closed with code `FORBIDDEN`.\n\n")

	buf.WriteString("## Standard CLI Exit Codes\n\n")
	buf.WriteString("| Exit Code | Meaning | Description |\n")
	buf.WriteString("| :---: | :--- | :--- |\n")
	buf.WriteString("| `0` | Success / OK | Command completed cleanly with all checks or operations satisfied. |\n")
	buf.WriteString("| `1` | Check / Diff Failure | Verification check failed, quality gate check failed, or behavioral mismatch detected. |\n")
	buf.WriteString("| `2` | Usage Error | Invalid flag, missing required flag, or unrecognized subcommand. |\n")
	buf.WriteString("| `3` | Internal Error | Unexpected runtime error, I/O failure, or execution exception. |\n")

	return buf.String()
}

const (
	mcpToolsBeginMarker = "<!-- BEGIN GENERATED: mcp-tools -->"
	mcpToolsEndMarker   = "<!-- END GENERATED: mcp-tools -->"
)

var mcpToolsTargets = []string{
	"SECURITY.md",
	"CLAUDE.md",
	"AGENTS.md",
	filepath.Join("templates", "mcp_tools.md"),
}

// GenerateMCPToolsSection emits the MCP tool table and human-only controls list
// derived directly from internal/mcp.
func GenerateMCPToolsSection() string {
	defs := mcp.GetToolDefinitions()
	var buf bytes.Buffer

	buf.WriteString(mcpToolsBeginMarker + "\n")
	buf.WriteString(fmt.Sprintf("The MCP server (`go-anything mcp`, `stdio` only) exposes **%d tools** (agent-accessible):\n\n", len(defs)))
	buf.WriteString("| Tool | Access | Purpose |\n")
	buf.WriteString("| :--- | :--- | :--- |\n")
	for _, def := range defs {
		purpose := strings.ReplaceAll(def.Description, "|", "\\|")
		buf.WriteString(fmt.Sprintf("| `%s` | Agent | %s |\n", def.Name, purpose))
	}

	buf.WriteString("\n**Not exposed via MCP (CLI-only, Execution Rule 4):**\n\n")
	for _, ctrl := range mcp.HumanOnlyControls {
		buf.WriteString(fmt.Sprintf("- `%s`: %s\n", ctrl.Name, ctrl.Description))
	}
	buf.WriteString(mcpToolsEndMarker)

	return buf.String()
}

// ReplaceMCPToolsBlock replaces the content between BEGIN/END GENERATED: mcp-tools markers.
func ReplaceMCPToolsBlock(content, generated string) (string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	startIdx := strings.Index(normalized, mcpToolsBeginMarker)
	if startIdx < 0 {
		return "", fmt.Errorf("missing marker %q", mcpToolsBeginMarker)
	}
	endIdx := strings.Index(normalized[startIdx:], mcpToolsEndMarker)
	if endIdx < 0 {
		return "", fmt.Errorf("missing marker %q", mcpToolsEndMarker)
	}
	endIdx += startIdx + len(mcpToolsEndMarker)
	return normalized[:startIdx] + generated + normalized[endIdx:], nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not locate repository root (go.mod not found in parent directories)")
}

func main() {
	checkMode := flag.Bool("check", false, "Verify that generated docs are up to date without modifying them")
	flag.BoolVar(checkMode, "c", false, "Verify that generated docs are up to date (shorthand)")
	flag.Parse()

	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(3)
	}

	statusPath := filepath.Join(repoRoot, "STATUS.md")
	expectedStatus := GenerateStatusMarkdown()
	mcpSection := GenerateMCPToolsSection()

	if *checkMode {
		existing, err := os.ReadFile(statusPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", statusPath, err)
			os.Exit(1)
		}
		if strings.ReplaceAll(string(existing), "\r\n", "\n") != expectedStatus {
			fmt.Fprintf(os.Stderr, "error: STATUS.md is out of date. Run 'go run ./tools/genstatus' to regenerate.\n")
			os.Exit(1)
		}

		for _, rel := range mcpToolsTargets {
			targetPath := filepath.Join(repoRoot, rel)
			raw, err := os.ReadFile(targetPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading %s: %v\n", targetPath, err)
				os.Exit(1)
			}
			normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
			updated, err := ReplaceMCPToolsBlock(normalized, mcpSection)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error in %s: %v\n", rel, err)
				os.Exit(1)
			}
			if normalized != updated {
				fmt.Fprintf(os.Stderr, "error: %s is out of date. Run 'go run ./tools/genstatus' to regenerate.\n", rel)
				os.Exit(1)
			}
		}

		fmt.Println("STATUS.md and MCP tool docs are up to date.")
		return
	}

	if err := os.WriteFile(statusPath, []byte(expectedStatus), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", statusPath, err)
		os.Exit(3)
	}

	for _, rel := range mcpToolsTargets {
		targetPath := filepath.Join(repoRoot, rel)
		raw, err := os.ReadFile(targetPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", targetPath, err)
			os.Exit(3)
		}
		updated, err := ReplaceMCPToolsBlock(string(raw), mcpSection)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error updating %s: %v\n", rel, err)
			os.Exit(3)
		}
		if err := os.WriteFile(targetPath, []byte(updated), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", targetPath, err)
			os.Exit(3)
		}
	}

	fmt.Printf("Successfully generated %s (%d commands, %d MCP tools) and updated %d MCP tool doc targets.\n",
		statusPath, len(AllRegisteredCommands), len(mcp.StandardToolNames), len(mcpToolsTargets))
}
