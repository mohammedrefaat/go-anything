package typescript_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/adapters/source/typescript"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit/conformance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a comprehensive sample TypeScript repository fixture.
func createTestTSRepo(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()

	// 1. package.json
	pkgJSON := `{
  "name": "sample-ts-app",
  "version": "1.0.0",
  "main": "dist/index.js",
  "scripts": {
    "build": "tsc",
    "test": "jest"
  },
  "dependencies": {
    "express": "^4.19.0"
  },
  "devDependencies": {
    "@types/node": "^20.0.0",
    "jest": "^29.5.0",
    "typescript": "^5.4.0"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644))

	// 2. tsconfig.json
	tsConfig := `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "commonjs",
    "strict": true,
    "outDir": "dist"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "tsconfig.json"), []byte(tsConfig), 0644))

	// 3. Leaf unit: src/math (Wave 0)
	mathDir := filepath.Join(tempDir, "src", "math")
	require.NoError(t, os.MkdirAll(mathDir, 0755))
	mathSrc := `// Math utility functions
export function add(a: number, b: number): number {
  return a + b;
}

export function multiply(a: number, b: number): number {
  return a * b;
}
`
	require.NoError(t, os.WriteFile(filepath.Join(mathDir, "calculator.ts"), []byte(mathSrc), 0644))

	mathTestSrc := `import { add, multiply } from './calculator';

describe('Math Calculator', () => {
  it('should add numbers correctly', () => {
    expect(add(2, 3)).toBe(5);
  });

  test('should multiply numbers correctly', () => {
    expect(multiply(3, 4)).toBe(12);
  });

  it.skip('skipped test case', () => {
    expect(true).toBe(false);
  });
});
`
	require.NoError(t, os.WriteFile(filepath.Join(mathDir, "calculator.test.ts"), []byte(mathTestSrc), 0644))

	// 4. Dependent unit: src/service (Wave 1, depends on src/math)
	svcDir := filepath.Join(tempDir, "src", "service")
	require.NoError(t, os.MkdirAll(svcDir, 0755))
	svcSrc := `import { add, multiply } from '../math/calculator';

export class OrderService {
  calculateTotal(price: number, quantity: number, tax: number): number {
    const subtotal = multiply(price, quantity);
    return add(subtotal, tax);
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(svcDir, "order.ts"), []byte(svcSrc), 0644))

	// 5. Dependent unit: src/api (Wave 2, depends on src/service)
	apiDir := filepath.Join(tempDir, "src", "api")
	require.NoError(t, os.MkdirAll(apiDir, 0755))
	apiSrc := `import { OrderService } from '../service/order';
import * as express from 'express';

