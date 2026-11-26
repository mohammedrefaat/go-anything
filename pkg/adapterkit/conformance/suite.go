package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// Suite manages and executes the full suite of conformance checks against a LanguageAdapter.
type Suite struct {
	adapter adapterkit.LanguageAdapter
	config  Config
}

// NewSuite constructs a new conformance test suite for the given adapter and configuration.
func NewSuite(adapter adapterkit.LanguageAdapter, cfg Config) *Suite {
	return &Suite{
		adapter: adapter,
		config:  cfg,
	}
}

// RunSuite runs the conformance test suite directly on a testing.T runner.
func RunSuite(t *testing.T, adapter adapterkit.LanguageAdapter, cfg Config) {
	t.Helper()
	NewSuite(adapter, cfg).Run(t)
}

// Run executes the complete conformance suite using testing.T subtests.
func (s *Suite) Run(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	t.Run(string(CheckMetadata), func(t *testing.T) {
		r := &Report{Passed: true}
		s.checkMetadata(r)
		s.assertReport(t, r)
	})

	t.Run(string(CheckDetectionAndEnv), func(t *testing.T) {
		r := &Report{Passed: true}
		s.checkDetectionAndEnvironment(ctx, r)
		s.assertReport(t, r)
	})

	t.Run(string(CheckCapabilities), func(t *testing.T) {
		r := &Report{Passed: true}
		s.checkCapabilities(r)
		s.assertReport(t, r)
	})

	t.Run(string(CheckFieldSemantics), func(t *testing.T) {
		r := &Report{Passed: true}
		s.checkFieldSemantics(r)
		s.assertReport(t, r)
	})

	t.Run(string(CheckAnalysisAndDiscovery), func(t *testing.T) {
		r := &Report{Passed: true}
		s.checkAnalysisAndDiscovery(ctx, r)
		s.assertReport(t, r)
	})

	t.Run(string(CheckCaseSchema), func(t *testing.T) {
		r := &Report{Passed: true}
		s.checkCaseSchema(ctx, r)
		s.assertReport(t, r)
	})

	t.Run(string(CheckReplayAcrossTransports), func(t *testing.T) {
		r := &Report{Passed: true}
		s.checkReplayTransports(ctx, r)
		s.assertReport(t, r)
	})

	t.Run(string(CheckCoverageLimits), func(t *testing.T) {
		r := &Report{Passed: true}
		s.checkCoverageLimits(r)
		s.assertReport(t, r)
	})
}

// Validate executes all conformance checks programmatically without failing testing.T,
// returning a Report containing all discovered violations.
func (s *Suite) Validate(ctx context.Context) *Report {
	r := &Report{Passed: true}
	s.checkMetadata(r)
	s.checkDetectionAndEnvironment(ctx, r)
	s.checkCapabilities(r)
	s.checkFieldSemantics(r)
	s.checkAnalysisAndDiscovery(ctx, r)
	s.checkCaseSchema(ctx, r)
	s.checkReplayTransports(ctx, r)
	s.checkCoverageLimits(r)
	return r
}

func (s *Suite) assertReport(t *testing.T, r *Report) {
	t.Helper()
	if !r.Passed {
		for _, v := range r.Violations {
			t.Errorf("[%s] %s", v.Check, v.Description)
		}
	}
}

func (s *Suite) checkMetadata(r *Report) {
	if s.adapter == nil {
		r.AddViolation(CheckMetadata, "adapter implementation is nil")
		return
	}

	if s.adapter.Name() == "" {
		r.AddViolation(CheckMetadata, "adapter name cannot be empty")
	}

	if s.adapter.Version() == "" {
		r.AddViolation(CheckMetadata, "adapter version cannot be empty")
	}

	if s.adapter.SourceLanguage() == "" {
		r.AddViolation(CheckMetadata, "adapter source language cannot be empty")
	}

	transports := s.adapter.SupportedTransports()
	if len(transports) == 0 {
		r.AddViolation(CheckMetadata, "supported transports cannot be empty")
	}

	seenTransports := make(map[adapterkit.TransportType]struct{}, len(transports))
	for _, tr := range transports {
		if _, ok := adapterkit.ValidTransportTypes[tr]; !ok {
			r.AddViolation(CheckMetadata, "invalid transport type %q declared by adapter", tr)
		}
		if _, ok := seenTransports[tr]; ok {
			r.AddViolation(CheckMetadata, "duplicate transport type %q declared by adapter", tr)
		}
		seenTransports[tr] = struct{}{}
	}
}

