package analyze

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// UseCase coordinates static analysis and dependency wave generation.
type UseCase struct {
	port AnalyzerPort
}

// New creates a new analyze UseCase.
func New(port AnalyzerPort) *UseCase {
	if port == nil {
		port = NewDefaultAnalyzerPort()
	}
	return &UseCase{port: port}
}

// Run executes the analysis use-case.
func (uc *UseCase) Run(ctx context.Context, input Input) (*Output, error) {
	dir := input.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}

	source := input.Source
	if source == "" {
		source = "go"
	}

	analysisRes, err := uc.port.Analyze(ctx, absDir, source)
	if err != nil {
		return nil, fmt.Errorf("analysis failed: %w", err)
	}

	planRes, err := uc.port.Plan(ctx, analysisRes)
	if err != nil {
		return nil, fmt.Errorf("planning failed: %w", err)
	}

	projectName := filepath.Base(absDir)
	if analysisRes != nil && analysisRes.ModulePath != "" {
		projectName = analysisRes.ModulePath
	}

	var waveList []WaveInfo
	maxWave := 0
	if planRes != nil && planRes.Waves != nil {
		var waveNums []int
		for w := range planRes.Waves {
			waveNums = append(waveNums, w)
		}
		sort.Ints(waveNums)

		for _, w := range waveNums {
			if w > maxWave {
				maxWave = w
			}
			units := planRes.Waves[w]
			var ids []string
			for _, u := range units {
				if u != nil {
					ids = append(ids, u.ID)
				}
			}
			sort.Strings(ids)
			waveList = append(waveList, WaveInfo{
				Wave:    w,
				UnitIDs: ids,
			})
		}
	}

	cycles := make([][]string, 0)
	if planRes != nil && planRes.Cycles != nil {
		for _, c := range planRes.Cycles {
			sortedCycle := append([]string{}, c...)
			sort.Strings(sortedCycle)
			cycles = append(cycles, sortedCycle)
		}
	}

	graph := make(map[string][]string)
	if analysisRes != nil && analysisRes.InternalGraph != nil {
		for k, v := range analysisRes.InternalGraph {
			edges := append([]string{}, v...)
			sort.Strings(edges)
			graph[k] = edges
		}
	}

	units := make([]*adapterkit.Unit, 0)
	if planRes != nil && planRes.Units != nil {
		units = planRes.Units
		// Rule 2: Deterministic sort by Unit ID
		sort.Slice(units, func(i, j int) bool {
			return units[i].ID < units[j].ID
		})
	}

	return &Output{
		SchemaVersion:  "v2",
		Project:        projectName,
		SourceLanguage: source,
		Source:         source,
		Units:          units,
		Waves:          waveList,
		Cycles:         cycles,
		Graph:          graph,
		TotalUnits:     len(units),
		MaxWave:        maxWave,
	}, nil
}

// Run is a convenience function that executes the use-case with the provided port.
func Run(ctx context.Context, input Input, port ...AnalyzerPort) (*Output, error) {
	var p AnalyzerPort
	if len(port) > 0 {
		p = port[0]
	}
	return New(p).Run(ctx, input)
}
