package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/setup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_Setup_DryRun(t *testing.T) {
	tempDir := t.TempDir()

	global := &GlobalFlags{Jobs: 1}
	var stdout, stderr bytes.Buffer

	// Default should run in dry-run mode
	err := runSetup(global, []string{"--dir", tempDir, "--tool", "claude-code"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "DRY-RUN")
	assert.Contains(t, out, "claude-code")

	// Config file should NOT be created on disk in dry-run mode
	configPath := filepath.Join(tempDir, ".mcp.json")
	_, err = os.Stat(configPath)
	assert.True(t, os.IsNotExist(err), "config file should not be created in dry run")
}

func TestCLI_Setup_ApplyAllTools(t *testing.T) {
	tempDir := t.TempDir()

	global := &GlobalFlags{Jobs: 1}
	var stdout, stderr bytes.Buffer

	err := runSetup(global, []string{"--dir", tempDir, "--tool", "all", "--yes"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "APPLIED")
	assert.Contains(t, out, "Configured agent integration")

	// Verify files created
	assert.FileExists(t, filepath.Join(tempDir, ".mcp.json"))
	assert.FileExists(t, filepath.Join(tempDir, ".cursor", "mcp.json"))
	assert.FileExists(t, filepath.Join(tempDir, ".codex", "config.toml"))
	assert.FileExists(t, filepath.Join(tempDir, ".gemini", "settings.json"))
	assert.FileExists(t, filepath.Join(tempDir, "opencode.json"))

	// Verify instruction files created
	assert.FileExists(t, filepath.Join(tempDir, "CLAUDE.md"))
	assert.FileExists(t, filepath.Join(tempDir, "AGENTS.md"))
	assert.FileExists(t, filepath.Join(tempDir, ".cursorrules"))
	assert.FileExists(t, filepath.Join(tempDir, "GEMINI.md"))
}

func TestCLI_Setup_JSONOutput(t *testing.T) {
	tempDir := t.TempDir()

	global := &GlobalFlags{Jobs: 1}
	var stdout, stderr bytes.Buffer

	err := runSetup(global, []string{"--dir", tempDir, "--tool", "cursor", "--yes", "--json"}, &stdout, &stderr)
	require.NoError(t, err)

	var res setup.Result
	err = json.Unmarshal(stdout.Bytes(), &res)
	require.NoError(t, err)
	assert.Equal(t, "cursor", res.Tool)
	assert.False(t, res.DryRun)
	assert.NotEmpty(t, res.Changes)
}

func TestCLI_Setup_Uninstall(t *testing.T) {
	tempDir := t.TempDir()

	global := &GlobalFlags{Jobs: 1}
	var stdout, stderr bytes.Buffer

	// Apply first
	err := runSetup(global, []string{"--dir", tempDir, "--tool", "gemini", "--yes"}, &stdout, &stderr)
	require.NoError(t, err)

	geminiConfig := filepath.Join(tempDir, ".gemini", "settings.json")
	require.FileExists(t, geminiConfig)

	// Now uninstall
	stdout.Reset()
	stderr.Reset()
	err = runSetup(global, []string{"--dir", tempDir, "--tool", "gemini", "--uninstall", "--yes"}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Uninstalled agent integration")

	// Read content of settings.json - go-anything entry should be removed
	data, err := os.ReadFile(geminiConfig)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "go-anything")
}