func (s *Suite) checkDetectionAndEnvironment(ctx context.Context, r *Report) {
	if s.adapter == nil {
		return
	}

	// 1. Environment checking
	if envChecker, ok := s.adapter.(EnvironmentChecker); ok {
		if err := envChecker.CheckEnvironment(ctx); err != nil {
			r.AddViolation(CheckDetectionAndEnv, "environment check failed: %v", err)
		}
	}

	// 2. Repository detection
	invalidPath := s.config.InvalidRepoPath
	if invalidPath == "" {
		invalidPath = filepath.Join(os.TempDir(), "ga-invalid-repo-conformance-check-999999")
	}

	if detector, ok := s.adapter.(Detector); ok {
		// Valid repository must be detected if provided
		if s.config.RepoPath != "" {
			detected, err := detector.Detect(ctx, s.config.RepoPath)
			if err != nil {
				r.AddViolation(CheckDetectionAndEnv, "detection error on valid repository %q: %v", s.config.RepoPath, err)
			} else if !detected {
				r.AddViolation(CheckDetectionAndEnv, "detection failed: valid repository %q was not detected as %s",
					s.config.RepoPath, s.adapter.SourceLanguage())
			}
		}

		// Invalid repository must not be detected
		detected, err := detector.Detect(ctx, invalidPath)
		if err == nil && detected {
			r.AddViolation(CheckDetectionAndEnv, "detection failed: invalid repository path %q was incorrectly detected as valid", invalidPath)
		}
	} else {
		// Adapters without explicit Detector interface must reject non-existent / invalid repositories during Analyze
		if _, err := s.adapter.Analyze(ctx, invalidPath); err == nil {
			r.AddViolation(CheckDetectionAndEnv, "detection check failed: adapter accepted invalid repository path %q without error", invalidPath)
		}
	}
}

func (s *Suite) checkCapabilities(r *Report) {
	if s.adapter == nil {
		return
	}

	caps := s.adapter.Capabilities()
	if err := adapterkit.ValidateCapabilities(caps); err != nil {
		r.AddViolation(CheckCapabilities, "invalid capabilities declaration: %v", err)
	}

	// Verify capability enforcement: undeclared actions must fail closed
	guard := NewCapabilityGuard(caps)
	allKnownCaps := []adapterkit.Capability{
		adapterkit.CapabilityReadRepo,
		adapterkit.CapabilityRunOriginal,
		adapterkit.CapabilityNetwork,
	}

	for _, c := range allKnownCaps {
		if !guard.Has(c) {
			err := guard.Execute(c, func() error {
				return nil
			})
			if err == nil {
				r.AddViolation(CheckCapabilities, "capability enforcement failure: undeclared capability %q was allowed without error", c)
			}
		}
	}

	// Optional probe action check
	if s.config.ProbeAction != nil {
		if err := s.config.ProbeAction("test_probe"); err != nil {
			r.AddViolation(CheckCapabilities, "probe action failed: %v", err)
		}
	}
}

func (s *Suite) checkFieldSemantics(r *Report) {
	if s.adapter == nil {
		return
	}

	semantics := s.adapter.FieldSemantics()
	if err := adapterkit.ValidateFieldSemantics(semantics); err != nil {
		r.AddViolation(CheckFieldSemantics, "invalid field semantics declaration: %v", err)
	}

	seenPaths := make(map[string]struct{}, len(semantics))
	for _, sem := range semantics {
		if _, exists := seenPaths[sem.Path]; exists {
			r.AddViolation(CheckFieldSemantics, "duplicate field semantic declared for path %q", sem.Path)
		}
		seenPaths[sem.Path] = struct{}{}
	}
}

