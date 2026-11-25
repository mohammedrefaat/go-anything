package javascript

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mohammedrefaat/go-anything/adapters/source/typescript"
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

// Option configures the JavaScript Adapter.
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

// WithSharedReplayHandler configures a shared replay execution handler for tests.
func WithSharedReplayHandler(handler typescript.ReplayHandler) Option {
	return func(a *Adapter) {
		a.sharedHandler = handler
	}
}

// Adapter provides the JavaScript source code language adapter,
// specialized for pure JavaScript applications (.js, .mjs, .cjs) and Node.js microservices.
type Adapter struct {
	defaultTransport adapterkit.TransportType
	nodeBinary       string
	httpBaseURL      string
	sharedHandler    typescript.ReplayHandler
	tsAdapter        *typescript.Adapter
}

// NewAdapter creates a new JavaScript source language adapter.
func NewAdapter(opts ...Option) *Adapter {
	a := &Adapter{
		defaultTransport: adapterkit.TransportSubprocess,
		nodeBinary:       "node",
	}
	for _, opt := range opts {
		opt(a)
	}

	tsOpts := []typescript.Option{
		typescript.WithDefaultTransport(a.defaultTransport),
		typescript.WithNodeBinary(a.nodeBinary),
		typescript.WithBaseURL(a.httpBaseURL),
	}
	if a.sharedHandler != nil {
		tsOpts = append(tsOpts, typescript.WithAdapterReplayHandler(a.sharedHandler))
	}
	a.tsAdapter = typescript.NewAdapter(tsOpts...)

	return a
}

// Name returns the language adapter identifier.
func (a *Adapter) Name() string {
	return "javascript"
}

// Version returns the adapter specification version.
func (a *Adapter) Version() string {
	return adapterkit.Version
}

// SourceLanguage returns the primary handled programming language.
func (a *Adapter) SourceLanguage() string {
	return "javascript"
}

// AdapterManifest returns the static adapter manifest including handled file extensions.
func (a *Adapter) AdapterManifest() adapterkit.AdapterManifest {
	return adapterkit.AdapterManifest{
		Name:           a.Name(),
		SourceLanguage: a.SourceLanguage(),
		Extensions:     []string{".js", ".jsx", ".mjs", ".cjs"},
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

// CoverageLimits declares coverage expectations for JavaScript projects.
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

// Detect checks if the specified directory contains JavaScript project files.
func (a *Adapter) Detect(ctx context.Context, repoPath string) (bool, error) {
	info, err := os.Stat(repoPath)
	if err != nil || !info.IsDir() {
		return false, nil
	}

	// 1. package.json exists
	pkgPath := filepath.Join(repoPath, "package.json")
	if _, err := os.Stat(pkgPath); err == nil {
		// If tsconfig.json is absent, this is primarily a JavaScript project
		tsPath := filepath.Join(repoPath, "tsconfig.json")
		if _, err := os.Stat(tsPath); os.IsNotExist(err) {
			return true, nil
		}
	}

	// 2. Scan for .js/.mjs/.cjs files
	foundJS := false
	_ = filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == "node_modules" || base == ".git" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".js" || ext == ".mjs" || ext == ".cjs" {
			foundJS = true
			return filepath.SkipAll
		}
		return nil
	})

	return foundJS, nil
}

// CheckEnvironment verifies that the required Node.js runtime is installed and accessible.
func (a *Adapter) CheckEnvironment(ctx context.Context) error {
	nodePath, err := exec.LookPath(a.nodeBinary)
	if err != nil {
		return fmt.Errorf("node runtime %q not found on PATH: install Node.js 18+ to use javascript adapter", a.nodeBinary)
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

// Analyze inspects a JavaScript repository and returns a migration manifest.
func (a *Adapter) Analyze(ctx context.Context, repoPath string) (*adapterkit.Manifest, error) {
	manifest, err := a.tsAdapter.Analyze(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	if manifest != nil {
		manifest.SourceLanguage = "javascript"
	}
	return manifest, nil
}

// DiscoverUnits discovers all candidate migration units organized by module boundaries.
func (a *Adapter) DiscoverUnits(ctx context.Context, repoPath string) ([]adapterkit.Unit, error) {
	return a.tsAdapter.DiscoverUnits(ctx, repoPath)
}

// CreateReplayDriver returns the default replay driver configured for this adapter.
func (a *Adapter) CreateReplayDriver(ctx context.Context, unitID string) (adapterkit.Replay, error) {
	return a.tsAdapter.CreateReplayDriver(ctx, unitID)
}

// CreateReplayDrivers returns multi-transport replay drivers for verifying cross-transport consistency.
func (a *Adapter) CreateReplayDrivers(ctx context.Context, unitID string) (map[adapterkit.TransportType]adapterkit.Replay, error) {
	return a.tsAdapter.CreateReplayDrivers(ctx, unitID)
}
