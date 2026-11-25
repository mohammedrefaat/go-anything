package setup_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/setup"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetup_All5Tools_FreshInstallation(t *testing.T) {
	tempDir := t.TempDir()

	res, err := setup.Run(setup.Options{
		Tool:       "all",
		Dir:        tempDir,
		Yes:        true,
		BinaryPath: "/usr/local/bin/go-anything",
	})
	require.NoError(t, err)
	assert.False(t, res.DryRun)
	assert.NotEmpty(t, res.Changes)

	// 1. Claude Code
	claudePath := filepath.Join(tempDir, ".mcp.json")
	assert.FileExists(t, claudePath)
	claudeData, _ := os.ReadFile(claudePath)
	assert.Contains(t, string(claudeData), "go-anything")
	assert.FileExists(t, filepath.Join(tempDir, "CLAUDE.md"))

	// 2. Codex
	codexPath := filepath.Join(tempDir, ".codex", "config.toml")
	assert.FileExists(t, codexPath)
	codexData, _ := os.ReadFile(codexPath)
	assert.Contains(t, string(codexData), "[mcp_servers.go-anything]")
	assert.FileExists(t, filepath.Join(tempDir, "AGENTS.md"))

	// 3. Cursor
	cursorPath := filepath.Join(tempDir, ".cursor", "mcp.json")
	assert.FileExists(t, cursorPath)
	cursorData, _ := os.ReadFile(cursorPath)
	assert.Contains(t, string(cursorData), "go-anything")
	assert.FileExists(t, filepath.Join(tempDir, ".cursorrules"))

	// 4. Gemini
	geminiPath := filepath.Join(tempDir, ".gemini", "settings.json")
	assert.FileExists(t, geminiPath)
	geminiData, _ := os.ReadFile(geminiPath)
	assert.Contains(t, string(geminiData), "go-anything")
	assert.FileExists(t, filepath.Join(tempDir, "GEMINI.md"))

	// 5. OpenCode
	openCodePath := filepath.Join(tempDir, "opencode.json")
	assert.FileExists(t, openCodePath)
	openCodeData, _ := os.ReadFile(openCodePath)
	assert.Contains(t, string(openCodeData), "go-anything")
}

func TestSetup_MergePreservesExistingEntriesAndBacksUp(t *testing.T) {
	tempDir := t.TempDir()

	// Pre-create existing Claude .mcp.json with another server
	claudePath := filepath.Join(tempDir, ".mcp.json")
	existingContent := `{
  "mcpServers": {
    "weather-server": {
      "command": "weather-cli",
      "args": ["serve"]
    }
  }
}`
	require.NoError(t, os.WriteFile(claudePath, []byte(existingContent), 0644))

	// Pre-create existing Codex TOML with another server
	codexDir := filepath.Join(tempDir, ".codex")
	require.NoError(t, os.MkdirAll(codexDir, 0755))
	codexPath := filepath.Join(codexDir, "config.toml")
	existingTOML := `[mcp_servers.database]
command = "db-mcp"
args = ["run"]
`
	require.NoError(t, os.WriteFile(codexPath, []byte(existingTOML), 0644))

	// Run setup for Claude and Codex
	_, err := setup.Run(setup.Options{
		Tool: "claude-code",
		Dir:  tempDir,
		Yes:  true,
	})
	require.NoError(t, err)

	_, err = setup.Run(setup.Options{
		Tool: "codex",
		Dir:  tempDir,
		Yes:  true,
	})
	require.NoError(t, err)

	// Check Claude: both servers exist, backup exists
	assert.FileExists(t, claudePath+".bak")
	var claudeDoc map[string]any
	cData, _ := os.ReadFile(claudePath)
	require.NoError(t, json.Unmarshal(cData, &claudeDoc))
	servers := claudeDoc["mcpServers"].(map[string]any)
	assert.Contains(t, servers, "weather-server", "existing weather-server must be preserved")
	assert.Contains(t, servers, "go-anything", "go-anything must be added")

	// Check Codex: both servers exist, backup exists
	assert.FileExists(t, codexPath+".bak")
	var codexDoc map[string]any
	tData, _ := os.ReadFile(codexPath)
	require.NoError(t, toml.Unmarshal(tData, &codexDoc))
	codexServers := codexDoc["mcp_servers"].(map[string]any)
	assert.Contains(t, codexServers, "database", "existing database server must be preserved")
	assert.Contains(t, codexServers, "go-anything", "go-anything must be added")
}

func TestSetup_MalformedConfigRefusesWithoutClobbering(t *testing.T) {
	tempDir := t.TempDir()

	claudePath := filepath.Join(tempDir, ".mcp.json")
	require.NoError(t, os.WriteFile(claudePath, []byte("{ malformed json ..."), 0644))

	_, err := setup.Run(setup.Options{
		Tool: "claude-code",
		Dir:  tempDir,
		Yes:  true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed")

	// Verify original file was NOT overwritten
	content, _ := os.ReadFile(claudePath)
	assert.Equal(t, "{ malformed json ...", string(content))
}

func TestSetup_Uninstall_RemovesOnlyGoAnything(t *testing.T) {
	tempDir := t.TempDir()

	// Install all
	_, err := setup.Run(setup.Options{
		Tool: "all",
		Dir:  tempDir,
		Yes:  true,
	})
	require.NoError(t, err)

	// Uninstall all
	res, err := setup.Run(setup.Options{
		Tool:      "all",
		Dir:       tempDir,
		Uninstall: true,
		Yes:       true,
	})
	require.NoError(t, err)
	assert.True(t, res.Uninstall)

	// Verify go-anything entry is removed from .mcp.json
	claudePath := filepath.Join(tempDir, ".mcp.json")
	var doc map[string]any
	data, _ := os.ReadFile(claudePath)
	require.NoError(t, json.Unmarshal(data, &doc))
	servers := doc["mcpServers"].(map[string]any)
	assert.NotContains(t, servers, "go-anything")
}

func TestSetup_DryRun_DoesNotModifyDisk(t *testing.T) {
	tempDir := t.TempDir()

	res, err := setup.Run(setup.Options{
		Tool:   "claude-code",
		Dir:    tempDir,
		DryRun: true,
		Yes:    false,
	})
	require.NoError(t, err)
	assert.True(t, res.DryRun)

	// Disk should NOT have .mcp.json
	claudePath := filepath.Join(tempDir, ".mcp.json")
	assert.NoFileExists(t, claudePath)
}
