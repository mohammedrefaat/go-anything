package python

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit/conformance"
)

// Ensure Adapter satisfies all required interfaces.
var (
	_ adapterkit.LanguageAdapter               = (*Adapter)(nil)
	_ conformance.Detector                     = (*Adapter)(nil)
	_ conformance.EnvironmentChecker           = (*Adapter)(nil)
	_ conformance.CaseRecorder                 = (*Adapter)(nil)
	_ conformance.CoverageLimiter              = (*Adapter)(nil)
	_ conformance.MultiTransportReplayProvider = (*Adapter)(nil)
)

// Adapter implements adapterkit.LanguageAdapter for Python source repositories.
type Adapter struct {
	discoverer *DependencyDiscoverer
	testDisc   *TestDiscoverer
	extractor  *HotPathExtractor
	bridge     BridgeLimits
}

// NewAdapter creates a new Python source language adapter.
func NewAdapter() *Adapter {
	return &Adapter{
		discoverer: NewDependencyDiscoverer(),
		testDisc:   NewTestDiscoverer(),
		extractor:  NewHotPathExtractor(),
		bridge:     DefaultBridgeLimits(),
	}
}

// Name returns the identifier of the adapter.
func (a *Adapter) Name() string {
	return "python"
}

// Version returns the version of this adapter.
func (a *Adapter) Version() string {
	return adapterkit.Version
}

// SourceLanguage returns the primary source language handled.
func (a *Adapter) SourceLanguage() string {
	return "python"
}

// SupportedTransports returns the list of transports supported by this adapter.
func (a *Adapter) SupportedTransports() []adapterkit.TransportType {
	return []adapterkit.TransportType{
		adapterkit.TransportSubprocess,
		adapterkit.TransportHTTP,
	}
}

// Capabilities returns the explicit capability privileges declared by this adapter.
func (a *Adapter) Capabilities() []adapterkit.Capability {
	return []adapterkit.Capability{
		adapterkit.CapabilityReadRepo,
		adapterkit.CapabilityRunOriginal,
	}
}

// FieldSemantics returns the declared data semantics for Python fields.
func (a *Adapter) FieldSemantics() []adapterkit.FieldSemantic {
	return []adapterkit.FieldSemantic{
		{
			Path:        "*.timestamp",
			Semantic:    adapterkit.SemanticVolatile,
			Description: "ISO-8601 or epoch timestamps generated dynamically by Python datetime/time",
		},
		{
			Path:        "created_at",
			Semantic:    adapterkit.SemanticVolatile,
			Description: "Record creation timestamp",
		},
		{
			Path:        "updated_at",
			Semantic:    adapterkit.SemanticVolatile,
			Description: "Record update timestamp",
		},
		{
			Path:        "request_id",
			Semantic:    adapterkit.SemanticVolatile,
			Description: "Per-request UUID or tracing ID",
		},
		{
			Path:        "*.token",
			Semantic:    adapterkit.SemanticSecret,
			Description: "Authentication bearer tokens or session secrets",
		},
		{
			Path:        "*.secret",
			Semantic:    adapterkit.SemanticSecret,
			Description: "Application secrets or cryptographic keys",
		},
		{
			Path:        "*.password",
			Semantic:    adapterkit.SemanticSecret,
			Description: "User passwords or private credentials",
		},
		{
			Path:        "*.api_key",
			Semantic:    adapterkit.SemanticSecret,
			Description: "API key credentials",
		},
		{
			Path:        "auth.token",
			Semantic:    adapterkit.SemanticSecret,
			Description: "Authorization token payload",
		},
		{
			Path:        "headers.Authorization",
			Semantic:    adapterkit.SemanticSecret,
			Description: "HTTP Authorization header",
		},
		{
			Path:        "*.items",
			Semantic:    adapterkit.SemanticOrdered,
			Description: "Ordered list or sequence",
		},
		{
			Path:        "*.list",
			Semantic:    adapterkit.SemanticOrdered,
			Description: "Ordered array elements",
		},
		{
			Path:        "items",
			Semantic:    adapterkit.SemanticOrdered,
			Description: "Top-level collection elements",
		},
		{
			Path:        "results",
			Semantic:    adapterkit.SemanticOrdered,
			Description: "Ordered query or search results",
		},
	}
}

// Detect checks whether the specified repository path contains a Python project.
func (a *Adapter) Detect(ctx context.Context, repoPath string) (bool, error) {
	fi, err := os.Stat(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !fi.IsDir() {
		return false, nil
	}

	// Known Python project root manifest indicators
	manifestIndicators := []string{
		"setup.py",
		"pyproject.toml",
		"requirements.txt",
		"Pipfile",
		"tox.ini",
		"setup.cfg",
		"manage.py",
		"main.py",
		"app.py",
	}

	for _, file := range manifestIndicators {
		if _, err := os.Stat(filepath.Join(repoPath, file)); err == nil {
			return true, nil
		}
	}

	// Walk directory to check for any .py files
	hasPyFiles := false
	_ = filepath.WalkDir(repoPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "__pycache__" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".py") {
			hasPyFiles = true
			return filepath.SkipAll
		}
		return nil
	})

	return hasPyFiles, nil
}

