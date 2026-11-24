package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAgentTemplates_RegisterAndRender(t *testing.T) {
	registry := DefaultRegistry()

	tests := []struct {
		name         string
		wantFileName string
	}{
		{name: "claude", wantFileName: "CLAUDE.md"},
		{name: "cursor", wantFileName: ".cursorrules"},
		{name: "codex", wantFileName: "AGENTS.md"},
		{name: "gemini", wantFileName: "GEMINI.md"},
		{name: "opencode", wantFileName: "AGENTS.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, ok := registry.Get(tt.name)
			require.True(t, ok, "template %q should be registered", tt.name)
			assert.Equal(t, tt.wantFileName, tmpl.FileName)

			file, err := registry.Render(tt.name, Vars{
				ProjectName: "payments",
				Profile:     "full",
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantFileName, file.FileName)

			body := string(file.Content)
			assert.Contains(t, body, "payments")
			assert.Contains(t, body, "full")
			// Every agent file must enforce the agent loop, human-only controls,
			// lifecycle/layout, scorecard reading, claim process, and semantic traps checklist.
			for _, marker := range []string{
				"status", "claim_unit", "inspect_unit", "verify_diff",
				"run_gate", "mark_verified", "handoff",
				"approve", "promote", "lift",
				"DECISIONS.md",
				"Unit lifecycle",
				"Workspace layout",
				"Key semantic traps",
				"Empty collections",
				"HTML escaping",
				"How to claim a unit",
				"How to read a gate scorecard",
				`What "done" means`,
				"Human-only controls (Rule R-1)",
				"build", "format", "vet", "race", "govulncheck", "replay", "error_paths", "api_compat",
			} {
				assert.Contains(t, body, marker, "agent file should mention %q", marker)
			}
		})
	}
}

func TestRegistry_RenderAllIsDeterministic(t *testing.T) {
	registry := DefaultRegistry()

	first, err := registry.RenderAll(Vars{ProjectName: "svc"})
	require.NoError(t, err)
	second, err := registry.RenderAll(Vars{ProjectName: "svc"})
	require.NoError(t, err)

	require.Len(t, first, 5)
	assert.Equal(t, first, second, "rendering must be deterministic")

	names := registry.Names()
	assert.Equal(t, []string{"claude", "codex", "cursor", "gemini", "opencode"}, names)
}

func TestVars_Defaults(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Template{
		Name:     "probe",
		FileName: "PROBE.md",
		Body:     "project={{.ProjectName}} lang={{.TargetLanguage}} profile={{.Profile}}",
	}))

	file, err := registry.Render("probe", Vars{})
	require.NoError(t, err)
	assert.Equal(t, "project=the migrated project lang=go profile=full\n", string(file.Content))
}

func TestRegistry_Validation(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    Template
		wantErr string
	}{
		{
			name:    "empty name",
			tmpl:    Template{FileName: "x.md", Body: "x"},
			wantErr: "name cannot be empty",
		},
		{
			name:    "empty file name",
			tmpl:    Template{Name: "x", Body: "x"},
			wantErr: "empty file name",
		},
		{
			name:    "invalid body",
			tmpl:    Template{Name: "x", FileName: "x.md", Body: "{{.Broken"},
			wantErr: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewRegistry().Register(tt.tmpl)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRegistry_PluggableTemplate(t *testing.T) {
	registry := DefaultRegistry()
	require.NoError(t, registry.Register(Template{
		Name:        "acme",
		FileName:    ".acme-rules",
		Description: "custom",
		Body:        "custom instructions for {{.ProjectName}}",
	}))

	file, err := registry.Render("acme", Vars{ProjectName: "widgets"})
	require.NoError(t, err)
	assert.Equal(t, ".acme-rules", file.FileName)
	assert.Equal(t, "custom instructions for widgets\n", string(file.Content))

	_, err = registry.Render("does-not-exist", Vars{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown template")
}

func TestRender_TrimsTrailingNewlines(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Template{
		Name:     "trim",
		FileName: "trim.md",
		Body:     "line\n\n\n",
	}))

	file, err := registry.Render("trim", Vars{})
	require.NoError(t, err)
	assert.Equal(t, "line\n", string(file.Content))
	assert.False(t, strings.HasSuffix(string(file.Content), "\n\n"))
}

func TestTemplates_GoldenOutput(t *testing.T) {
	registry := DefaultRegistry()
	goldenDir := filepath.Join("testdata", "golden")
	require.NoError(t, os.MkdirAll(goldenDir, 0755))

	vars := Vars{
		ProjectName:    "payments",
		ModulePath:     "github.com/example/payments",
		TargetLanguage: "go",
		Profile:        "full",
	}

	for _, name := range registry.Names() {
		file, err := registry.Render(name, vars)
		require.NoError(t, err)

		goldenFile := filepath.Join(goldenDir, name+".golden.md")
		if _, err := os.Stat(goldenFile); os.IsNotExist(err) {
			// Initialize golden file if missing
			require.NoError(t, os.WriteFile(goldenFile, file.Content, 0644))
		}

		expected, err := os.ReadFile(goldenFile)
		require.NoError(t, err)
		assert.Equal(t, string(expected), string(file.Content), "rendered template %q must match golden file %q", name, goldenFile)
	}
}
