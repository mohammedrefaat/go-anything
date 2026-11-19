package conformance_test

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/adapters/source/golang"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit/conformance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRule8_NoInternalImports verifies that pkg/adapterkit/conformance never imports internal/ packages.
func TestRule8_NoInternalImports(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ImportsOnly)
	require.NoError(t, err)

	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			// Skip test files when inspecting package imports
			if strings.HasSuffix(fileName, "_test.go") {
				continue
			}
			for _, imp := range file.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)
				assert.False(
					t,
					strings.Contains(importPath, "internal"),
					"file %s imports forbidden internal package %s",
					filepath.Base(fileName),
					importPath,
				)
			}
		}
	}
}

// TestValidMockAdapter_PassesAllChecks verifies that a fully compliant adapter passes all checks.
func TestValidMockAdapter_PassesAllChecks(t *testing.T) {
	adapter := newValidMockAdapter()
	sampleCase := &adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         "case-001",
		UnitID:         "unit-1",
		Kind:           adapterkit.KindHappy,
		Runtime:        "mocklang",
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input:          adapterkit.CaseInput{Method: "GET", Path: "/status"},
		ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200},
	}

	cfg := conformance.Config{
		RepoPath:     "valid-repo",
		TargetUnitID: "unit-1",
		SampleCases:  []*adapterkit.Case{sampleCase},
	}

	conformance.RunSuite(t, adapter, cfg)
}

