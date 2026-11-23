package main

import (
	"os"
	"path/filepath"
	"testing"

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
	assert.Equal(t, expected, string(data), "STATUS.md on disk does not match generator output. Run 'go run ./tools/genstatus' to regenerate.")
}

func TestAllRegisteredCommandsValid(t *testing.T) {
	assert.NotEmpty(t, AllRegisteredCommands)
	assert.Equal(t, 22, len(AllRegisteredCommands), "expected exactly 22 commands documented")

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
