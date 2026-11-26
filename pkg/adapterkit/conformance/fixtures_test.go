package conformance_test

import (
	"context"
	"errors"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit/conformance"
)

// mockReplayDriver implements adapterkit.Replay for testing.
type mockReplayDriver struct {
	name   string
	output *adapterkit.CaseOutput
	closed bool
}

func (m *mockReplayDriver) DriverName() string { return m.name }
func (m *mockReplayDriver) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if m.output != nil {
		return m.output, nil
	}
	return &adapterkit.CaseOutput{
		StatusCode: 200,
		Body:       map[string]any{"status": "ok"},
	}, nil
}
func (m *mockReplayDriver) Close() error {
	m.closed = true
	return nil
}

// validMockAdapter provides a fully compliant mock adapter for all checks.
type validMockAdapter struct {
	name           string
	sourceLang     string
	caps           []adapterkit.Capability
	semantics      []adapterkit.FieldSemantic
	units          []adapterkit.Unit
	manifest       *adapterkit.Manifest
	drivers        map[adapterkit.TransportType]adapterkit.Replay
	coverageLimits conformance.CoverageLimits
}

func newValidMockAdapter() *validMockAdapter {
	return &validMockAdapter{
		name:       "mock-adapter",
		sourceLang: "mocklang",
		caps: []adapterkit.Capability{
			adapterkit.CapabilityReadRepo,
			adapterkit.CapabilityRunOriginal,
		},
		semantics: []adapterkit.FieldSemantic{
			{Path: "auth.token", Semantic: adapterkit.SemanticSecret},
			{Path: "request.id", Semantic: adapterkit.SemanticVolatile},
			{Path: "items.list", Semantic: adapterkit.SemanticOrdered},
		},
		manifest: &adapterkit.Manifest{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			ProjectName:    "mock-project",
			SourceLanguage: "mocklang",
			TargetLanguage: "go",
			Units:          []string{"unit-1", "unit-2"},
			Waves: []adapterkit.Wave{
				{Wave: 0, UnitIDs: []string{"unit-1"}},
				{Wave: 1, UnitIDs: []string{"unit-2"}},
			},
		},
		units: []adapterkit.Unit{
			{
				SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
				ID:              "unit-1",
				Name:            "Unit 1",
				Wave:            0,
				State:           adapterkit.StatePlanned,
				EvidenceProfile: adapterkit.ProfileReplay,
			},
			{
				SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
				ID:              "unit-2",
				Name:            "Unit 2",
				Wave:            1,
				State:           adapterkit.StatePlanned,
				EvidenceProfile: adapterkit.ProfileReplay,
			},
		},
		coverageLimits: conformance.CoverageLimits{
			MinStatementCoverage: 0.8,
			MaxUnreplayableRatio: 0.1,
			SupportedCaseKinds: []adapterkit.CaseKind{
				adapterkit.KindHappy,
				adapterkit.KindEdge,
				adapterkit.KindTrap,
			},
		},
	}
}

