package profile

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// UseCase coordinates hot function profiling, candidate ranking, and purity analysis.
type UseCase struct {
	port ProfilePort
}

// New creates a new profiling UseCase.
func New(port ProfilePort) *UseCase {
	if port == nil {
		port = NewDefaultProfilePort()
	}
	return &UseCase{port: port}
}

// Run executes the profile use-case.
func (uc *UseCase) Run(ctx context.Context, in Input) (*Output, error) {
	if in.UnitID == "" {
		return nil, fmt.Errorf("unit ID is required")
	}

	dir := in.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}

	top := in.Top
	if top <= 0 {
		top = 10
	}

	lang, err := uc.port.DetectLanguage(ctx, absDir, in.UnitID)
	if err != nil || lang == "" {
		lang = "python"
	}

	res, err := uc.port.ProfileUnit(ctx, absDir, in.UnitID, lang, top)
	if err != nil {
		return nil, fmt.Errorf("profiling failed: %w", err)
	}

	candidates := append([]Candidate{}, res.Candidates...)

	// Sort candidates by SelfPercent descending, then CumulativePercent descending
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].SelfPercent != candidates[j].SelfPercent {
			return candidates[i].SelfPercent > candidates[j].SelfPercent
		}
		return candidates[i].CumulativePercent > candidates[j].CumulativePercent
	})

	// Slice to Top candidates
	if len(candidates) > top {
		candidates = candidates[:top]
	}

	// Assign 1-based ranks
	for i := range candidates {
		candidates[i].Rank = i + 1
	}

	summary := fmt.Sprintf("Profiled %d hotspot candidate(s) using %s for unit %s (top %d)",
		len(candidates), res.Profiler, in.UnitID, top)

	timestamp := time.Now().UTC().Format(time.RFC3339)

	out := &Output{
		SchemaVersion:   "v2",
		UnitID:          in.UnitID,
		Language:        res.Language,
		Profiler:        res.Profiler,
		TotalDurationMS: res.TotalDurationMS,
		Top:             top,
		Candidates:      candidates,
		Summary:         summary,
		Timestamp:       timestamp,
	}

	// Persist report under .go-anything/reports/profile_<unit>.json
	_ = uc.port.SaveReport(ctx, absDir, in.UnitID, out)

	return out, nil
}

// Run is a convenience function.
func Run(ctx context.Context, in Input, port ...ProfilePort) (*Output, error) {
	var p ProfilePort
	if len(port) > 0 {
		p = port[0]
	}
	return New(p).Run(ctx, in)
}