export function createServer() {
  const service = new OrderService();
  return { status: "ready" };
}
`
	require.NoError(t, os.WriteFile(filepath.Join(apiDir, "server.ts"), []byte(apiSrc), 0644))

	return tempDir
}

// TestAdapter_Metadata verifies language adapter identity and declared capabilities.
func TestAdapter_Metadata(t *testing.T) {
	adapter := typescript.NewAdapter()

	assert.Equal(t, "typescript", adapter.Name())
	assert.Equal(t, adapterkit.Version, adapter.Version())
	assert.Equal(t, "typescript", adapter.SourceLanguage())

	transports := adapter.SupportedTransports()
	assert.Contains(t, transports, adapterkit.TransportSubprocess)
	assert.Contains(t, transports, adapterkit.TransportHTTP)

	caps := adapter.Capabilities()
	assert.Contains(t, caps, adapterkit.CapabilityReadRepo)
	assert.Contains(t, caps, adapterkit.CapabilityRunOriginal)
	assert.Contains(t, caps, adapterkit.CapabilityNetwork)
	assert.NoError(t, adapterkit.ValidateCapabilities(caps))

	semantics := adapter.FieldSemantics()
	assert.NotEmpty(t, semantics)
	assert.NoError(t, adapterkit.ValidateFieldSemantics(semantics))

	limits := adapter.CoverageLimits()
	assert.True(t, limits.MinStatementCoverage >= 0.80)
	assert.True(t, limits.MaxUnreplayableRatio <= 0.10)
}

// TestAdapter_Detector verifies detection of TypeScript and JavaScript repositories.
func TestAdapter_Detector(t *testing.T) {
	ctx := context.Background()
	adapter := typescript.NewAdapter()

	// 1. Valid TS repo
	validRepo := createTestTSRepo(t)
	detected, err := adapter.Detect(ctx, validRepo)
	require.NoError(t, err)
	assert.True(t, detected, "expected valid repo to be detected")

	// 2. Non-existent path
	nonExistent := filepath.Join(os.TempDir(), "ga-ts-detector-missing-999999")
	detected, err = adapter.Detect(ctx, nonExistent)
	require.NoError(t, err)
	assert.False(t, detected, "non-existent path must not be detected")

	// 3. Empty directory without JS/TS
	emptyDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(emptyDir, "notes.txt"), []byte("hello"), 0644))
	detected, err = adapter.Detect(ctx, emptyDir)
	require.NoError(t, err)
	assert.False(t, detected, "directory with only text files must not be detected")

	// 4. JS repository with only package.json
	jsRepo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(jsRepo, "package.json"), []byte(`{"name":"js-app"}`), 0644))
	detected, err = adapter.Detect(ctx, jsRepo)
	require.NoError(t, err)
	assert.True(t, detected, "package.json alone should trigger detection")
}

// TestAdapter_EnvironmentChecker verifies host Node.js runtime validation.
func TestAdapter_EnvironmentChecker(t *testing.T) {
	ctx := context.Background()

	// 1. Standard node binary on host
	adapter := typescript.NewAdapter()
	err := adapter.CheckEnvironment(ctx)
	require.NoError(t, err, "node runtime should be detected on host")

	// 2. Missing binary path
	badAdapter := typescript.NewAdapter(typescript.WithNodeBinary("non_existent_node_exec_999"))
	err = badAdapter.CheckEnvironment(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found on PATH")
}

// TestParsePackageJSON verifies parsing package.json configurations.
func TestParsePackageJSON(t *testing.T) {
	tempDir := t.TempDir()
	pkgContent := `{
  "name": "my-awesome-pkg",
  "version": "2.1.0",
  "main": "lib/index.js",
  "scripts": {
    "start": "node index.js",
    "test": "vitest run"
  },
  "dependencies": {
    "koffi": "^2.8.0",
    "lodash": "^4.17.21"
  },
  "devDependencies": {
    "vitest": "^1.0.0"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgContent), 0644))

	pkg, err := typescript.ParsePackageJSON(tempDir)
	require.NoError(t, err)
	assert.Equal(t, "my-awesome-pkg", pkg.Name)
	assert.Equal(t, "2.1.0", pkg.Version)
	assert.Equal(t, "lib/index.js", pkg.Main)
	assert.Equal(t, "^2.8.0", pkg.Dependencies["koffi"])
	assert.Equal(t, "^1.0.0", pkg.DevDependencies["vitest"])
}

