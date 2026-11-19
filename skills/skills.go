// Package skills provides AI coding agent skill definitions and tool integration assets.
//
// A skill is a small, reusable playbook an agent can load when a task matches
// its description. The built-in skills mirror the go-anything workflow that the
// MCP server enforces.
package skills

import (
	"fmt"
	"sort"
	"strings"
)

// Skill is a reusable agent playbook.
type Skill struct {
	// ID is the stable identifier (e.g. "migrate-unit").
	ID string
	// Name is the human-readable title.
	Name string
	// Description explains when an agent should load the skill.
	Description string
	// Triggers lists example phrases or task shapes that match the skill.
	Triggers []string
	// Instructions is the Markdown body the agent follows.
	Instructions string
}

// DefaultSkills returns the built-in go-anything skills.
func DefaultSkills() []Skill {
	return []Skill{
		{
			ID:          "migrate-unit",
			Name:        "Migrate one unit",
			Description: "Port a single Ready unit to Go and verify it against recorded behavior.",
			Triggers: []string{
				"migrate a unit",
				"port this package",
				"continue the migration",
			},
			Instructions: `# Migrate one unit

1. Call ` + "`status`" + ` and pick the next Ready unit in dependency order.
2. Call ` + "`claim_unit`" + ` with your actor identifier.
3. Call ` + "`inspect_unit`" + `. Read the original source, recorded cases, and
   traps before writing code.
4. Write the Go implementation. Keep the original's public surface and error
   messages; do not change behavior that recorded cases cover.
5. Call ` + "`verify_diff`" + ` and iterate until it returns PASS.
6. Call ` + "`run_gate`" + `. Fix every failure; do not lower the gate.
7. Call ` + "`mark_verified`" + `. If it is rejected, the evidence is missing or
   stale — rerun ` + "`run_gate`" + ` for the current commit.
8. Call ` + "`handoff`" + ` with a summary and next steps.

Never call ` + "`approve`" + `, ` + "`promote`" + `, or ` + "`lift`" + `; those require a human.
`,
		},
		{
			ID:          "verify-parity",
			Name:        "Verify parity",
			Description: "Replay recorded cases and explain any mismatches instead of guessing.",
			Triggers: []string{
				"verify parity",
				"why does replay fail",
				"compare against the original",
			},
			Instructions: `# Verify parity

1. Confirm the cases come from the current source revision.
2. Replay with ` + "`verify_diff`" + `. Results are sorted by ` + "`case_id`" + `, never by
   completion time.
3. For each mismatch, read the expected and actual payloads and classify it:
   - real behavior change (fix the port),
   - volatile field that needs a wildcard rule,
   - missing trap case (add a case through the recorder, not by hand).
4. Re-run until PASS. Then record the limitation: parity covers recorded and
   fuzzed inputs only.
`,
		},
		{
			ID:          "handoff-session",
			Name:        "Hand off a session",
			Description: "Write durable state so the next agent or reviewer can continue without chat history.",
			Triggers: []string{
				"hand off",
				"end the session",
				"write a handoff",
			},
			Instructions: `# Hand off a session

1. Call ` + "`status`" + ` and note Verified, Planned, and Blocked units.
2. Call ` + "`handoff`" + ` with:
   - a summary of what changed,
   - the next unit in dependency order,
   - any open question that needs an ADR.
3. Do not mark units Verified by hand and do not remove leases that another
   agent still holds.
`,
		},
	}
}

// All returns the built-in skills sorted by ID.
func All() []Skill {
	out := DefaultSkills()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns a built-in skill by ID.
func Get(id string) (Skill, bool) {
	for _, s := range DefaultSkills() {
		if s.ID == id {
			return s, true
		}
	}
	return Skill{}, false
}

// RenderMarkdown renders one skill as Markdown.
func RenderMarkdown(s Skill) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", s.Name))
	sb.WriteString(fmt.Sprintf("> Skill ID: `%s`\n\n", s.ID))
	sb.WriteString(s.Description)
	sb.WriteString("\n\n")
	if len(s.Triggers) > 0 {
		sb.WriteString("**Use when:**\n\n")
		for _, t := range s.Triggers {
			sb.WriteString(fmt.Sprintf("- %s\n", t))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(strings.TrimSpace(s.Instructions))
	sb.WriteString("\n")
	return sb.String()
}

// RenderAllMarkdown renders every built-in skill into a single Markdown document.
func RenderAllMarkdown() string {
	var sb strings.Builder
	sb.WriteString("# go-anything agent skills\n\n")
	sb.WriteString("Load a skill when a task matches its description.\n\n")
	for i, s := range All() {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString(RenderMarkdown(s))
	}
	return sb.String()
}
