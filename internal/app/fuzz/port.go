package fuzz

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	cliadapter "github.com/mohammedrefaat/go-anything/adapters/application/cli"
	httpadapter "github.com/mohammedrefaat/go-anything/adapters/application/http"
	"github.com/mohammedrefaat/go-anything/internal/comparator"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// FuzzExecutionRequest defines the parameters passed to the fuzz engine.
type FuzzExecutionRequest struct {
	UnitID        string
	Time          time.Duration
	Runs          int
	Seed          int64
	MaxInputBytes int
	Dir           string
	Target        string
	SeedCases     []*adapterkit.Case
}

// FuzzExecutionResult contains the outcomes of executing fuzzed cases across legacy and modern targets.
type FuzzExecutionResult struct {
	TotalRuns        int
	DivergencesCount int
	Divergences      []DivergenceRecord
	DivergentCases   []*adapterkit.Case
}

// FuzzPort abstracts seed loading, differential execution, case promotion, and report persistence.
type FuzzPort interface {
	LoadSeedCases(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error)
	ExecuteFuzz(ctx context.Context, req FuzzExecutionRequest) (*FuzzExecutionResult, error)
	PromoteCases(ctx context.Context, dir, unitID string, cases []*adapterkit.Case) error
	SaveReport(ctx context.Context, dir, unitID string, out *Output) error
}

// DefaultFuzzPort implements FuzzPort using case files, replay drivers, and internal/comparator.
type DefaultFuzzPort struct{}

// NewDefaultFuzzPort creates a new DefaultFuzzPort.
func NewDefaultFuzzPort() *DefaultFuzzPort {
	return &DefaultFuzzPort{}
}

// LoadSeedCases reads existing test cases for the unit from .go-anything/cases/<unit>.jsonl.
func (p *DefaultFuzzPort) LoadSeedCases(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error) {
	casesPath := filepath.Join(dir, ".go-anything", "cases", filepath.FromSlash(unitID)+".jsonl")
	file, err := os.Open(casesPath)
	if err != nil && os.IsNotExist(err) {
		sanitized := strings.ReplaceAll(unitID, "/", "_")
		casesPath = filepath.Join(dir, ".go-anything", "cases", sanitized+".jsonl")
		file, err = os.Open(casesPath)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open cases file: %w", err)
	}
	defer file.Close()

	var cases []*adapterkit.Case
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c adapterkit.Case
		if err := json.Unmarshal([]byte(line), &c); err == nil {
			cases = append(cases, &c)
		}
	}
	return cases, nil
}

// ExecuteFuzz runs mutated input executions across legacy and modern targets.
func (p *DefaultFuzzPort) ExecuteFuzz(ctx context.Context, req FuzzExecutionRequest) (*FuzzExecutionResult, error) {
	seed := req.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rnd := rand.New(rand.NewSource(seed))

	seeds := req.SeedCases
	if len(seeds) == 0 {
		seeds = []*adapterkit.Case{
			{
				CaseID: "default-seed-1",
				UnitID: req.UnitID,
				Kind:   adapterkit.KindHappy,
				Input: adapterkit.CaseInput{
					Argv: []string{req.UnitID, "run"},
					Body: "{\"test\": true}",
				},
				ExpectedOutput: adapterkit.CaseOutput{
					Stdout:   "OK\n",
					ExitCode: 0,
				},
			},
		}
	}

	comp := comparator.New()
	targetDriver, err := p.resolveDriver(req.Dir, req.UnitID, req.Target)
	if err != nil {
		return nil, err
	}
	if targetDriver != nil {
		defer targetDriver.Close()
	}

	var divergences []DivergenceRecord
	var divergentCases []*adapterkit.Case
	totalRuns := 0

	runsLimit := req.Runs
	if runsLimit <= 0 && req.Time <= 0 {
		runsLimit = 50
	}

	var timeoutChan <-chan time.Time
	if req.Time > 0 {
		timer := time.NewTimer(req.Time)
		defer timer.Stop()
		timeoutChan = timer.C
	}

	for i := 0; ; i++ {
		if runsLimit > 0 && totalRuns >= runsLimit {
			break
		}
		if timeoutChan != nil {
			select {
			case <-timeoutChan:
				goto Done
			default:
			}
		}

		baseSeed := seeds[rnd.Intn(len(seeds))]
		mutatedInput := p.mutateInput(rnd, baseSeed.Input, req.MaxInputBytes)
		totalRuns++

		testCase := &adapterkit.Case{
			CaseID:         fmt.Sprintf("fuzz-%s-%04d", req.UnitID, totalRuns),
			UnitID:         req.UnitID,
			Kind:           adapterkit.KindFuzz,
			Input:          mutatedInput,
			ExpectedOutput: baseSeed.ExpectedOutput,
		}

		if targetDriver != nil {
			modernOutput, err := targetDriver.ExecuteCase(ctx, testCase)
			if err != nil {
				continue
			}

			res, err := comp.CompareCaseOutput(comparator.ModeExact, &testCase.ExpectedOutput, modernOutput)
			if err == nil && !res.Passed {
				div := DivergenceRecord{
					CaseID:       testCase.CaseID,
					Input:        mutatedInput,
					LegacyOutput: testCase.ExpectedOutput,
					ModernOutput: *modernOutput,
					Diff:         res.Mismatches,
				}
				divergences = append(divergences, div)
				divergentCases = append(divergentCases, testCase)
			}
		}
	}

Done:
	if divergences == nil {
		divergences = []DivergenceRecord{}
	}

	return &FuzzExecutionResult{
		TotalRuns:        totalRuns,
		DivergencesCount: len(divergences),
		Divergences:      divergences,
		DivergentCases:   divergentCases,
	}, nil
}

