// Package templates provides agent instruction templates and project scaffolding templates.
//
// Templates are pluggable: built-in agent instruction files (CLAUDE.md,
// .cursorrules, AGENTS.md, GEMINI.md) are registered on the default registry and
// callers may register additional templates without touching this package.
package templates

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	"text/template"
)

// Vars carries the values available to every template.
type Vars struct {
	// ProjectName is the migrated application name.
	ProjectName string
	// ModulePath is the Go module path of the workspace, when known.
	ModulePath string
	// TargetLanguage is the migration target (always "go" today).
	TargetLanguage string
	// Profile is the evidence profile required by the workspace.
	Profile string
}

// Template is a single renderable agent instruction file.
type Template struct {
	// Name is the stable identifier used on the command line (e.g. "claude").
	Name string
	// FileName is the file written into the workspace (e.g. "CLAUDE.md").
	FileName string
	// Description explains what the template is for.
	Description string
	// Body is a text/template executed with Vars.
	Body string
}

// RenderedFile is the result of rendering a template.
type RenderedFile struct {
	Name     string
	FileName string
	Content  []byte
}

// Registry holds templates by name. It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]Template
	order     []string
}

// NewRegistry returns an empty template registry.
func NewRegistry() *Registry {
	return &Registry{templates: make(map[string]Template)}
}

// Register adds or replaces a template. A template must have a name, a file
// name, and a parsable body.
func (r *Registry) Register(t Template) error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("templates: template name cannot be empty")
	}
	if strings.TrimSpace(t.FileName) == "" {
		return fmt.Errorf("templates: template %q has an empty file name", t.Name)
	}
	if _, err := template.New(t.Name).Parse(t.Body); err != nil {
		return fmt.Errorf("templates: template %q is invalid: %w", t.Name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.templates[t.Name]; !exists {
		r.order = append(r.order, t.Name)
	}
	r.templates[t.Name] = t
	return nil
}

// Get returns a template by name.
func (r *Registry) Get(name string) (Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.templates[name]
	return t, ok
}

// Names returns the registered template names in deterministic order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, len(r.order))
	copy(names, r.order)
	sort.Strings(names)
	return names
}

// Render renders a single template by name.
func (r *Registry) Render(name string, vars Vars) (RenderedFile, error) {
	t, ok := r.Get(name)
	if !ok {
		return RenderedFile{}, fmt.Errorf("templates: unknown template %q", name)
	}
	return renderTemplate(t, vars)
}