func (m *validMockAdapter) Name() string           { return m.name }
func (m *validMockAdapter) Version() string        { return adapterkit.Version }
func (m *validMockAdapter) SourceLanguage() string { return m.sourceLang }
func (m *validMockAdapter) SupportedTransports() []adapterkit.TransportType {
	return []adapterkit.TransportType{
		adapterkit.TransportInProcess,
		adapterkit.TransportSubprocess,
	}
}
func (m *validMockAdapter) Capabilities() []adapterkit.Capability {
	return m.caps
}
func (m *validMockAdapter) FieldSemantics() []adapterkit.FieldSemantic {
	return m.semantics
}
func (m *validMockAdapter) Analyze(ctx context.Context, repoPath string) (*adapterkit.Manifest, error) {
	if repoPath != "valid-repo" {
		return nil, errors.New("invalid repo")
	}
	return m.manifest, nil
}
func (m *validMockAdapter) DiscoverUnits(ctx context.Context, repoPath string) ([]adapterkit.Unit, error) {
	if repoPath != "valid-repo" {
		return nil, errors.New("invalid repo")
	}
	return m.units, nil
}
func (m *validMockAdapter) CreateReplayDriver(ctx context.Context, unitID string) (adapterkit.Replay, error) {
	return &mockReplayDriver{name: "valid-replay-driver"}, nil
}
func (m *validMockAdapter) Detect(ctx context.Context, repoPath string) (bool, error) {
	if repoPath != "valid-repo" {
		return false, nil
	}
	return true, nil
}
func (m *validMockAdapter) CheckEnvironment(ctx context.Context) error {
	return nil
}
func (m *validMockAdapter) RecordCases(ctx context.Context, unitID string, inputs []adapterkit.CaseInput) ([]*adapterkit.Case, error) {
	var cases []*adapterkit.Case
	for i, in := range inputs {
		cases = append(cases, &adapterkit.Case{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			CaseID:         "case-001",
			UnitID:         unitID,
			Kind:           adapterkit.KindHappy,
			Runtime:        "mocklang",
			CapturedAt:     time.Now().UTC().Format(time.RFC3339),
			Input:          in,
			ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200},
		})
		_ = i
	}
	return cases, nil
}
func (m *validMockAdapter) CoverageLimits() conformance.CoverageLimits {
	return m.coverageLimits
}
func (m *validMockAdapter) CreateReplayDrivers(ctx context.Context, unitID string) (map[adapterkit.TransportType]adapterkit.Replay, error) {
	if m.drivers != nil {
		return m.drivers, nil
	}
	return map[adapterkit.TransportType]adapterkit.Replay{
		adapterkit.TransportInProcess:  &mockReplayDriver{name: "inproc-driver"},
		adapterkit.TransportSubprocess: &mockReplayDriver{name: "subproc-driver"},
	}, nil
}

// Broken Fixtures for Testing Conformance Violations

// brokenMetadataAdapter violates metadata constraints.
type brokenMetadataAdapter struct {
	validMockAdapter
}

func (b *brokenMetadataAdapter) Name() string           { return "" }
func (b *brokenMetadataAdapter) Version() string        { return "" }
func (b *brokenMetadataAdapter) SourceLanguage() string { return "" }
func (b *brokenMetadataAdapter) SupportedTransports() []adapterkit.TransportType {
	return []adapterkit.TransportType{
		adapterkit.TransportType("QuantumTeleportation"),
	}
}

// brokenUnknownCapabilityAdapter declares an unknown capability.
type brokenUnknownCapabilityAdapter struct {
	validMockAdapter
}

func (b *brokenUnknownCapabilityAdapter) Capabilities() []adapterkit.Capability {
	return []adapterkit.Capability{
		adapterkit.Capability("admin_override"),
	}
}

// brokenDuplicateCapabilityAdapter declares duplicate capabilities.
type brokenDuplicateCapabilityAdapter struct {
	validMockAdapter
}

func (b *brokenDuplicateCapabilityAdapter) Capabilities() []adapterkit.Capability {
	return []adapterkit.Capability{
		adapterkit.CapabilityReadRepo,
		adapterkit.CapabilityReadRepo,
	}
}

// brokenEmptyPathFieldSemanticsAdapter declares a field semantic with an empty path.
type brokenEmptyPathFieldSemanticsAdapter struct {
	validMockAdapter
}

func (b *brokenEmptyPathFieldSemanticsAdapter) FieldSemantics() []adapterkit.FieldSemantic {
	return []adapterkit.FieldSemantic{
		{Path: "", Semantic: adapterkit.SemanticSecret},
	}
}

// brokenInvalidKindFieldSemanticsAdapter declares an invalid semantic kind.
type brokenInvalidKindFieldSemanticsAdapter struct {
	validMockAdapter
}

func (b *brokenInvalidKindFieldSemanticsAdapter) FieldSemantics() []adapterkit.FieldSemantic {
	return []adapterkit.FieldSemantic{
		{Path: "user.pass", Semantic: adapterkit.SemanticKind("encrypted_hash")},
	}
}

// brokenDuplicatePathFieldSemanticsAdapter declares duplicate field semantic paths.
type brokenDuplicatePathFieldSemanticsAdapter struct {
	validMockAdapter
}