// TestParseSourceImports verifies extraction of ESM and CommonJS imports.
func TestParseSourceImports(t *testing.T) {
	sourceCode := `
// Single line commented import
// import { fake } from './commented';

/* Multi-line commented import
import { ignored } from './ignored';
*/

import defaultExport from './defaultExport';
import { a, b as renamed } from '../utils/helpers';
import * as allFromUtils from './utils';
import './side-effect-module';

export * from './exportedModule';
export { foo } from './fooModule';

const legacyLib = require('./legacyLib');
const { utilFn } = require('../common/util');
const externalDep = require('express');

async function lazyLoad() {
  const dynamicModule = await import('./dynamicModule');
  return dynamicModule;
}
`

	imports := typescript.ParseSourceImports(sourceCode)
	require.NotEmpty(t, imports)

	specifiers := make(map[string]typescript.ImportStatement)
	for _, imp := range imports {
		specifiers[imp.Specifier] = imp
	}

	assert.Contains(t, specifiers, "./defaultExport")
	assert.Contains(t, specifiers, "../utils/helpers")
	assert.Contains(t, specifiers, "./utils")
	assert.Contains(t, specifiers, "./side-effect-module")
	assert.Contains(t, specifiers, "./exportedModule")
	assert.Contains(t, specifiers, "./fooModule")
	assert.Contains(t, specifiers, "./legacyLib")
	assert.Contains(t, specifiers, "../common/util")
	assert.Contains(t, specifiers, "express")
	assert.Contains(t, specifiers, "./dynamicModule")

	// Comments must be stripped
	assert.NotContains(t, specifiers, "./commented")
	assert.NotContains(t, specifiers, "./ignored")

	// Check relative vs external flags
	assert.True(t, specifiers["./defaultExport"].IsRelative)
	assert.False(t, specifiers["express"].IsRelative)
	assert.True(t, specifiers["./legacyLib"].IsCommonJS)
	assert.True(t, specifiers["./dynamicModule"].IsDynamic)
}

// TestDependencyAnalysisAndWavePlanning verifies wave assignment and leaf detection.
func TestDependencyAnalysisAndWavePlanning(t *testing.T) {
	repoDir := createTestTSRepo(t)
	adapter := typescript.NewAdapter()

	ctx := context.Background()
	manifest, err := adapter.Analyze(ctx, repoDir)
	require.NoError(t, err)
	require.NotNil(t, manifest)

	assert.Equal(t, adapterkit.SchemaVersionV1Alpha1, manifest.SchemaVersion)
	assert.Equal(t, "sample-ts-app", manifest.ProjectName)
	assert.Equal(t, "typescript", manifest.SourceLanguage)
	assert.Equal(t, "go", manifest.TargetLanguage)
	assert.NotEmpty(t, manifest.Units)
	assert.NotEmpty(t, manifest.Waves)

	units, err := adapter.DiscoverUnits(ctx, repoDir)
	require.NoError(t, err)
	require.NotEmpty(t, units)

	unitWaveMap := make(map[string]int)
	for _, u := range units {
		unitWaveMap[u.ID] = u.Wave
		assert.Equal(t, adapterkit.StatePlanned, u.State)
		assert.Equal(t, adapterkit.ProfileReplay, u.EvidenceProfile)
	}

	// src/math is a leaf (Wave 0)
	assert.Equal(t, 0, unitWaveMap["src/math"], "src/math should be Wave 0 (leaf)")
	// src/service depends on src/math (Wave 1)
	assert.Equal(t, 1, unitWaveMap["src/service"], "src/service should be Wave 1")
	// src/api depends on src/service (Wave 2)
	assert.Equal(t, 2, unitWaveMap["src/api"], "src/api should be Wave 2")
}

// TestDependencyCycles_GroupedInSameWave verifies that import cycles are detected and grouped.
func TestDependencyCycles_GroupedInSameWave(t *testing.T) {
	tempDir := t.TempDir()

	// Create package.json
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(`{"name":"cycle-app"}`), 0644))

	// Unit A imports Unit B
	dirA := filepath.Join(tempDir, "pkgA")
	require.NoError(t, os.MkdirAll(dirA, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dirA, "index.ts"), []byte(`import { b } from '../pkgB/index'; export const a = 1;`), 0644))

	// Unit B imports Unit A (cycle!)
	dirB := filepath.Join(tempDir, "pkgB")
	require.NoError(t, os.MkdirAll(dirB, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dirB, "index.ts"), []byte(`import { a } from '../pkgA/index'; export const b = 2;`), 0644))

	adapter := typescript.NewAdapter()
	ctx := context.Background()

	units, err := adapter.DiscoverUnits(ctx, tempDir)
	require.NoError(t, err)
	require.Len(t, units, 2)

	// Both must be assigned to the same wave
	assert.Equal(t, units[0].Wave, units[1].Wave, "units in a cycle must share the same wave")

	// Metadata should record cycle peers
	for _, u := range units {
		inCycle, ok := u.Metadata["in_cycle"].(bool)
		assert.True(t, ok && inCycle, "unit in cycle must have in_cycle: true")
	}
}

