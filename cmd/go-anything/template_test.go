package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateCommand_List(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"template", "--list"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	for _, name := range []string{"claude", "cursor", "codex", "gemini"} {
		assert.Contains(t, out, name)
	}
	assert.Contains(t, out, "CLAUDE.md")
	assert.Contains(t, out, ".cursorrules")
	assert.Contains(t, out, "AGENTS.md")
	assert.Contains(t, out, "GEMINI.md")
}

func TestTemplateCommand_StdoutRendersEveryAgentFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"template", "--stdout", "--name=payments"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	for _, fileName := range []string{"CLAUDE.md", ".cursorrules", "AGENTS.md", "GEMINI.md"} {
		assert.Contains(t, out, "=== "+fileName+" ===")
	}
	// All four files must carry the enforced agent loop.
	assert.Equal(t, 4, bytes.Count([]byte(out), []byte("mark_verified")))
	assert.Contains(t, out, "payments")
}

func TestTemplateCommand_WritesFilesToWorkspace(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/demo\n\ngo 1.27.0\n"), 0644))

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"template", dir, "--skills"}, &stdout, &stderr)
	require.NoError(t, err, "stderr: %s", stderr.String())

	for _, fileName := range []string{"CLAUDE.md", ".cursorrules", "AGENTS.md", "GEMINI.md"} {
		path := filepath.Join(dir, fileName)
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr, "expected %s to be written", fileName)
		assert.Contains(t, string(data), "go-anything")
	}

	skillsPath := filepath.Join(dir, ".go-anything", "skills.md")
	skillsData, err := os.ReadFile(skillsPath)
	require.NoError(t, err)
	assert.Contains(t, string(skillsData), "migrate-unit")
	assert.Contains(t, stdout.String(), "rendered claude")
	assert.Contains(t, stdout.String(), "profile: full")
}

func TestTemplateCommand_RefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	require.NoError(t, runCLI([]string{"template", dir, "--agent=claude"}, &stdout, &stderr))

	stdout.Reset()
	stderr.Reset()
	err := runCLI([]string{"template", dir, "--agent=claude"}, &stdout, &stderr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite existing files")
	assert.Contains(t, err.Error(), "CLAUDE.md")

	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"template", dir, "--agent=claude", "--force"}, &stdout, &stderr)
	require.NoError(t, err)
}

func TestTemplateCommand_AgentSelection(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"template", dir, "--agent=cursor,gemini"}, &stdout, &stderr)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, ".cursorrules"))
	assert.FileExists(t, filepath.Join(dir, "GEMINI.md"))
	assert.NoFileExists(t, filepath.Join(dir, "CLAUDE.md"))
	assert.NoFileExists(t, filepath.Join(dir, "AGENTS.md"))
}

func TestTemplateCommand_UnknownAgent(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"template", dir, "--agent=vim"}, &stdout, &stderr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown agent template "vim"`)
}