func (b *brokenDuplicatePathFieldSemanticsAdapter) FieldSemantics() []adapterkit.FieldSemantic {
	return []adapterkit.FieldSemantic{
		{Path: "dup.path", Semantic: adapterkit.SemanticVolatile},
		{Path: "dup.path", Semantic: adapterkit.SemanticOrdered},
	}
}

// brokenDetectionAdapter incorrectly detects invalid repositories and misses valid ones.
type brokenDetectionAdapter struct {
	validMockAdapter
}

func (b *brokenDetectionAdapter) Detect(ctx context.Context, repoPath string) (bool, error) {
	if repoPath == "valid-repo" {
		return false, nil // Fails to detect valid repository
	}
	return true, nil // Incorrectly claims invalid path is valid
}

// brokenAnalysisAdapter returns invalid manifest.
type brokenAnalysisAdapter struct {
	validMockAdapter
}

func (b *brokenAnalysisAdapter) Analyze(ctx context.Context, repoPath string) (*adapterkit.Manifest, error) {
	return &adapterkit.Manifest{
		SchemaVersion:  "v999",
		ProjectName:    "broken",
		SourceLanguage: "unexpected-lang",
		TargetLanguage: "",
		Units:          []string{"unit-A"},
		Waves: []adapterkit.Wave{
			{Wave: -1, UnitIDs: []string{"unit-Z"}}, // negative wave and undeclared unit
		},
	}, nil
}

// brokenDiscoveryAdapter returns units violating schema and lifecycle states.
type brokenDiscoveryAdapter struct {
	validMockAdapter
}

func (b *brokenDiscoveryAdapter) DiscoverUnits(ctx context.Context, repoPath string) ([]adapterkit.Unit, error) {
	return []adapterkit.Unit{
		{
			SchemaVersion:   "v0",
			ID:              "",
			Name:            "",
			State:           adapterkit.UnitState("Finished"),         // invalid state
			EvidenceProfile: adapterkit.EvidenceProfile("unverified"), // invalid profile
		},
	}, nil
}

// brokenCaseSchemaAdapter returns cases violating case schema constraints.
type brokenCaseSchemaAdapter struct {
	validMockAdapter
}

func (b *brokenCaseSchemaAdapter) RecordCases(ctx context.Context, unitID string, inputs []adapterkit.CaseInput) ([]*adapterkit.Case, error) {
	return []*adapterkit.Case{
		{
			SchemaVersion: "bad-version",
			CaseID:        "",
			UnitID:        "",
			Kind:          adapterkit.CaseKind("imaginary"),
			Runtime:       "",
			CapturedAt:    "invalid-timestamp-string",
		},
	}, nil
}

// brokenReplayTransportMismatchAdapter simulates divergent replay output across transports.
type brokenReplayTransportMismatchAdapter struct {
	validMockAdapter
}

func (b *brokenReplayTransportMismatchAdapter) CreateReplayDrivers(ctx context.Context, unitID string) (map[adapterkit.TransportType]adapterkit.Replay, error) {
	return map[adapterkit.TransportType]adapterkit.Replay{
		adapterkit.TransportInProcess: &mockReplayDriver{
			name: "inproc-driver",
			output: &adapterkit.CaseOutput{
				StatusCode: 200,
				ExitCode:   0,
				Stdout:     "success",
				Body:       map[string]any{"data": 123},
			},
		},
		adapterkit.TransportSubprocess: &mockReplayDriver{
			name: "subproc-driver",
			output: &adapterkit.CaseOutput{
				StatusCode:   500,
				ExitCode:     1,
				Stdout:       "failed",
				ErrorMessage: "subprocess internal crash",
				Body:         map[string]any{"error": "fatal"},
			},
		},
	}, nil
}

// brokenCoverageLimitsAdapter declares invalid coverage limits.
type brokenCoverageLimitsAdapter struct {
	validMockAdapter
}

func (b *brokenCoverageLimitsAdapter) CoverageLimits() conformance.CoverageLimits {
	return conformance.CoverageLimits{
		MinStatementCoverage: 1.5,  // > 1.0
		MaxUnreplayableRatio: -0.2, // < 0.0
		SupportedCaseKinds: []adapterkit.CaseKind{
			adapterkit.CaseKind("unsupported_kind"),
		},
	}
}