func (p *DefaultFuzzPort) resolveDriver(dir, unitID, target string) (adapterkit.Replay, error) {
	if target == "" {
		// Look up unit target metadata from store
		store := state.NewStore(dir)
		if u, err := store.GetUnit(unitID); err == nil && u.Metadata != nil {
			if t, ok := u.Metadata["target"].(string); ok && t != "" {
				target = t
			}
		}
	}
	if target == "" {
		return nil, nil
	}

	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return httpadapter.NewAdapter(httpadapter.WithBaseURL(target)), nil
	}
	return cliadapter.NewAdapter(target), nil
}

func (p *DefaultFuzzPort) mutateInput(rnd *rand.Rand, input adapterkit.CaseInput, maxBytes int) adapterkit.CaseInput {
	mutated := input

	// Mutate Argv
	if len(mutated.Argv) > 0 {
		newArgv := make([]string, len(mutated.Argv))
		copy(newArgv, mutated.Argv)
		idx := rnd.Intn(len(newArgv))
		newArgv[idx] = mutateString(rnd, newArgv[idx])
		mutated.Argv = newArgv
	}

	// Mutate Body if string
	if strBody, ok := mutated.Body.(string); ok && len(strBody) > 0 {
		mutated.Body = mutateString(rnd, strBody)
	}

	// Truncate if maxBytes specified
	if maxBytes > 0 {
		if strBody, ok := mutated.Body.(string); ok && len(strBody) > maxBytes {
			mutated.Body = strBody[:maxBytes]
		}
	}

	return mutated
}

func mutateString(rnd *rand.Rand, s string) string {
	strategy := rnd.Intn(6)
	switch strategy {
	case 0:
		return ""
	case 1:
		return "\x00\x00\x00"
	case 2:
		return "%s%d%v%x"
	case 3:
		return "日本語🚀🔥\r\n\t"
	case 4:
		return s + strings.Repeat("A", 64)
	default:
		if len(s) > 0 {
			idx := rnd.Intn(len(s))
			return s[:idx] + string(rune('a'+rnd.Intn(26))) + s[idx+1:]
		}
		return "fuzzed"
	}
}

// PromoteCases appends divergent test cases of kind "fuzz" to .go-anything/cases/<unit>.jsonl.
func (p *DefaultFuzzPort) PromoteCases(ctx context.Context, dir, unitID string, cases []*adapterkit.Case) error {
	if len(cases) == 0 {
		return nil
	}
	casesPath := filepath.Join(dir, ".go-anything", "cases", filepath.FromSlash(unitID)+".jsonl")
	if err := os.MkdirAll(filepath.Dir(casesPath), 0755); err != nil {
		return fmt.Errorf("failed to create cases dir: %w", err)
	}

	file, err := os.OpenFile(casesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open cases file: %w", err)
	}
	defer file.Close()

	for _, c := range cases {
		c.Kind = adapterkit.KindFuzz
		data, err := json.Marshal(c)
		if err != nil {
			continue
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("failed to append case: %w", err)
		}
	}
	return nil
}

// SaveReport writes the fuzz report JSON to .go-anything/reports/fuzz_<unit>.json.
func (p *DefaultFuzzPort) SaveReport(ctx context.Context, dir, unitID string, out *Output) error {
	reportsDir := filepath.Join(dir, ".go-anything", "reports")
	sanitized := strings.ReplaceAll(unitID, "/", "_")
	reportPath := filepath.Join(reportsDir, fmt.Sprintf("fuzz_%s.json", sanitized))
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		return fmt.Errorf("failed to create reports dir: %w", err)
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal fuzz report: %w", err)
	}
	return os.WriteFile(reportPath, data, 0644)
}