// CheckEnvironment verifies that the Python runtime is installed and operational.
func (a *Adapter) CheckEnvironment(ctx context.Context) error {
	// Probe for python or python3 on system PATH
	bins := []string{"python", "python3"}
	var foundPath string
	for _, b := range bins {
		p, err := exec.LookPath(b)
		if err == nil {
			foundPath = p
			break
		}
	}

	if foundPath == "" {
		return fmt.Errorf("python binary not found on PATH: expected 'python' or 'python3'")
	}

	cmd := exec.CommandContext(ctx, foundPath, "--version")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("python execution check failed (%s --version): %w (output: %s)", foundPath, err, string(out))
	}

	return nil
}

// Analyze inspects a repository and generates an initial migration manifest.
func (a *Adapter) Analyze(ctx context.Context, repoPath string) (*adapterkit.Manifest, error) {
	project, err := a.discoverer.Discover(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("python analysis failed: %w", err)
	}

	var unitIDs []string
	for _, u := range project.Units {
		unitIDs = append(unitIDs, u.ID)
	}

	var waves []adapterkit.Wave
	for w := 0; w <= project.Plan.MaxWave; w++ {
		if unitsInWave, ok := project.Plan.Waves[w]; ok && len(unitsInWave) > 0 {
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

	return &adapterkit.Manifest{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		ProjectName:    project.ProjectName,
		SourceLanguage: "python",
		TargetLanguage: "go",
		Units:          unitIDs,
		Waves:          waves,
		Metadata: map[string]any{
			"total_units": project.Plan.TotalUnits,
			"max_wave":    project.Plan.MaxWave,
			"cycles":      project.Plan.Cycles,
		},
	}, nil
}

// DiscoverUnits discovers all candidate migration units within the repository.
func (a *Adapter) DiscoverUnits(ctx context.Context, repoPath string) ([]adapterkit.Unit, error) {
	project, err := a.discoverer.Discover(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("python unit discovery failed: %w", err)
	}

	var units []adapterkit.Unit
	for _, u := range project.Units {
		if u != nil {
			units = append(units, *u)
		}
	}
	return units, nil
}

// CreateReplayDriver returns a default replay driver configured for this adapter.
func (a *Adapter) CreateReplayDriver(ctx context.Context, unitID string) (adapterkit.Replay, error) {
	return NewSubprocessReplayDriver(unitID), nil
}

// CreateReplayDrivers returns replay drivers for both Subprocess and HTTP transports.
func (a *Adapter) CreateReplayDrivers(ctx context.Context, unitID string) (map[adapterkit.TransportType]adapterkit.Replay, error) {
	return map[adapterkit.TransportType]adapterkit.Replay{
		adapterkit.TransportSubprocess: NewSubprocessReplayDriver(unitID),
		adapterkit.TransportHTTP:       NewHTTPReplayDriver(unitID),
	}, nil
}

// RecordCases records or generates behavioral test cases for the specified unit.
func (a *Adapter) RecordCases(ctx context.Context, unitID string, inputs []adapterkit.CaseInput) ([]*adapterkit.Case, error) {
	var cases []*adapterkit.Case
	for i, in := range inputs {
		caseID := fmt.Sprintf("case-%s-%04d", unitID, i+1)
		cases = append(cases, &adapterkit.Case{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         caseID,
			UnitID:         unitID,
			Kind:           adapterkit.KindHappy,
			SourceRevision: "HEAD",
			Runtime:        "python",
			CapturedAt:     time.Now().UTC().Format(time.RFC3339),
			Input:          in,
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: 200,
				Body:       map[string]any{"status": "ok"},
			},
		})
	}
	return cases, nil
}

// CoverageLimits returns the declared coverage limits for the Python adapter.
func (a *Adapter) CoverageLimits() conformance.CoverageLimits {
	return a.bridge.CoverageLimits
}

// DiscoverTests provides access to pytest and unittest discovery for a repository.
func (a *Adapter) DiscoverTests(ctx context.Context, repoPath string, units []string) (*TestDiscoveryResult, error) {
	return a.testDisc.DiscoverTests(ctx, repoPath, units)
}

// ExtractHotPathReport parses cProfile profiling output.
func (a *Adapter) ExtractHotPathReport(content string, knownUnits []string) (*HotPathReport, error) {
	return a.extractor.ParseReport(content, knownUnits)
}

// BridgeLimits returns the declared bridge constraints.
func (a *Adapter) BridgeLimits() BridgeLimits {
	return a.bridge
}