func (s *Suite) checkAnalysisAndDiscovery(ctx context.Context, r *Report) {
	if s.adapter == nil {
		return
	}

	if s.config.RepoPath == "" {
		// When no repo fixture is configured, analysis cannot be run
		return
	}

	manifest, err := s.adapter.Analyze(ctx, s.config.RepoPath)
	if err != nil {
		r.AddViolation(CheckAnalysisAndDiscovery, "analysis failed on repository %q: %v", s.config.RepoPath, err)
		return
	}

	if manifest == nil {
		r.AddViolation(CheckAnalysisAndDiscovery, "analysis returned nil manifest")
		return
	}

	if manifest.SchemaVersion != adapterkit.SchemaVersionV1Alpha1 && manifest.SchemaVersion != adapterkit.SchemaVersionV1 {
		r.AddViolation(CheckAnalysisAndDiscovery, "manifest schema_version must be %q, got %q",
			adapterkit.SchemaVersionV1Alpha1, manifest.SchemaVersion)
	}

	if manifest.SourceLanguage != s.adapter.SourceLanguage() {
		r.AddViolation(CheckAnalysisAndDiscovery, "manifest source_language mismatch: expected %q, got %q",
			s.adapter.SourceLanguage(), manifest.SourceLanguage)
	}

	if manifest.TargetLanguage == "" {
		r.AddViolation(CheckAnalysisAndDiscovery, "manifest target_language cannot be empty")
	}

	if len(manifest.Units) == 0 {
		r.AddViolation(CheckAnalysisAndDiscovery, "manifest units cannot be empty for fixture repository")
	}

	if len(manifest.Waves) == 0 {
		r.AddViolation(CheckAnalysisAndDiscovery, "manifest waves cannot be empty for fixture repository")
	}

	// Verify all wave units exist in manifest units
	unitMap := make(map[string]struct{}, len(manifest.Units))
	for _, u := range manifest.Units {
		unitMap[u] = struct{}{}
	}

	waveUnitsCount := 0
	for _, wave := range manifest.Waves {
		if wave.Wave < 0 {
			r.AddViolation(CheckAnalysisAndDiscovery, "invalid negative wave number %d", wave.Wave)
		}
		for _, uid := range wave.UnitIDs {
			waveUnitsCount++
			if _, ok := unitMap[uid]; !ok {
				r.AddViolation(CheckAnalysisAndDiscovery, "wave unit %q is not declared in manifest.units", uid)
			}
		}
	}

	if waveUnitsCount < len(manifest.Units) {
		r.AddViolation(CheckAnalysisAndDiscovery, "waves do not cover all declared units (%d in waves vs %d in manifest)",
			waveUnitsCount, len(manifest.Units))
	}

	// DiscoverUnits verification
	units, err := s.adapter.DiscoverUnits(ctx, s.config.RepoPath)
	if err != nil {
		r.AddViolation(CheckAnalysisAndDiscovery, "unit discovery failed on repository %q: %v", s.config.RepoPath, err)
		return
	}

	if len(units) == 0 {
		r.AddViolation(CheckAnalysisAndDiscovery, "discover units returned zero units")
		return
	}

	validStates := map[adapterkit.UnitState]struct{}{
		adapterkit.StatePlanned:    {},
		adapterkit.StateRecorded:   {},
		adapterkit.StateReady:      {},
		adapterkit.StateInProgress: {},
		adapterkit.StateVerified:   {},
		adapterkit.StateShadow:     {},
		adapterkit.StateApproved:   {},
		adapterkit.StateCutover:    {},
		adapterkit.StateCompleted:  {},
		adapterkit.StateBlocked:    {},
	}

	for _, u := range units {
		if u.ID == "" {
			r.AddViolation(CheckAnalysisAndDiscovery, "discovered unit has empty ID")
		}
		if u.Name == "" {
			r.AddViolation(CheckAnalysisAndDiscovery, "discovered unit %s has empty Name", u.ID)
		}
		if u.SchemaVersion != adapterkit.SchemaVersionV1Alpha1 && u.SchemaVersion != adapterkit.SchemaVersionV1 {
			r.AddViolation(CheckAnalysisAndDiscovery, "discovered unit %s has schema_version %q, expected %q",
				u.ID, u.SchemaVersion, adapterkit.SchemaVersionV1Alpha1)
		}
		if _, ok := validStates[u.State]; !ok {
			r.AddViolation(CheckAnalysisAndDiscovery, "discovered unit %s has invalid lifecycle state %q", u.ID, u.State)
		}
		if u.EvidenceProfile != adapterkit.ProfileReplay && u.EvidenceProfile != adapterkit.ProfileFull {
			r.AddViolation(CheckAnalysisAndDiscovery, "discovered unit %s has invalid evidence profile %q", u.ID, u.EvidenceProfile)
		}
	}
}