// RenderAll renders every registered template in deterministic name order.
func (r *Registry) RenderAll(vars Vars) ([]RenderedFile, error) {
	names := r.Names()
	files := make([]RenderedFile, 0, len(names))
	for _, name := range names {
		file, err := r.Render(name, vars)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func renderTemplate(t Template, vars Vars) (RenderedFile, error) {
	vars = withDefaults(vars)

	tmpl, err := template.New(t.Name).Option("missingkey=error").Parse(t.Body)
	if err != nil {
		return RenderedFile{}, fmt.Errorf("templates: failed to parse %q: %w", t.Name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return RenderedFile{}, fmt.Errorf("templates: failed to render %q: %w", t.Name, err)
	}

	content := strings.TrimRight(buf.String(), "\n") + "\n"
	return RenderedFile{
		Name:     t.Name,
		FileName: t.FileName,
		Content:  []byte(content),
	}, nil
}

func withDefaults(vars Vars) Vars {
	if strings.TrimSpace(vars.ProjectName) == "" {
		vars.ProjectName = "the migrated project"
	}
	if strings.TrimSpace(vars.TargetLanguage) == "" {
		vars.TargetLanguage = "go"
	}
	if strings.TrimSpace(vars.Profile) == "" {
		vars.Profile = "full"
	}
	return vars
}

// DefaultRegistry returns a registry preloaded with the built-in agent
// instruction templates.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	for _, t := range DefaultAgentTemplates() {
		// Built-in templates are covered by tests; a parse failure here is a
		// programming error, so surface it loudly during development.
		if err := r.Register(t); err != nil {
			panic(err)
		}
	}
	return r
}

// DefaultAgentTemplates returns the built-in instruction files for the
// supported coding agents.
func DefaultAgentTemplates() []Template {
	return []Template{
		{
			Name:        "claude",
			FileName:    "CLAUDE.md",
			Description: "Anthropic Claude Code project instructions.",
			Body:        agentInstructions("Claude Code", "CLAUDE.md"),
		},
		{
			Name:        "cursor",
			FileName:    ".cursorrules",
			Description: "Cursor project rules.",
			Body:        agentInstructions("Cursor", ".cursorrules"),
		},
		{
			Name:        "codex",
			FileName:    "AGENTS.md",
			Description: "OpenAI Codex / generic AGENTS.md instructions.",
			Body:        agentInstructions("Codex", "AGENTS.md"),
		},
		{
			Name:        "gemini",
			FileName:    "GEMINI.md",
			Description: "Google Gemini project instructions.",
			Body:        agentInstructions("Gemini", "GEMINI.md"),
		},
		{
			Name:        "opencode",
			FileName:    "AGENTS.md",
			Description: "OpenCode project instructions.",
			Body:        agentInstructions("OpenCode", "AGENTS.md"),
		},
	}
}

func agentInstructions(agentName, fileName string) string {
	return `# {{.ProjectName}} — agent instructions (` + agentName + `)

This file was generated by ` + "`go-anything template`" + `. It is safe to edit;
re-running the command overwrites it only when ` + "`--force`" + ` is passed.

## What this repository is

` + "`{{.ProjectName}}`" + ` is being migrated to {{.TargetLanguage}} with go-anything.
go-anything does not translate code automatically. It organizes the migration into
dependency waves, records the original's behavioral evidence, replays it against the port,
and enforces quality gates. The evidence profile for this workspace is ` + "`{{.Profile}}`" + `.

**Parity is limited.** Passing recorded cases is evidence for those cases only.
Never claim the port is equivalent for behavior that was not observed.

## Standard agent loop

1. ` + "`status`" + ` — inspect migration progress, active leases, blockers, and next unit in wave order.
2. ` + "`claim_unit`" + ` — take a Ready unit with a time-bounded local lease.
3. ` + "`inspect_unit`" + ` — read original source, sample cases, detected traps, and constraints.
4. Write Go port — implement the unit cleanly inside its declared scope. Scaffolds return ` + "`ErrNotPorted`" + ` and never generate business logic.
5. ` + "`verify_diff`" + ` — build and replay; fix until it returns PASS (0 mismatches).
6. ` + "`run_gate`" + ` — run full quality checks (build, format, vet, race, vulnerability, replay, error-paths, api-compat).
7. ` + "`mark_verified`" + ` — advance the unit to Verified. This is rejected unless commit-tied evidence exists for HEAD.
8. ` + "`handoff`" + ` — write HANDOFF.md with state, progress, and next steps for the next session or reviewer.

For hot-path optimizations and strangler sidecars:
` + "`profile`" + ` → ` + "`bridge`" + ` → ` + "`go`" + ` → verify diff → ` + "`gate`" + ` → ` + "`shadow`" + ` → human approve.

Do not skip steps. Do not edit ` + "`.go-anything/units/`" + ` or evidence files by
hand; the state machine must advance through the tools.

## How to claim a unit

To claim work on a unit:
1. Run ` + "`status`" + ` to view the migration progress and find the next available unit in dependency wave order.
2. Ensure all dependency units are in ` + "`Verified`" + `, ` + "`Shadow`" + `, ` + "`Approved`" + `, ` + "`Cutover`" + `, or ` + "`Completed`" + ` state. Units can only be claimed when in ` + "`Ready`" + ` state.
3. Call ` + "`claim_unit`" + ` with ` + "`unit_id`" + ` and ` + "`actor`" + ` (your agent or developer ID).
4. This acquires a time-bounded local lease in ` + "`.go-anything/local/<id>.lease`" + ` under OS file lock.
5. If dependencies are not met or another lease is active, claiming is rejected fail-closed.

## Human-only controls (Rule R-1)

These controls are CLI-only and must NEVER be automated or approximated via MCP:

- ` + "`approve`" + ` — human operator sign-off.
- ` + "`promote`" + ` — routing change switching primary traffic to the Go port.
- ` + "`lift`" + ` — permanent removal of the bridge and the original.
- Any routing change, shadow toggle, or kill-switch override.

If a task reaches a human-only boundary, STOP immediately, print the exact CLI command for the human operator, and exit cleanly.

## How to read a gate scorecard

Running ` + "`run_gate`" + ` evaluates the port across all mandatory dimensions and generates a scorecard at ` + "`.go-anything/reports/scorecard.md`" + `:

| Check | What It Validates | Pass Condition |
| :--- | :--- | :--- |
| ` + "`build`" + ` | Clean compilation under target toolchain | Exit code 0, no compiler errors |
| ` + "`format`" + ` | Go code formatting standard | Matches ` + "`gofmt -s -w`" + ` |
| ` + "`vet`" + ` | Static analysis and correctness | Exit code 0, zero ` + "`go vet`" + ` issues |
| ` + "`race`" + ` | Concurrency race condition detection | Zero data races detected via ` + "`go test -race`" + ` |
| ` + "`govulncheck`" + ` | Dependency vulnerability audit | Zero known CVE vulnerabilities reported |
| ` + "`replay`" + ` | Behavioral equivalence on recorded cases | 100% match on recorded cases (0 mismatches) |
| ` + "`error_paths`" + ` | Negative and malformed input scenarios | Correct error types and exit codes |
| ` + "`api_compat`" + ` | Exported symbol & signature parity | Exact match on public types, functions, and interfaces |

Under the ` + "`full`" + ` profile, all required checks must report ` + "`PASS`" + ` (fail-closed). Any skipped or failed check blocks evidence generation.

## What "done" means

A unit or task is "done" only when all of the following conditions are met:
1. **Zero Divergence:** ` + "`verify_diff`" + ` confirms 100% passing cases with 0 mismatches across recorded and fuzzed inputs.
2. **Quality Gate Clean:** ` + "`run_gate`" + ` passes all required checks (` + "`build`" + `, ` + "`format`" + `, ` + "`vet`" + `, ` + "`race`" + `, ` + "`govulncheck`" + `, ` + "`replay`" + `, ` + "`error_paths`" + `, ` + "`api_compat`" + `).
3. **Commit-Tied Evidence:** Evidence JSON is saved to ` + "`.go-anything/evidence/`" + ` cryptographically tied to the HEAD git commit SHA.
4. **State Advanced:** Unit is transitioned to ` + "`Verified`" + ` (via ` + "`mark_verified`" + `) or ` + "`Completed`" + ` (after human cutover and zero fallback traffic).
5. **Clean Verification:** ` + "`gofmt -s -w .`" + `, ` + "`go vet ./...`" + `, and ` + "`go test -race ./...`" + ` pass with zero warnings or errors.
6. **Handoff Written:** ` + "`HANDOFF.md`" + ` documents what was ported, verified, and what the next legal human action is.
7. **No Over-Claiming:** Parity is claimed strictly for observed and fuzzed cases (Rule R-6).

## Unit lifecycle (10 states)

` + "```text" + `
Planned → Recorded → Ready → In Progress → Verified → Shadow → Approved → Cutover → Completed
                                   ↓
                               Blocked (needs human decision)
` + "```" + `

## Workspace layout

` + "```text" + `
.go-anything/
  manifest.json        Index of units, schema_version
  units/               One durable state file per unit (JSON)
  local/               Leases and locks (GITIGNORED)
  rules.json           Auto-detected wildcards and normalizers
  cases/               Redacted cases, one JSONL per unit
  reports/             Diffs, benchmarks, shadow mismatches, scorecard
  evidence/            Gate reports tied to a git commit
` + "```" + `

## Key semantic traps (Go target)

When porting to Go, generate or watch for cases covering:

| Category | Issue |
| :--- | :--- |
| Empty collections | nil slice → null vs empty slice → [] |
| HTML escaping | encoding/json escapes <, >, & |
| Map key order | Go sorts keys; originals may keep insertion order |
| Numbers | float64 default for JSON; int64 overflow, -0, NaN |
| Strings | UTF-16 vs UTF-8 offsets; rune vs byte slicing; \r\n vs \n |
| Time | Timezone handling, format strings, sub-second precision |
| Error paths | Malformed input, missing fields, non-zero exit codes |
| go.mod directive | Loop var semantics, GODEBUG, ServeMux |

## Security rules

- Redaction is fail-closed and core-owned. Never disable it.
- The sandbox has no network by default. ` + "`--allow-network`" + ` is a human decision.
- Recording executes original project code. Only work with repositories you trust.
- Never commit ` + "`.go-anything/local/`" + ` (leases and locks).

## When the plan is silent

Stop. Write a short ADR to ` + "`DECISIONS.md`" + ` (and ` + "`Docs/decisions/`" + `) with
the question, the options, and a recommended answer. Do not guess.

## Verification before claiming a task is done

` + "```sh" + `
gofmt -s -w .
go vet ./...
go test -race ./...
` + "```" + `

Report any limitation, such as a missing C toolchain for the race detector,
instead of silently skipping it.

<!-- generated by go-anything template; file: ` + fileName + ` -->
`
}
