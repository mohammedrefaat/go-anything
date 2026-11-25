package typescript

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit/conformance"
)

// Ensure Adapter implements all required and optional adapterkit interfaces at compile time.
var (
	_ adapterkit.LanguageAdapter               = (*Adapter)(nil)
	_ conformance.Detector                     = (*Adapter)(nil)
	_ conformance.EnvironmentChecker           = (*Adapter)(nil)
	_ conformance.MultiTransportReplayProvider = (*Adapter)(nil)
	_ conformance.CoverageLimiter              = (*Adapter)(nil)
)

// Option configures the TypeScript Adapter.
type Option func(*Adapter)

// WithDefaultTransport sets the primary transport (Subprocess or HTTP).
func WithDefaultTransport(t adapterkit.TransportType) Option {
	return func(a *Adapter) {
		a.defaultTransport = t
	}
}

// WithNodeBinary configures custom Node executable path.
func WithNodeBinary(path string) Option {
	return func(a *Adapter) {
		a.nodeBinary = path
	}
}

// WithBaseURL configures target base URL for HTTP replay driver.
func WithBaseURL(url string) Option {
	return func(a *Adapter) {
		a.httpBaseURL = url
	}
}

// WithAdapterReplayHandler configures a shared replay execution handler for tests.
func WithAdapterReplayHandler(handler ReplayHandler) Option {
	return func(a *Adapter) {
		a.sharedHandler = handler
	}
}

// Adapter provides the TypeScript and JavaScript source code language adapter,
// implementing repository detection, environment checking, package.json parsing,
// AST/import inspection, topological wave planning, test discovery, and replay drivers.
type Adapter struct {
	defaultTransport adapterkit.TransportType
	nodeBinary       string
	httpBaseURL      string
	sharedHandler    ReplayHandler
}

// NewAdapter creates a new TypeScript source language adapter.
func NewAdapter(opts ...Option) *Adapter {
	a := &Adapter{
		defaultTransport: adapterkit.TransportSubprocess,
		nodeBinary:       "node",
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Name returns the language adapter identifier.
func (a *Adapter) Name() string {
	return "typescript"
}

// Version returns the adapter specification version.
func (a *Adapter) Version() string {
	return adapterkit.Version
}

// SourceLanguage returns the primary handled programming language.
func (a *Adapter) SourceLanguage() string {
	return "typescript"
}

// AdapterManifest returns the static adapter manifest including handled file extensions.
func (a *Adapter) AdapterManifest() adapterkit.AdapterManifest {
	return adapterkit.AdapterManifest{
		Name:           a.Name(),
		SourceLanguage: a.SourceLanguage(),
		Extensions:     []string{".ts", ".tsx", ".mts", ".cts"},
	}
}

// SupportedTransports returns the transport mechanisms supported by this adapter.
func (a *Adapter) SupportedTransports() []adapterkit.TransportType {
	return []adapterkit.TransportType{
		adapterkit.TransportSubprocess,
		adapterkit.TransportHTTP,
	}
}

// Capabilities declares explicit privileges requested by this adapter.
func (a *Adapter) Capabilities() []adapterkit.Capability {
	return []adapterkit.Capability{
		adapterkit.CapabilityReadRepo,
		adapterkit.CapabilityRunOriginal,
		adapterkit.CapabilityNetwork,
	}
}

// FieldSemantics returns data semantics declared by this adapter.
func (a *Adapter) FieldSemantics() []adapterkit.FieldSemantic {
	return []adapterkit.FieldSemantic{
		{Path: "auth.token", Semantic: adapterkit.SemanticSecret},
		{Path: "authorization", Semantic: adapterkit.SemanticSecret},
		{Path: "password", Semantic: adapterkit.SemanticSecret},
		{Path: "secret", Semantic: adapterkit.SemanticSecret},
		{Path: "timestamp", Semantic: adapterkit.SemanticVolatile},
		{Path: "created_at", Semantic: adapterkit.SemanticVolatile},
		{Path: "updated_at", Semantic: adapterkit.SemanticVolatile},
		{Path: "request_id", Semantic: adapterkit.SemanticVolatile},
		{Path: "trace_id", Semantic: adapterkit.SemanticVolatile},
		{Path: "items", Semantic: adapterkit.SemanticOrdered},
	}
}

// CoverageLimits declares coverage expectations for TypeScript projects.
func (a *Adapter) CoverageLimits() conformance.CoverageLimits {
	return conformance.CoverageLimits{
		MinStatementCoverage: 0.80,
		MaxUnreplayableRatio: 0.10,
		SupportedCaseKinds: []adapterkit.CaseKind{
			adapterkit.KindHappy,
			adapterkit.KindEdge,
			adapterkit.KindTrap,
			adapterkit.KindFuzz,
			adapterkit.KindCaptured,
		},
	}
}

// Detect checks if the specified directory contains TypeScript or JavaScript project files.
func (a *Adapter) Detect(ctx context.Context, repoPath string) (bool, error) {
	info, err := os.Stat(repoPath)
	if err != nil || !info.IsDir() {
		return false, nil
	}

	// 1. Direct project markers in root
	markers := []string{
		"package.json",
		"tsconfig.json",
		"jsconfig.json",
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(repoPath, m)); err == nil {
			return true, nil
		}
	}

	// 2. Scan shallowly for any .ts/.tsx/.js/.jsx files
	foundSource := false
	_ = filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if ignoredDirs[base] {
				return filepath.SkipDir
			}
			// Don't walk excessively deep on detection
			rel, _ := filepath.Rel(repoPath, path)
			if strings.Count(rel, string(filepath.Separator)) > 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if IsTypeScriptOrJavaScriptFile(path) {
			foundSource = true
			return filepath.SkipAll
		}
		return nil
	})

	return foundSource, nil
}