func (s *Suite) checkCaseSchema(ctx context.Context, r *Report) {
	if s.adapter == nil {
		return
	}

	// 1. Adapter case recording check if implemented
	if recorder, ok := s.adapter.(CaseRecorder); ok {
		inputs := []adapterkit.CaseInput{
			{Method: "test", Path: "/ping"},
		}
		unitID := s.config.TargetUnitID
		if unitID == "" {
			unitID = "test-unit"
		}
		cases, err := recorder.RecordCases(ctx, unitID, inputs)
		if err != nil {
			r.AddViolation(CheckCaseSchema, "case recording failed: %v", err)
		} else {
			for _, c := range cases {
				s.validateCase(c, r)
			}
		}
	}

	// 2. Validate sample cases
	for _, c := range s.config.SampleCases {
		s.validateCase(c, r)
	}
}

func (s *Suite) validateCase(c *adapterkit.Case, r *Report) {
	if c == nil {
		r.AddViolation(CheckCaseSchema, "recorded case is nil")
		return
	}

	if c.SchemaVersion != adapterkit.SchemaVersionV1Alpha1 && c.SchemaVersion != adapterkit.SchemaVersionV1 {
		r.AddViolation(CheckCaseSchema, "case %s schema_version must be %q, got %q",
			c.CaseID, adapterkit.SchemaVersionV1Alpha1, c.SchemaVersion)
	}

	if c.CaseID == "" {
		r.AddViolation(CheckCaseSchema, "case has empty case_id")
	}

	if c.UnitID == "" {
		r.AddViolation(CheckCaseSchema, "case %s has empty unit_id", c.CaseID)
	}

	validKinds := map[adapterkit.CaseKind]struct{}{
		adapterkit.KindHappy:    {},
		adapterkit.KindEdge:     {},
		adapterkit.KindTrap:     {},
		adapterkit.KindFuzz:     {},
		adapterkit.KindCaptured: {},
	}
	if _, ok := validKinds[c.Kind]; !ok {
		r.AddViolation(CheckCaseSchema, "case %s has invalid kind %q", c.CaseID, c.Kind)
	}

	if c.Runtime == "" {
		r.AddViolation(CheckCaseSchema, "case %s has empty runtime", c.CaseID)
	}

	if c.CapturedAt == "" {
		r.AddViolation(CheckCaseSchema, "case %s has empty captured_at timestamp", c.CaseID)
	} else if _, err := time.Parse(time.RFC3339, c.CapturedAt); err != nil {
		r.AddViolation(CheckCaseSchema, "case %s has invalid RFC3339 captured_at timestamp %q: %v",
			c.CaseID, c.CapturedAt, err)
	}

	// JSON serialization round-trip
	data, err := json.Marshal(c)
	if err != nil {
		r.AddViolation(CheckCaseSchema, "case %s failed JSON marshaling: %v", c.CaseID, err)
		return
	}

	var roundTripped adapterkit.Case
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		r.AddViolation(CheckCaseSchema, "case %s failed JSON unmarshaling round-trip: %v", c.CaseID, err)
	}
}