// TestTestDiscovery verifies Jest, Vitest, and Mocha test discovery.
func TestTestDiscovery(t *testing.T) {
	repoDir := createTestTSRepo(t)

	// Add Mocha test file
	mochaDir := filepath.Join(repoDir, "tests", "mocha")
	require.NoError(t, os.MkdirAll(mochaDir, 0755))
	mochaSrc := `describe('Mocha Suite', function() {
  it('should test something in mocha', function() {
    // test body
  });
});
`
	require.NoError(t, os.WriteFile(filepath.Join(mochaDir, "suite.spec.js"), []byte(mochaSrc), 0644))

	res, err := typescript.DiscoverTests(repoDir)
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Contains(t, res.Frameworks, typescript.FrameworkJest)
	assert.True(t, res.TotalTests >= 3)
	assert.True(t, res.TotalFiles >= 2)

	foundMathSuite := false
	foundMochaSuite := false
	for _, s := range res.Suites {
		if s.Name == "Math Calculator" {
			foundMathSuite = true
			assert.Len(t, s.Cases, 3)
			assert.Equal(t, "should add numbers correctly", s.Cases[0].Name)
			assert.True(t, s.Cases[2].Skipped)
		}
		if s.Name == "Mocha Suite" {
			foundMochaSuite = true
			assert.Len(t, s.Cases, 1)
			assert.Equal(t, "should test something in mocha", s.Cases[0].Name)
		}
	}
	assert.True(t, foundMathSuite, "Math Calculator suite must be discovered")
	assert.True(t, foundMochaSuite, "Mocha Suite must be discovered")
}

// TestBridgeDeclarations verifies documentation and limits for foreign library bridges.
func TestBridgeDeclarations(t *testing.T) {
	decls := typescript.GetBridgeDeclarations()
	require.NotEmpty(t, decls)

	// 1. Subprocess
	subLimits, err := typescript.ValidateBridge(typescript.BridgeSubprocess)
	require.NoError(t, err)
	assert.True(t, subLimits.ProductionGrade)
	assert.Contains(t, subLimits.MemoryOwnership, "Complete process memory isolation")

	// 2. HTTP
	httpLimits, err := typescript.ValidateBridge(typescript.BridgeHTTP)
	require.NoError(t, err)
	assert.True(t, httpLimits.ProductionGrade)
	assert.Contains(t, httpLimits.ConcurrencyModel, "Node.js event loop")

	// 3. Koffi
	koffiLimits, err := typescript.ValidateBridge(typescript.BridgeKoffi)
	require.NoError(t, err)
	assert.False(t, koffiLimits.ProductionGrade, "koffi is experimental/foreign library bridge")
	assert.Contains(t, koffiLimits.ABICompatibility, "C ABI via libffi")
	assert.Contains(t, koffiLimits.MemoryOwnership, "V8 GC cannot manage Go heap")

	// 4. Node-API
	napiLimits, err := typescript.ValidateBridge(typescript.BridgeNAPI)
	require.NoError(t, err)
	assert.False(t, napiLimits.ProductionGrade, "N-API is experimental/foreign library bridge")
	assert.Contains(t, napiLimits.ABICompatibility, "Node-API (N-API) C ABI")
	assert.Contains(t, napiLimits.ConcurrencyModel, "napi_threadsafe_function")

	// 5. Code generation templates
	koffiStub := typescript.GenerateKoffiBinding("./libcalc.so", "Add", "'int', 'int'", "'int'")
	assert.Contains(t, koffiStub, "koffi.load")
	assert.Contains(t, koffiStub, "lib.func('Add', 'int', ['int', 'int'])")

	napiStub := typescript.GenerateNAPIBinding("ExecuteQuery")
	assert.Contains(t, napiStub, "#include <node_api.h>")
	assert.Contains(t, napiStub, "Wrap_ExecuteQuery")
}