// CheckEnvironment verifies that the required Node.js runtime is installed and accessible.
func (a *Adapter) CheckEnvironment(ctx context.Context) error {
	nodePath, err := exec.LookPath(a.nodeBinary)
	if err != nil {
		return fmt.Errorf("node runtime %q not found on PATH: install Node.js 18+ to use typescript adapter", a.nodeBinary)
	}

	cmd := exec.CommandContext(ctx, nodePath, "-v")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to execute node binary at %s: %w", nodePath, err)
	}

	versionStr := strings.TrimSpace(string(out))
	if !strings.HasPrefix(versionStr, "v") {
		return fmt.Errorf("unexpected node version output %q from %s", versionStr, nodePath)
	}

	return nil
}

// Analyze inspects a TypeScript/JavaScript repository and returns a migration manifest.
func (a *Adapter) Analyze(ctx context.Context, repoPath string) (*adapterkit.Manifest, error) {
	manifest, _, err := AnalyzeDependencies(repoPath)
	return manifest, err
}

// DiscoverUnits discovers all candidate migration units organized by module boundaries.
func (a *Adapter) DiscoverUnits(ctx context.Context, repoPath string) ([]adapterkit.Unit, error) {
	_, units, err := AnalyzeDependencies(repoPath)
	if err != nil {
		return nil, err
	}
	var res []adapterkit.Unit
	for _, u := range units {
		if u != nil {
			res = append(res, *u)
		}
	}
	return res, nil
}

// CreateReplayDriver returns the default replay driver configured for this adapter.
func (a *Adapter) CreateReplayDriver(ctx context.Context, unitID string) (adapterkit.Replay, error) {
	if a.defaultTransport == adapterkit.TransportHTTP {
		return NewHTTPDriver(
			WithHTTPBaseURL(a.httpBaseURL),
			WithHTTPHandler(a.sharedHandler),
		), nil
	}
	return NewSubprocessDriver(
		WithSubprocessNodePath(a.nodeBinary),
		WithSubprocessHandler(a.sharedHandler),
	), nil
}

// CreateReplayDrivers returns multi-transport replay drivers for verifying cross-transport consistency.
func (a *Adapter) CreateReplayDrivers(ctx context.Context, unitID string) (map[adapterkit.TransportType]adapterkit.Replay, error) {
	return map[adapterkit.TransportType]adapterkit.Replay{
		adapterkit.TransportSubprocess: NewSubprocessDriver(
			WithSubprocessNodePath(a.nodeBinary),
			WithSubprocessHandler(a.sharedHandler),
		),
		adapterkit.TransportHTTP: NewHTTPDriver(
			WithHTTPBaseURL(a.httpBaseURL),
			WithHTTPHandler(a.sharedHandler),
		),
	}, nil
}
