// Package recorder captures behavioral cases from tests, doc examples,
// and execution traffic, detecting volatile fields via dual-run diffing.
package recorder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mohammedrefat/go-anything/internal/redactor"
	"github.com/mohammedrefat/go-anything/internal/sandbox"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// ExecutionFunc defines the function that executes an application or unit given an input.
type ExecutionFunc func(ctx context.Context, input adapterkit.CaseInput) (*adapterkit.CaseOutput, error)

// Config configures the Recorder engine.
type Config struct {
	WorkspaceRoot  string
	SourceRevision string
	Runtime        string
	SandboxRunner  sandbox.Runner
	Redactor       *redactor.Redactor
	Store          *state.Store
}

// RecordResult represents the outcome of recording a case with dual-run diffing.
type RecordResult struct {
	Case      *adapterkit.Case
	Wildcards []adapterkit.WildcardRule
	Run1      *adapterkit.CaseOutput
	Run2      *adapterkit.CaseOutput
}

// Recorder captures behavioral cases with dual-run volatility detection.
type Recorder struct {
	cfg         Config
	caseCounter uint64
	mu          sync.Mutex
}

// New returns a new Recorder instance.
func New(configs ...Config) *Recorder {
	var cfg Config
	if len(configs) > 0 {
		cfg = configs[0]
	}

	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = "."
	}
	if cfg.SourceRevision == "" {
		cfg.SourceRevision = "head"
	}
	if cfg.Runtime == "" {
		cfg.Runtime = "go1.22"
	}
	if cfg.SandboxRunner == nil {
		cfg.SandboxRunner = sandbox.New()
	}
	if cfg.Redactor == nil {
		cfg.Redactor = redactor.New()
	}
	if cfg.Store == nil {
		cfg.Store = state.NewStore(cfg.WorkspaceRoot)
	}

	return &Recorder{
		cfg: cfg,
	}
}

// RecordCase performs the full dual-run recording pipeline for a given input:
// 1. Redacts and applies format-preserving substitutions to input.
// 2. Runs the original application twice.
// 3. Redacts outputs.
// 4. Diffs the two executions to detect volatile fields (timestamps, UUIDs).
// 5. Appends the recorded case to .go-anything/cases/<unit>.jsonl.
// 6. Merges detected wildcards into .go-anything/rules.json.
func (r *Recorder) RecordCase(ctx context.Context, unitID string, input adapterkit.CaseInput, kind adapterkit.CaseKind, execFn ExecutionFunc) (*RecordResult, error) {
	if unitID == "" {
		return nil, errors.New("recorder: unitID cannot be empty")
	}
	if execFn == nil {
		return nil, errors.New("recorder: execution function cannot be nil")
	}

	// 1. Redact input with format-preserving substitution
	redactedInput, inputUnrep, err := r.redactInput(input)
	if err != nil {
		return nil, fmt.Errorf("failed to redact case input: %w", err)
	}

	// 2. Dual-run execution
	out1, err := execFn(ctx, *redactedInput)
	if err != nil {
		return nil, fmt.Errorf("first execution run failed: %w", err)
	}

	// Brief pause to ensure time-based volatile fields reliably advance across runs
	time.Sleep(15 * time.Millisecond)

	out2, err := execFn(ctx, *redactedInput)
	if err != nil {
		return nil, fmt.Errorf("second execution run failed: %w", err)
	}

	// 3. Redact outputs
	out1Redacted, out1Unrep, err := r.redactOutput(out1)
	if err != nil {
		return nil, fmt.Errorf("failed to redact run 1 output: %w", err)
	}

	out2Redacted, _, err := r.redactOutput(out2)
	if err != nil {
		return nil, fmt.Errorf("failed to redact run 2 output: %w", err)
	}

	// 4. Volatility detection (diff run 1 vs run 2)
	wildcards := DetectVolatileFields(out1Redacted, out2Redacted)

	// Collect and deduplicate wildcard paths
	wildcardPaths := make([]string, 0, len(wildcards))
	for _, w := range wildcards {
		wildcardPaths = append(wildcardPaths, w.Path)
	}

	// Collect unreplayable fields
	unrepMap := make(map[string]bool)
	for _, u := range inputUnrep {
		unrepMap[u] = true
	}
	for _, u := range out1Unrep {
		unrepMap[u] = true
	}
	var unreplayableFields []string
	for k := range unrepMap {
		unreplayableFields = append(unreplayableFields, k)
	}

	r.mu.Lock()
	if r.caseCounter == 0 {
		if caseData, err := os.ReadFile(filepath.Join(r.cfg.Store.CasesDir(), unitID+".jsonl")); err == nil {
			count := uint64(0)
			for _, line := range bytes.Split(caseData, []byte{'\n'}) {
				if len(bytes.TrimSpace(line)) > 0 {
					count++
				}
			}
			r.caseCounter = count
		}
	}
	r.caseCounter++
	caseNumber := r.caseCounter
	r.mu.Unlock()

	caseID := fmt.Sprintf("case-%s-%04d", unitID, caseNumber)

	recordCase := &adapterkit.Case{
		SchemaVersion:      adapterkit.SchemaVersionV1Alpha1,
		CaseID:             caseID,
		UnitID:             unitID,
		Kind:               kind,
		SourceRevision:     r.cfg.SourceRevision,
		Runtime:            r.cfg.Runtime,
		CapturedAt:         time.Now().UTC().Format(time.RFC3339),
		Input:              *redactedInput,
		ExpectedOutput:     *out1Redacted,
		Wildcards:          wildcardPaths,
		UnreplayableFields: unreplayableFields,
	}

	// 5. Persist case in .go-anything/cases/<unit>.jsonl
	if err := r.SaveCase(unitID, recordCase); err != nil {
		return nil, fmt.Errorf("failed to save recorded case: %w", err)
	}

	// 6. Update .go-anything/rules.json with new wildcards
	if len(wildcards) > 0 {
		if err := r.mergeWildcards(wildcards); err != nil {
			return nil, fmt.Errorf("failed to update rules.json: %w", err)
		}
	}

	return &RecordResult{
		Case:      recordCase,
		Wildcards: wildcards,
		Run1:      out1Redacted,
		Run2:      out2Redacted,
	}, nil
}

