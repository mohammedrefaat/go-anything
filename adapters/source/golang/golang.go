// Package golang provides the Go source code language adapter, implementing
// repository analysis, package boundary inspection, and topological wave planning.
package golang

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mohammedrefat/go-anything/internal/analyzer"
	"github.com/mohammedrefat/go-anything/internal/planner"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// Adapter implements adapterkit.LanguageAdapter for Go source repositories.
type Adapter struct {
	analyzer *analyzer.Analyzer
	planner  *planner.Planner
}

// NewAdapter creates a new Go source language adapter.
func NewAdapter() *Adapter {
	return &Adapter{
		analyzer: analyzer.New(),
		planner:  planner.New(),
	}
}

// Name returns the language adapter identifier.
func (a *Adapter) Name() string { return "golang" }

// Version returns the adapter specification version.
func (a *Adapter) Version() string { return adapterkit.Version }

// SourceLanguage returns the handled programming language.
func (a *Adapter) SourceLanguage() string { return "go" }

// SupportedTransports returns the transport mechanisms supported by this adapter.
func (a *Adapter) SupportedTransports() []adapterkit.TransportType {
	return []adapterkit.TransportType{
		adapterkit.TransportInProcess,
	}
}

// Capabilities declares explicit privileges requested by this adapter.
func (a *Adapter) Capabilities() []adapterkit.Capability {
	return []adapterkit.Capability{
		adapterkit.CapabilityReadRepo,
		adapterkit.CapabilityRunOriginal,
	}
}

// FieldSemantics returns data semantics declared by this adapter.
func (a *Adapter) FieldSemantics() []adapterkit.FieldSemantic {
	return []adapterkit.FieldSemantic{}
}

// Analyze inspects a repository and generates a migration manifest.
func (a *Adapter) Analyze(ctx context.Context, repoPath string) (*adapterkit.Manifest, error) {
	manifest, _, err := a.PlanMigration(ctx, repoPath, nil)
	return manifest, err
}

// DiscoverUnits discovers all candidate migration units within the repository.
func (a *Adapter) DiscoverUnits(ctx context.Context, repoPath string) ([]adapterkit.Unit, error) {
	_, plan, err := a.PlanMigration(ctx, repoPath, nil)
	if err != nil {
		return nil, err
	}
	var units []adapterkit.Unit
	for _, u := range plan.Units {
		if u != nil {
			units = append(units, *u)
		}
	}
	return units, nil
}

// CreateReplayDriver returns a replay driver configured for this adapter.
func (a *Adapter) CreateReplayDriver(ctx context.Context, unitID string) (adapterkit.Replay, error) {
	return nil, nil
}

// PlanMigration executes static analysis, computes topological waves, groups cycles,
// and optionally persists manifest.json and Planned state files to store.
func (a *Adapter) PlanMigration(ctx context.Context, repoPath string, store *state.Store) (*adapterkit.Manifest, *planner.PlanResult, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve repo path: %w", err)
	}

	analysis, err := a.analyzer.Analyze(ctx, absPath, "./...")
	if err != nil {
		return nil, nil, fmt.Errorf("analysis failed: %w", err)
	}

	plan, err := a.planner.PlanFromAnalyzerResult(analysis)
	if err != nil {
		return nil, nil, fmt.Errorf("planning failed: %w", err)
	}

	projectName := filepath.Base(absPath)
	if analysis.ModulePath != "" {
		projectName = analysis.ModulePath
	}

	var unitIDs []string
	for _, u := range plan.Units {
		unitIDs = append(unitIDs, u.ID)
	}

	var waves []adapterkit.Wave
	for w := 0; w <= plan.MaxWave; w++ {
		if unitsInWave, ok := plan.Waves[w]; ok && len(unitsInWave) > 0 {
			var ids []string
			for _, u := range unitsInWave {
				ids = append(ids, u.ID)
			}
			waves = append(waves, adapterkit.Wave{
				Wave:    w,
				UnitIDs: ids,
			})
		}
	}

	manifest := &adapterkit.Manifest{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		ProjectName:    projectName,
		SourceLanguage: "go",
		TargetLanguage: "go",
		Units:          unitIDs,
		Waves:          waves,
	}

	// Persist to .go-anything store if provided
	if store != nil {
		if err := store.SaveManifest(manifest); err != nil {
			return nil, nil, fmt.Errorf("failed to save manifest: %w", err)
		}

		for _, u := range plan.Units {
			if err := store.SaveUnit(u); err != nil {
				return nil, nil, fmt.Errorf("failed to save planned unit %s: %w", u.ID, err)
			}
		}
	}

	return manifest, plan, nil
}

// ModernizationResult encapsulates the outcome of setting up a Go modernization workspace.
type ModernizationResult struct {
	Workspace *ThreeTierWorkspace  `json:"workspace"`
	Tier1     *Tier1Result         `json:"tier1"`
	Manifest  *adapterkit.Manifest `json:"manifest"`
	Plan      *planner.PlanResult  `json:"plan"`
}

// ModernizeWorkspace sets up the three-tier workspace in repoRoot, applies Tier 1 mechanical
// transformations to modern/, plans dependency waves, and records Planned units in store.
func (a *Adapter) ModernizeWorkspace(ctx context.Context, repoRoot string, store *state.Store) (*ModernizationResult, error) {
	ws, err := SetupThreeTierWorkspace(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to setup three-tier workspace: %w", err)
	}

	tier1Res, err := RunTier1(ctx, ws.ModernDir)
	if err != nil {
		return nil, fmt.Errorf("failed to run tier 1 transformations: %w", err)
	}

	manifest, plan, err := a.PlanMigration(ctx, ws.ModernDir, store)
	if err != nil {
		return nil, fmt.Errorf("failed to plan migration waves: %w", err)
	}

	// Canonicalize project name in manifest to workspace module path
	if ws.ModulePath != "" {
		manifest.ProjectName = ws.ModulePath
		if store != nil {
			_ = store.SaveManifest(manifest)
		}
	}

	return &ModernizationResult{
		Workspace: ws,
		Tier1:     tier1Res,
		Manifest:  manifest,
		Plan:      plan,
	}, nil
}
