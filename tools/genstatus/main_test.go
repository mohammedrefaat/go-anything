package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusFileUpToDate(t *testing.T) {
	repoRoot, err := findRepoRoot()
	require.NoError(t, err)

	statusPath := filepath.Join(repoRoot, "STATUS.md")
	data, err := os.ReadFile(statusPath)
	require.NoError(t, err, "STATUS.md must exist in repo root. Run 'go run ./tools/genstatus' to generate it.")

	expected := GenerateStatusMarkdown()
	actual := strings.ReplaceAll(string(data), "\r\n", "\n")
	assert.Equal(t, expected, actual, "STATUS.md on disk does not match generator output. Run 'go run ./tools/genstatus' to regenerate.")
}

func TestMCPToolDocsUpToDate(t *testing.T) {
	repoRoot, err := findRepoRoot()
	require.NoError(t, err)

	expectedSection := GenerateMCPToolsSection()
	for _, rel := range mcpToolsTargets {
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(repoRoot, rel)
			raw, err := os.ReadFile(path)
			require.NoError(t, err)

			actual := strings.ReplaceAll(string(raw), "\r\n", "\n")
			updated, err := ReplaceMCPToolsBlock(actual, expectedSection)
			require.NoError(t, err)
			assert.Equal(t, updated, actual, "%s generated mcp-tools block is out of date. Run 'go run ./tools/genstatus' to regenerate.", rel)
		})
	}
}

func TestMCPToolNamesMatchDocs(t *testing.T) {
	repoRoot, err := findRepoRoot()
	require.NoError(t, err)

	expected := append([]string(nil), mcp.StandardToolNames...)
	sort.Strings(expected)

	registered := mcp.NewServer().RegisteredToolNames()
	assert.Equal(t, expected, registered, "server.RegisteredToolNames() must match mcp.StandardToolNames")

	var defNames []string
	for _, def := range mcp.GetToolDefinitions() {
		defNames = append(defNames, def.Name)
	}
	sort.Strings(defNames)
	assert.Equal(t, expected, defNames, "mcp.GetToolDefinitions() must match mcp.StandardToolNames")

	docFiles := append([]string{"STATUS.md"}, mcpToolsTargets...)
	rowPattern := regexp.MustCompile("(?m)^\\|\\s*`([a-z0-9_]+)`\\s*\\|\\s*Agent\\s*\\|")

	for _, rel := range docFiles {
		t.Run(rel, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
			require.NoError(t, err)

			normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
			start := strings.Index(normalized, mcpToolsBeginMarker)
			require.GreaterOrEqual(t, start, 0, "%s missing %s", rel, mcpToolsBeginMarker)
			end := strings.Index(normalized[start:], mcpToolsEndMarker)
			require.GreaterOrEqual(t, end, 0, "%s missing %s", rel, mcpToolsEndMarker)
			block := normalized[start : start+end]

			matches := rowPattern.FindAllStringSubmatch(block, -1)
			var docTools []string
			for _, m := range matches {
				docTools = append(docTools, m[1])
			}
			sort.Strings(docTools)
			assert.Equal(t, expected, docTools, "MCP tools documented in %s must match StandardToolNames and registered server tools", rel)
		})
	}
}

func TestAllRegisteredCommandsValid(t *testing.T) {
	assert.NotEmpty(t, AllRegisteredCommands)
	assert.Equal(t, 32, len(AllRegisteredCommands), "expected exactly 32 commands documented")

	seen := make(map[string]bool)
	for _, c := range AllRegisteredCommands {
		assert.NotEmpty(t, c.Name)
		assert.False(t, seen[c.Name], "duplicate command name: %s", c.Name)
		seen[c.Name] = true

		assert.Equal(t, "Fully Wired", c.WiringStatus, "command %s must be Fully Wired", c.Name)
		assert.True(t, c.E2ECovered, "command %s must be E2E covered", c.Name)
		assert.NotEmpty(t, c.Description)
		assert.NotEmpty(t, c.SinceVersion)
		assert.NotEmpty(t, c.Milestone)
		assert.NotEmpty(t, c.MCPCounterpart)
	}
}
