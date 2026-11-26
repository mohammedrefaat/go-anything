package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammedrefaat/go-anything/templates"
	toml "github.com/pelletier/go-toml/v2"
)

// SupportedTools lists all 5 distinct AI agent tool integrations.
var SupportedTools = []string{
	string(ToolClaudeCode),
	string(ToolCodex),
	string(ToolCursor),
	string(ToolGemini),
	string(ToolOpenCode),
}

// Run executes setup registration or uninstallation for the requested tools.
func Run(opts Options) (*Result, error) {
	tool := strings.ToLower(strings.TrimSpace(opts.Tool))
	if tool == "" || tool == "all" {
		tool = string(ToolAll)
	}
	if tool == "claude" {
		tool = string(ToolClaudeCode)
	}

	scope := strings.ToLower(strings.TrimSpace(opts.Scope))
	if scope == "" {
		scope = string(ScopeProject)
	}

	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}

	binPath := opts.BinaryPath
	if binPath == "" {
		binPath = detectBinaryPath()
	}

	selectedTools := make([]string, 0)
	if tool == string(ToolAll) {
		selectedTools = append(selectedTools, SupportedTools...)
	} else {
		valid := false
		for _, st := range SupportedTools {
			if tool == st {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("unsupported tool %q; must be one of: %s, all", tool, strings.Join(SupportedTools, ", "))
		}
		selectedTools = append(selectedTools, tool)
	}

	changes := make([]FileChange, 0)

	for _, t := range selectedTools {
		ch, err := processTool(t, scope, absDir, binPath, opts)
		if err != nil {
			return nil, err
		}
		changes = append(changes, ch...)
	}

	actionDesc := "Configured"
	if opts.Uninstall {
		actionDesc = "Uninstalled"
	}
	if opts.DryRun && !opts.Yes {
		actionDesc = "[DRY-RUN] Would " + strings.ToLower(actionDesc)
	}

	res := &Result{
		Tool:      tool,
		Scope:     scope,
		DryRun:    opts.DryRun && !opts.Yes,
		Uninstall: opts.Uninstall,
		Changes:   changes,
		Message:   fmt.Sprintf("%s agent integration for: %s (%d files affected)", actionDesc, strings.Join(selectedTools, ", "), len(changes)),
	}

	return res, nil
}

func processTool(tool, scope, dir, binPath string, opts Options) ([]FileChange, error) {
	var changes []FileChange

	configPath, isTOML, tmplName, instFileName := getToolPaths(tool, scope, dir)

	// 1. Process MCP Config File
	change, err := configureMCPFile(configPath, isTOML, tool, binPath, opts)
	if err != nil {
		return nil, err
	}
	changes = append(changes, *change)

	// 2. Process Instructions File (only during install, skip during uninstall)
	if !opts.Uninstall && tmplName != "" && instFileName != "" {
		instPath := filepath.Join(dir, instFileName)
		instChange, err := configureInstructionFile(instPath, tmplName, dir, opts)
		if err == nil && instChange != nil {
			changes = append(changes, *instChange)
		}
	}

	return changes, nil
}

func getToolPaths(tool, scope, dir string) (configPath string, isTOML bool, tmplName, instFileName string) {
	homeDir, _ := os.UserHomeDir()
	isUser := scope == string(ScopeUser) && homeDir != ""

	switch tool {
	case string(ToolClaudeCode):
		if isUser {
			configPath = filepath.Join(homeDir, ".mcp.json")
		} else {
			configPath = filepath.Join(dir, ".mcp.json")
		}
		tmplName = "claude"
		instFileName = "CLAUDE.md"

	case string(ToolCodex):
		if isUser {
			configPath = filepath.Join(homeDir, ".codex", "config.toml")
		} else {
			configPath = filepath.Join(dir, ".codex", "config.toml")
		}
		isTOML = true
		tmplName = "codex"
		instFileName = "AGENTS.md"

	case string(ToolCursor):
		if isUser {
			configPath = filepath.Join(homeDir, ".cursor", "mcp.json")
		} else {
			configPath = filepath.Join(dir, ".cursor", "mcp.json")
		}
		tmplName = "cursor"
		instFileName = ".cursorrules"

	case string(ToolGemini):
		if isUser {
			configPath = filepath.Join(homeDir, ".gemini", "settings.json")
		} else {
			configPath = filepath.Join(dir, ".gemini", "settings.json")
		}
		tmplName = "gemini"
		instFileName = "GEMINI.md"

	case string(ToolOpenCode):
		configPath = filepath.Join(dir, "opencode.json")
		tmplName = "codex"
		instFileName = "AGENTS.md"
	}

	return configPath, isTOML, tmplName, instFileName
}