func (s *Suite) checkReplayTransports(ctx context.Context, r *Report) {
	if s.adapter == nil {
		return
	}

	unitID := s.config.TargetUnitID
	if unitID == "" {
		unitID = "default-unit"
	}

	// 1. Cross-transport comparison if multi-transport provider is available
	if provider, ok := s.adapter.(MultiTransportReplayProvider); ok {
		drivers, err := provider.CreateReplayDrivers(ctx, unitID)
		if err != nil {
			r.AddViolation(CheckReplayAcrossTransports, "failed to create multi-transport replay drivers: %v", err)
			return
		}
		defer func() {
			for _, d := range drivers {
				_ = d.Close()
			}
		}()

		for _, sampleCase := range s.config.SampleCases {
			discrepancies, err := CompareReplayDrivers(ctx, sampleCase, drivers)
			if err != nil {
				r.AddViolation(CheckReplayAcrossTransports, "replay execution error: %v", err)
				continue
			}
			for _, disc := range discrepancies {
				r.AddViolation(CheckReplayAcrossTransports, "cross-transport replay discrepancy: %s", disc)
			}
		}
	}

	// 2. Standard single-transport replay driver check
	driver, err := s.adapter.CreateReplayDriver(ctx, unitID)
	if err != nil {
		r.AddViolation(CheckReplayAcrossTransports, "CreateReplayDriver returned error: %v", err)
		return
	}

	if driver != nil {
		defer driver.Close()

		if driver.DriverName() == "" {
			r.AddViolation(CheckReplayAcrossTransports, "replay driver name cannot be empty")
		}

		for _, sampleCase := range s.config.SampleCases {
			if _, err := driver.ExecuteCase(ctx, sampleCase); err != nil {
				r.AddViolation(CheckReplayAcrossTransports, "replay execution error on case %s: %v",
					sampleCase.CaseID, err)
			}
		}
	} else if s.config.StrictReplay {
		r.AddViolation(CheckReplayAcrossTransports, "strict replay enabled but CreateReplayDriver returned nil driver")
	}
}

func (s *Suite) checkCoverageLimits(r *Report) {
	if s.adapter == nil {
		return
	}

	var limits CoverageLimits
	if limiter, ok := s.adapter.(CoverageLimiter); ok {
		limits = limiter.CoverageLimits()
	} else if s.config.DeclaredLimits != nil {
		limits = *s.config.DeclaredLimits
	} else {
		// No declared coverage limits to check
		return
	}

	if limits.MinStatementCoverage < 0.0 || limits.MinStatementCoverage > 1.0 {
		r.AddViolation(CheckCoverageLimits, "min statement coverage must be between 0.0 and 1.0, got %f",
			limits.MinStatementCoverage)
	}

	if limits.MaxUnreplayableRatio < 0.0 || limits.MaxUnreplayableRatio > 1.0 {
		r.AddViolation(CheckCoverageLimits, "max unreplayable ratio must be between 0.0 and 1.0, got %f",
			limits.MaxUnreplayableRatio)
	}

	validKinds := map[adapterkit.CaseKind]struct{}{
		adapterkit.KindHappy:    {},
		adapterkit.KindEdge:     {},
		adapterkit.KindTrap:     {},
		adapterkit.KindFuzz:     {},
		adapterkit.KindCaptured: {},
	}
	for _, k := range limits.SupportedCaseKinds {
		if _, ok := validKinds[k]; !ok {
			r.AddViolation(CheckCoverageLimits, "invalid supported case kind %q declared in coverage limits", k)
		}
	}

	// Check sample cases against declared limits
	if len(s.config.SampleCases) > 0 {
		supportedKindMap := make(map[adapterkit.CaseKind]struct{}, len(limits.SupportedCaseKinds))
		for _, k := range limits.SupportedCaseKinds {
			supportedKindMap[k] = struct{}{}
		}

		for _, c := range s.config.SampleCases {
			if len(supportedKindMap) > 0 {
				if _, ok := supportedKindMap[c.Kind]; !ok {
					r.AddViolation(CheckCoverageLimits, "case %s kind %q exceeds declared supported case kinds %v",
						c.CaseID, c.Kind, limits.SupportedCaseKinds)
				}
			}

			// If MaxUnreplayableRatio is set, evaluate unreplayable fields
			if limits.MaxUnreplayableRatio > 0 && len(c.UnreplayableFields) > 0 {
				// E.g., if input has fields and unreplayable count is high
				if len(c.UnreplayableFields) > 10 { // Excessive unreplayable fields check
					r.AddViolation(CheckCoverageLimits, "case %s unreplayable fields count (%d) exceeds allowable coverage limit",
						c.CaseID, len(c.UnreplayableFields))
				}
			}
		}
	}
}
