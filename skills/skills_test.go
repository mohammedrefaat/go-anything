package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSkills_AreComplete(t *testing.T) {
	all := All()
	require.Len(t, all, 3)

	seen := make(map[string]bool)
	for _, s := range all {
		t.Run(s.ID, func(t *testing.T) {
			assert.NotEmpty(t, s.Name)
			assert.NotEmpty(t, s.Description)
			assert.NotEmpty(t, s.Instructions)
			assert.NotEmpty(t, s.Triggers)
			assert.False(t, seen[s.ID], "duplicate skill ID %q", s.ID)
			seen[s.ID] = true
		})
	}

	assert.Equal(t, []string{"handoff-session", "migrate-unit", "verify-parity"}, []string{
		all[0].ID, all[1].ID, all[2].ID,
	}, "skills must be sorted by ID")
}

func TestGet(t *testing.T) {
	s, ok := Get("migrate-unit")
	require.True(t, ok)
	assert.Contains(t, s.Instructions, "claim_unit")
	assert.Contains(t, s.Instructions, "mark_verified")

	_, ok = Get("nope")
	assert.False(t, ok)
}

func TestRenderMarkdown(t *testing.T) {
	s, ok := Get("verify-parity")
	require.True(t, ok)

	md := RenderMarkdown(s)
	assert.True(t, strings.HasPrefix(md, "# Verify parity"))
	assert.Contains(t, md, "Skill ID: `verify-parity`")
	assert.Contains(t, md, "Use when:")
	assert.Contains(t, md, "parity covers recorded and")
}

func TestRenderAllMarkdown(t *testing.T) {
	md := RenderAllMarkdown()
	assert.Contains(t, md, "# go-anything agent skills")
	for _, s := range All() {
		assert.Contains(t, md, s.ID)
	}
	assert.Equal(t, strings.Count(md, "Skill ID:"), len(All()))
}