// RecordCommand records execution of a sandboxed CLI command.
func (r *Recorder) RecordCommand(ctx context.Context, unitID string, cmd string, args []string, kind adapterkit.CaseKind) (*RecordResult, error) {
	input := adapterkit.CaseInput{
		Argv: append([]string{cmd}, args...),
	}

	execFn := func(ctx context.Context, in adapterkit.CaseInput) (*adapterkit.CaseOutput, error) {
		var command string
		var cmdArgs []string
		if len(in.Argv) > 0 {
			command = in.Argv[0]
			cmdArgs = in.Argv[1:]
		} else {
			command = cmd
			cmdArgs = args
		}

		sandboxCfg := sandbox.Config{
			Command:         command,
			Args:            cmdArgs,
			WorkingDir:      r.cfg.WorkspaceRoot,
			TempWorkingCopy: true,
			AllowNetwork:    false,
			Timeout:         10 * time.Second,
		}

		res, err := r.cfg.SandboxRunner.Run(ctx, sandboxCfg)
		if err != nil && !errors.Is(err, sandbox.ErrTimeout) {
			return nil, err
		}

		output := &adapterkit.CaseOutput{
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			ExitCode: res.ExitCode,
		}

		// If stdout is valid JSON, also set Body
		var parsed any
		if json.Unmarshal([]byte(res.Stdout), &parsed) == nil {
			output.Body = parsed
		}

		return output, nil
	}

	return r.RecordCase(ctx, unitID, input, kind, execFn)
}

// SaveCase appends a recorded case to .go-anything/cases/<unit>.jsonl.
func (r *Recorder) SaveCase(unitID string, c *adapterkit.Case) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	casesDir := r.cfg.Store.CasesDir()
	caseFile := filepath.Join(casesDir, filepath.FromSlash(unitID)+".jsonl")
	if err := os.MkdirAll(filepath.Dir(caseFile), 0755); err != nil {
		return fmt.Errorf("failed to create cases directory: %w", err)
	}

	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal case: %w", err)
	}

	f, err := os.OpenFile(caseFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open case file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write case to %s: %w", caseFile, err)
	}
	return f.Sync()
}

func (r *Recorder) mergeWildcards(newRules []adapterkit.WildcardRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existingRules, err := r.cfg.Store.GetRules()
	if err != nil || existingRules == nil {
		existingRules = &adapterkit.Rules{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			Wildcards:     []adapterkit.WildcardRule{},
		}
	}

	existingPaths := make(map[string]bool)
	for _, w := range existingRules.Wildcards {
		existingPaths[w.Path] = true
	}

	for _, w := range newRules {
		if !existingPaths[w.Path] {
			existingPaths[w.Path] = true
			existingRules.Wildcards = append(existingRules.Wildcards, w)
		}
	}

	return r.cfg.Store.SaveRules(existingRules)
}

func (r *Recorder) redactInput(in adapterkit.CaseInput) (*adapterkit.CaseInput, []string, error) {
	var unrep []string
	redacted := in

	if in.Headers != nil {
		h, u, err := r.cfg.Redactor.RedactHeaders(in.Headers)
		if err != nil {
			return nil, nil, err
		}
		redacted.Headers = h
		for _, x := range u {
			unrep = append(unrep, "input.headers."+x)
		}
	}

	if in.Body != nil {
		b, u, err := r.cfg.Redactor.RedactValue(in.Body, "input.body")
		if err != nil {
			return nil, nil, err
		}
		redacted.Body = b
		unrep = append(unrep, u...)
	}

	if len(in.Argv) > 0 {
		var newArgv []string
		for _, a := range in.Argv {
			red, _, _ := r.cfg.Redactor.RedactString(a)
			newArgv = append(newArgv, red)
		}
		redacted.Argv = newArgv
	}

	return &redacted, unrep, nil
}

func (r *Recorder) redactOutput(out *adapterkit.CaseOutput) (*adapterkit.CaseOutput, []string, error) {
	if out == nil {
		return nil, nil, nil
	}
	var unrep []string
	redacted := *out

	if out.Headers != nil {
		h, u, err := r.cfg.Redactor.RedactHeaders(out.Headers)
		if err != nil {
			return nil, nil, err
		}
		redacted.Headers = h
		for _, x := range u {
			unrep = append(unrep, "output.headers."+x)
		}
	}

	if out.Body != nil {
		b, u, err := r.cfg.Redactor.RedactValue(out.Body, "output.body")
		if err != nil {
			return nil, nil, err
		}
		redacted.Body = b
		unrep = append(unrep, u...)
	}

	if out.Stdout != "" {
		s, _, _ := r.cfg.Redactor.RedactString(out.Stdout)
		redacted.Stdout = s
	}

	if out.Stderr != "" {
		s, _, _ := r.cfg.Redactor.RedactString(out.Stderr)
		redacted.Stderr = s
	}

	return &redacted, unrep, nil
}