func configureMCPFile(configPath string, isTOML bool, tool, binPath string, opts Options) (*FileChange, error) {
	action := "create"
	var existingContent []byte
	exists := false

	if data, err := os.ReadFile(configPath); err == nil {
		exists = true
		existingContent = data
		action = "modify"
	}

	var newContent []byte
	var backupPath string

	if isTOML {
		// TOML format for Codex
		var doc map[string]any
		if exists {
			if err := toml.Unmarshal(existingContent, &doc); err != nil {
				return nil, fmt.Errorf("existing TOML config at %q is malformed: %w", configPath, err)
			}
		} else {
			doc = make(map[string]any)
		}

		mcpServers, _ := doc["mcp_servers"].(map[string]any)
		if mcpServers == nil {
			mcpServers = make(map[string]any)
			doc["mcp_servers"] = mcpServers
		}

		if opts.Uninstall {
			delete(mcpServers, "go-anything")
			action = "remove"
		} else {
			mcpServers["go-anything"] = map[string]any{
				"command": binPath,
				"args":    []string{"mcp"},
			}
		}

		b, err := toml.Marshal(doc)
		if err != nil {
			return nil, err
		}
		newContent = b
	} else if tool == string(ToolOpenCode) {
		// OpenCode json format: { "mcp": { "go-anything": { "type": "local", "command": [...] } } }
		var doc map[string]any
		if exists {
			if err := json.Unmarshal(existingContent, &doc); err != nil {
				return nil, fmt.Errorf("existing JSON config at %q is malformed: %w", configPath, err)
			}
		} else {
			doc = make(map[string]any)
		}

		mcpMap, _ := doc["mcp"].(map[string]any)
		if mcpMap == nil {
			mcpMap = make(map[string]any)
			doc["mcp"] = mcpMap
		}

		if opts.Uninstall {
			delete(mcpMap, "go-anything")
			action = "remove"
		} else {
			mcpMap["go-anything"] = map[string]any{
				"type":    "local",
				"command": []string{binPath, "mcp"},
			}
		}

		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		newContent = append(b, '\n')
	} else {
		// Standard JSON format for Claude, Cursor, Gemini: { "mcpServers": { "go-anything": { "command": ..., "args": [...] } } }
		var doc map[string]any
		if exists {
			if err := json.Unmarshal(existingContent, &doc); err != nil {
				return nil, fmt.Errorf("existing JSON config at %q is malformed: %w", configPath, err)
			}
		} else {
			doc = make(map[string]any)
		}

		mcpServers, _ := doc["mcpServers"].(map[string]any)
		if mcpServers == nil {
			mcpServers = make(map[string]any)
			doc["mcpServers"] = mcpServers
		}

		if opts.Uninstall {
			delete(mcpServers, "go-anything")
			action = "remove"
		} else {
			mcpServers["go-anything"] = map[string]any{
				"command": binPath,
				"args":    []string{"mcp"},
			}
		}

		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		newContent = append(b, '\n')
	}

	diffStr := fmt.Sprintf("Updated %s with go-anything MCP entry", configPath)

	if !opts.DryRun || opts.Yes {
		// Create backup if modifying existing file
		if exists {
			backupPath = configPath + ".bak"
			_ = os.WriteFile(backupPath, existingContent, 0644)
		}

		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(configPath, newContent, 0644); err != nil {
			return nil, err
		}
	}

	return &FileChange{
		Path:       configPath,
		Action:     action,
		BackupPath: backupPath,
		Diff:       diffStr,
	}, nil
}

func configureInstructionFile(instPath, tmplName, dir string, opts Options) (*FileChange, error) {
	if _, err := os.Stat(instPath); err == nil {
		// Existing instruction file: merge / preserve without overwriting unless forced
		return &FileChange{
			Path:   instPath,
			Action: "unchanged",
			Diff:   fmt.Sprintf("Existing instruction file %s preserved", instPath),
		}, nil
	}

	reg := templates.DefaultRegistry()
	projName := opts.ProjectName
	if projName == "" {
		projName = filepath.Base(dir)
	}

	vars := templates.Vars{
		ProjectName:    projName,
		ModulePath:     projName,
		TargetLanguage: "go",
		Profile:        "full",
	}

	rendered, err := reg.Render(tmplName, vars)
	if err != nil {
		return nil, err
	}

	if !opts.DryRun || opts.Yes {
		if err := os.MkdirAll(filepath.Dir(instPath), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(instPath, rendered.Content, 0644); err != nil {
			return nil, err
		}
	}

	return &FileChange{
		Path:        instPath,
		Action:      "create",
		Diff:        fmt.Sprintf("Created agent instruction file %s from %s template", instPath, tmplName),
		Instruction: tmplName,
	}, nil
}

func detectBinaryPath() string {
	execPath, err := os.Executable()
	if err == nil && execPath != "" {
		if abs, err := filepath.Abs(execPath); err == nil {
			return abs
		}
	}
	return "go-anything"
}
