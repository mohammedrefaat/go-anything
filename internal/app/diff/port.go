package diff

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	cliadapter "github.com/mohammedrefat/go-anything/adapters/application/cli"
	httpadapter "github.com/mohammedrefat/go-anything/adapters/application/http"
	"github.com/mohammedrefat/go-anything/internal/comparator"
	"github.com/mohammedrefat/go-anything/internal/recorder/effects"
	"github.com/mohammedrefat/go-anything/internal/replay"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// ReplayComparatorPort abstracts case loading, rules fetching, and replay execution.
type ReplayComparatorPort interface {
	LoadCases(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error)
	LoadRules(ctx context.Context, dir string) (*adapterkit.Rules, error)
	ReplayCases(ctx context.Context, unitID string, cases []*adapterkit.Case, target string, mode string, jobs int, rules *adapterkit.Rules) (*replay.ReplayReport, error)
}

// DefaultReplayComparatorPort implements ReplayComparatorPort using internal/replay and state.
type DefaultReplayComparatorPort struct{}

// NewDefaultReplayComparatorPort creates a new DefaultReplayComparatorPort.
func NewDefaultReplayComparatorPort() *DefaultReplayComparatorPort {
	return &DefaultReplayComparatorPort{}
}

// LoadCases reads recorded test cases from .go-anything/cases/<unitID>.jsonl or .go-anything/cases/<baseID>.jsonl.
func (p *DefaultReplayComparatorPort) LoadCases(ctx context.Context, dir, unitID string) ([]*adapterkit.Case, error) {
	casesPath := filepath.Join(dir, ".go-anything", "cases", filepath.FromSlash(unitID)+".jsonl")
	file, err := os.Open(casesPath)
	if err != nil {
		altPath := filepath.Join(dir, ".go-anything", "cases", filepath.Base(unitID)+".jsonl")
		altFile, altErr := os.Open(altPath)
		if altErr == nil {
			file = altFile
		} else {
			return nil, fmt.Errorf("failed to open cases file %s: %w", casesPath, err)
		}
	}
	defer file.Close()

	var cases []*adapterkit.Case
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c adapterkit.Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("malformed case record: %w", err)
		}
		cases = append(cases, &c)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading cases: %w", err)
	}
	return cases, nil
}

// LoadRules retrieves auto-detected wildcarding and normalizer rules from the store.
func (p *DefaultReplayComparatorPort) LoadRules(ctx context.Context, dir string) (*adapterkit.Rules, error) {
	store := state.NewStore(dir)
	return store.GetRules()
}

// ReplayCases drives replay execution against the target URL or binary.
func (p *DefaultReplayComparatorPort) ReplayCases(ctx context.Context, unitID string, cases []*adapterkit.Case, target string, mode string, jobs int, rules *adapterkit.Rules) (*replay.ReplayReport, error) {
	var driver adapterkit.Replay
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		driver = httpadapter.NewAdapter(httpadapter.WithBaseURL(target))
	} else {
		driver = cliadapter.NewAdapter(target)
	}

	hasEffects := mode == comparator.ModeEffectMultiset
	if !hasEffects {
		for _, c := range cases {
			if c != nil && len(c.ExpectedOutput.Effects) > 0 {
				hasEffects = true
				break
			}
		}
	}

	if hasEffects {
		effectsMgr, err := effects.NewManager()
		if err != nil {
			driver.Close()
			return nil, fmt.Errorf("failed to start effects mock server: %w", err)
		}
		driver = newEffectsInterceptionDriver(driver, effectsMgr)
	}
	defer driver.Close()

	var opts []replay.Option
	if jobs > 0 {
		opts = append(opts, replay.WithJobs(jobs))
	}
	if rules != nil {
		opts = append(opts, replay.WithRules(rules))
	}
	if mode != "" {
		opts = append(opts, replay.WithMode(mode))
	}

	engine := replay.New(opts...)
	return engine.ReplayCases(ctx, unitID, cases, driver)
}

type effectsInterceptionDriver struct {
	inner      adapterkit.Replay
	effectsMgr *effects.Manager
	mu         sync.Mutex
}

func newEffectsInterceptionDriver(inner adapterkit.Replay, mgr *effects.Manager) *effectsInterceptionDriver {
	return &effectsInterceptionDriver{
		inner:      inner,
		effectsMgr: mgr,
	}
}

func (d *effectsInterceptionDriver) DriverName() string {
	return d.inner.DriverName()
}

func (d *effectsInterceptionDriver) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.effectsMgr.Reset()

	tc := *testCase
	inputEnv := make(map[string]string)
	for k, v := range tc.Input.Env {
		inputEnv[k] = v
	}
	for k, v := range d.effectsMgr.Env() {
		inputEnv[k] = v
		os.Setenv(k, v)
	}
	tc.Input.Env = inputEnv

	out, err := d.inner.ExecuteCase(ctx, &tc)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = &adapterkit.CaseOutput{}
	}

	out.Effects = d.effectsMgr.Effects()
	return out, nil
}

func (d *effectsInterceptionDriver) Close() error {
	for k := range d.effectsMgr.Env() {
		os.Unsetenv(k)
	}
	d.effectsMgr.Close()
	return d.inner.Close()
}