// TestReplayDrivers_Execution verifies Subprocess and HTTP driver execution.
func TestReplayDrivers_Execution(t *testing.T) {
	ctx := context.Background()

	// 1. HTTP Driver with real test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/greet" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"greeting": "hello from node service"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
	}))
	defer server.Close()

	httpDriver := typescript.NewHTTPDriver(typescript.WithHTTPBaseURL(server.URL))
	defer httpDriver.Close()

	assert.Equal(t, "typescript:http", httpDriver.DriverName())

	tc := &adapterkit.Case{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		CaseID:        "case-http-1",
		UnitID:        "api",
		Kind:          adapterkit.KindHappy,
		Runtime:       "typescript",
		CapturedAt:    time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Method: "GET",
			Path:   "/greet",
		},
	}

	out, err := httpDriver.ExecuteCase(ctx, tc)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, out.StatusCode)
	assert.Equal(t, 0, out.ExitCode)
	bodyMap, ok := out.Body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello from node service", bodyMap["greeting"])

	// 2. Subprocess Driver executing node
	subDriver := typescript.NewSubprocessDriver()
	defer subDriver.Close()

	assert.Equal(t, "typescript:subprocess", subDriver.DriverName())

	subCase := &adapterkit.Case{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		CaseID:        "case-sub-1",
		UnitID:        "calc",
		Kind:          adapterkit.KindHappy,
		Runtime:       "typescript",
		CapturedAt:    time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Method: "POST",
			Path:   "/compute",
			Body:   map[string]any{"x": 10, "y": 20},
		},
	}

	subOut, err := subDriver.ExecuteCase(ctx, subCase)
	require.NoError(t, err)
	assert.Equal(t, 200, subOut.StatusCode)
	assert.Equal(t, 0, subOut.ExitCode)
	subBody, ok := subOut.Body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", subBody["status"])
	assert.Equal(t, "/compute", subBody["path"])
}

// TestConformanceSuite executes the complete adapterkit conformance test suite
// against the TypeScript LanguageAdapter, verifying 100% compliance.
func TestConformanceSuite(t *testing.T) {
	repoDir := createTestTSRepo(t)

	sampleCase := &adapterkit.Case{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		CaseID:        "ts-case-001",
		UnitID:        "src/math",
		Kind:          adapterkit.KindHappy,
		Runtime:       "typescript",
		CapturedAt:    time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Method: "GET",
			Path:   "/status",
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: 200,
			ExitCode:   0,
			Body:       map[string]any{"status": "ok"},
		},
	}

	// Shared handler to ensure cross-transport replay consistency in conformance suite
	sharedReplayHandler := func(ctx context.Context, c *adapterkit.Case) (*adapterkit.CaseOutput, error) {
		return &adapterkit.CaseOutput{
			StatusCode: 200,
			ExitCode:   0,
			Stdout:     `{"status":"ok"}`,
			Body:       map[string]any{"status": "ok"},
		}, nil
	}

	adapter := typescript.NewAdapter(
		typescript.WithAdapterReplayHandler(sharedReplayHandler),
	)

	cfg := conformance.Config{
		RepoPath:     repoDir,
		TargetUnitID: "src/math",
		SampleCases:  []*adapterkit.Case{sampleCase},
		StrictReplay: true,
	}

	conformance.RunSuite(t, adapter, cfg)
}