// helper to create a temporary valid Go repository fixture.
func createTestGoRepo(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()

	goMod := `module example.com/conftest
go 1.22
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644))

	// Leaf package 1: pkg/core
	coreDir := filepath.Join(tempDir, "pkg", "core")
	require.NoError(t, os.MkdirAll(coreDir, 0755))
	coreSrc := `package core
type Item struct {
	ID string
}
`
	require.NoError(t, os.WriteFile(filepath.Join(coreDir, "item.go"), []byte(coreSrc), 0644))

	// Package 2: pkg/service (depends on core)
	svcDir := filepath.Join(tempDir, "pkg", "service")
	require.NoError(t, os.MkdirAll(svcDir, 0755))
	svcSrc := `package service
import "example.com/conftest/pkg/core"
type Service struct {
	Items []core.Item
}
`
	require.NoError(t, os.WriteFile(filepath.Join(svcDir, "service.go"), []byte(svcSrc), 0644))

	return tempDir
}

// TestGolangAdapter_PassesConformanceSuite tests that the real Go source adapter passes the suite.
func TestGolangAdapter_PassesConformanceSuite(t *testing.T) {
	repoDir := createTestGoRepo(t)
	adapter := golang.NewAdapter()

	cfg := conformance.Config{
		RepoPath:     repoDir,
		TargetUnitID: "pkg/core",
	}

	conformance.RunSuite(t, adapter, cfg)
}

// TestBrokenFixtureAdapters_FailWithDescriptiveMessages tests that deliberately broken
// adapters fail each respective check with clear, descriptive violation messages.
func TestBrokenFixtureAdapters_FailWithDescriptiveMessages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                 string
		adapter              adapterkit.LanguageAdapter
		cfg                  conformance.Config
		expectedCheck        conformance.CheckName
		expectedMessageParts []string
	}{
		{
			name:    "broken metadata adapter",
			adapter: &brokenMetadataAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckMetadata,
			expectedMessageParts: []string{
				"adapter name cannot be empty",
				"adapter version cannot be empty",
				"adapter source language cannot be empty",
				"invalid transport type \"QuantumTeleportation\"",
			},
		},
		{
			name:    "broken unknown capability adapter",
			adapter: &brokenUnknownCapabilityAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckCapabilities,
			expectedMessageParts: []string{
				"unknown capability declared: \"admin_override\"",
			},
		},
		{
			name:    "broken duplicate capability adapter",
			adapter: &brokenDuplicateCapabilityAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckCapabilities,
			expectedMessageParts: []string{
				"duplicate capability declared: \"read_repo\"",
			},
		},
		{
			name:    "broken empty path field semantics adapter",
			adapter: &brokenEmptyPathFieldSemanticsAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckFieldSemantics,
			expectedMessageParts: []string{
				"empty path",
			},
		},
		{
			name:    "broken invalid kind field semantics adapter",
			adapter: &brokenInvalidKindFieldSemanticsAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckFieldSemantics,
			expectedMessageParts: []string{
				"invalid semantic kind \"encrypted_hash\"",
			},
		},
		{
			name:    "broken duplicate path field semantics adapter",
			adapter: &brokenDuplicatePathFieldSemanticsAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckFieldSemantics,
			expectedMessageParts: []string{
				"duplicate field semantic declared for path \"dup.path\"",
			},
		},
		{
			name:    "broken detection adapter",
			adapter: &brokenDetectionAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckDetectionAndEnv,
			expectedMessageParts: []string{
				"valid repository \"valid-repo\" was not detected",
				"invalid repository path",
				"incorrectly detected as valid",
			},
		},
		{
			name:    "broken analysis adapter",
			adapter: &brokenAnalysisAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckAnalysisAndDiscovery,
			expectedMessageParts: []string{
				"manifest schema_version must be \"v1alpha1\", got \"v999\"",
				"manifest source_language mismatch: expected \"mocklang\", got \"unexpected-lang\"",
				"manifest target_language cannot be empty",
				"invalid negative wave number -1",
				"wave unit \"unit-Z\" is not declared in manifest.units",
			},
		},
		{
			name:    "broken discovery adapter",
			adapter: &brokenDiscoveryAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckAnalysisAndDiscovery,
			expectedMessageParts: []string{
				"discovered unit has empty ID",
				"discovered unit  has empty Name",
				"schema_version \"v0\", expected \"v1alpha1\"",
				"invalid lifecycle state \"Finished\"",
				"invalid evidence profile \"unverified\"",
			},
		},
		{
			name:    "broken case schema adapter",
			adapter: &brokenCaseSchemaAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath:     "valid-repo",
				TargetUnitID: "unit-1",
			},
			expectedCheck: conformance.CheckCaseSchema,
			expectedMessageParts: []string{
				"schema_version must be \"v1alpha1\", got \"bad-version\"",
				"case has empty case_id",
				"has empty unit_id",
				"has invalid kind \"imaginary\"",
				"has empty runtime",
				"invalid RFC3339 captured_at timestamp \"invalid-timestamp-string\"",
			},
		},
		{
			name:    "broken replay transport mismatch adapter",
			adapter: &brokenReplayTransportMismatchAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath:     "valid-repo",
				TargetUnitID: "unit-1",
				SampleCases: []*adapterkit.Case{
					{
						SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
						CaseID:         "case-diverge-001",
						UnitID:         "unit-1",
						Kind:           adapterkit.KindHappy,
						Runtime:        "mocklang",
						CapturedAt:     time.Now().UTC().Format(time.RFC3339),
						Input:          adapterkit.CaseInput{Method: "POST", Path: "/submit"},
						ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200},
					},
				},
			},
			expectedCheck: conformance.CheckReplayAcrossTransports,
			expectedMessageParts: []string{
				"status code divergence between InProcess (200) and Subprocess (500)",
				"exit code divergence between InProcess (0) and Subprocess (1)",
				"stdout divergence between InProcess (\"success\") and Subprocess (\"failed\")",
				"error message divergence between InProcess (\"\") and Subprocess (\"subprocess internal crash\")",
				"body divergence between InProcess and Subprocess",
			},
		},
		{
			name:    "broken coverage limits adapter",
			adapter: &brokenCoverageLimitsAdapter{*newValidMockAdapter()},
			cfg: conformance.Config{
				RepoPath: "valid-repo",
			},
			expectedCheck: conformance.CheckCoverageLimits,
			expectedMessageParts: []string{
				"min statement coverage must be between 0.0 and 1.0, got 1.5",
				"max unreplayable ratio must be between 0.0 and 1.0, got -0.2",
				"invalid supported case kind \"unsupported_kind\" declared in coverage limits",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			suite := conformance.NewSuite(tc.adapter, tc.cfg)
			report := suite.Validate(ctx)

			assert.False(t, report.Passed, "expected report to fail for broken fixture")
			assert.True(t, report.HasCheckViolation(tc.expectedCheck),
				"expected violation for check %q in %v", tc.expectedCheck, report.Violations)

			summary := report.Summary()
			for _, part := range tc.expectedMessageParts {
				assert.Contains(t, summary, part, "expected violation summary to contain %q", part)
			}
		})
	}
}

// TestCapabilityGuard_EnforcesUndeclaredActions verifies fail-closed security when undeclared actions are taken.
func TestCapabilityGuard_EnforcesUndeclaredActions(t *testing.T) {
	// Adapter only declares read_repo
	caps := []adapterkit.Capability{adapterkit.CapabilityReadRepo}
	guard := conformance.NewCapabilityGuard(caps)

	// 1. Declared action succeeds
	readExecuted := false
	err := guard.Execute(adapterkit.CapabilityReadRepo, func() error {
		readExecuted = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, readExecuted)

	// 2. Undeclared run_original fails closed
	runExecuted := false
	err = guard.Execute(adapterkit.CapabilityRunOriginal, func() error {
		runExecuted = true
		return nil
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, adapterkit.ErrUndeclaredCapability)
	assert.False(t, runExecuted)
	assert.Contains(t, err.Error(), `required capability "run_original" is not declared`)

	// 3. Undeclared network fails closed
	netExecuted := false
	err = guard.Execute(adapterkit.CapabilityNetwork, func() error {
		netExecuted = true
		return nil
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, adapterkit.ErrUndeclaredCapability)
	assert.False(t, netExecuted)
	assert.Contains(t, err.Error(), `required capability "network" is not declared`)
}

// TestCompareReplayOutputs verifies fine-grained cross-transport discrepancy detection.
func TestCompareReplayOutputs(t *testing.T) {
	caseID := "case-100"

	// 1. Single transport or empty -> no discrepancies
	assert.Empty(t, conformance.CompareReplayOutputs(caseID, nil))
	assert.Empty(t, conformance.CompareReplayOutputs(caseID, map[adapterkit.TransportType]*adapterkit.CaseOutput{
		adapterkit.TransportInProcess: {StatusCode: 200},
	}))

	// 2. Identical outputs across multiple transports -> no discrepancies
	identicalOutputs := map[adapterkit.TransportType]*adapterkit.CaseOutput{
		adapterkit.TransportInProcess: {
			StatusCode: 200,
			Stdout:     "hello",
			Body:       map[string]any{"count": 1},
		},
		adapterkit.TransportSubprocess: {
			StatusCode: 200,
			Stdout:     "hello",
			Body:       map[string]any{"count": 1},
		},
		adapterkit.TransportHTTP: {
			StatusCode: 200,
			Stdout:     "hello",
			Body:       map[string]any{"count": 1},
		},
	}
	assert.Empty(t, conformance.CompareReplayOutputs(caseID, identicalOutputs))

	// 3. Status and Body discrepancy
	divergentOutputs := map[adapterkit.TransportType]*adapterkit.CaseOutput{
		adapterkit.TransportInProcess: {
			StatusCode: 200,
			Body:       map[string]any{"status": "ok"},
		},
		adapterkit.TransportSubprocess: {
			StatusCode: 400,
			Body:       map[string]any{"status": "bad request"},
		},
	}
	disc := conformance.CompareReplayOutputs(caseID, divergentOutputs)
	require.Len(t, disc, 2)
	assert.Contains(t, disc[0], "status code divergence between InProcess (200) and Subprocess (400)")
	assert.Contains(t, disc[1], "body divergence between InProcess and Subprocess")
}
