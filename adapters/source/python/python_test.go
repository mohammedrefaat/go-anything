package python_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/adapters/source/python"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit/conformance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestPythonRepo builds a temporary Python repository with dependency waves.
func createTestPythonRepo(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()

	pyproject := `[project]
name = "sample-service"
version = "0.1.0"
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyproject), 0644))

	// Unit 1: Leaf module (utils.py) - no internal dependencies (Wave 0)
	utilsDir := filepath.Join(tempDir, "utils")
	require.NoError(t, os.MkdirAll(utilsDir, 0755))
	utilsPy := `"""Utility functions."""
import math
import os

def calculate_checksum(data: str) -> str:
    """Calculate checksum."""
    return f"chk_{hash(data)}"

def add(a: int, b: int) -> int:
    return a + b
`
	require.NoError(t, os.WriteFile(filepath.Join(utilsDir, "helpers.py"), []byte(utilsPy), 0644))

	// Unit 2: Core models (models/user.py) - no internal dependencies (Wave 0)
	modelsDir := filepath.Join(tempDir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0755))
	modelsPy := `import uuid
from dataclasses import dataclass

@dataclass
class User:
    id: str
    username: str

def create_user(username: str) -> User:
    return User(id=str(uuid.uuid4()), username=username)
`
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, "user.py"), []byte(modelsPy), 0644))

	// Unit 3: Service module (service/billing.py) - depends on utils and models (Wave 1)
	serviceDir := filepath.Join(tempDir, "service")
	require.NoError(t, os.MkdirAll(serviceDir, 0755))
	billingPy := `import json
from models.user import User
from utils.helpers import calculate_checksum

class BillingService:
    def __init__(self, user: User):
        self.user = user

    def process(self, amount: float) -> dict:
        chk = calculate_checksum(self.user.username)
        return {"status": "paid", "amount": amount, "checksum": chk}
`
	require.NoError(t, os.WriteFile(filepath.Join(serviceDir, "billing.py"), []byte(billingPy), 0644))

	// Tests: tests/test_billing.py (pytest + unittest)
	testsDir := filepath.Join(tempDir, "tests")
	require.NoError(t, os.MkdirAll(testsDir, 0755))
	testPy := `import unittest
from service.billing import BillingService
from models.user import User

class TestBillingService(unittest.TestCase):
    def setUp(self):
        self.user = User(id="u1", username="alice")
        self.service = BillingService(self.user)

    def test_process_payment(self):
        res = self.service.process(100.0)
        self.assertEqual(res["status"], "paid")

def test_standalone_checksum():
    assert True
`
	require.NoError(t, os.WriteFile(filepath.Join(testsDir, "test_billing.py"), []byte(testPy), 0644))

	return tempDir
}

// TestPythonAdapter_PassesConformanceSuite runs the official adapterkit conformance suite.
func TestPythonAdapter_PassesConformanceSuite(t *testing.T) {
	repoDir := createTestPythonRepo(t)
	adapter := python.NewAdapter()

	sampleCase := &adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         "case-py-0001",
		UnitID:         "utils/helpers",
		Kind:           adapterkit.KindHappy,
		Runtime:        "python",
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input:          adapterkit.CaseInput{Method: "POST", Path: "/checksum", Body: map[string]any{"data": "hello"}},
		ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200, Body: map[string]any{"status": "ok"}},
	}

	cfg := conformance.Config{
		RepoPath:     repoDir,
		TargetUnitID: "utils/helpers",
		SampleCases:  []*adapterkit.Case{sampleCase},
		StrictReplay: true,
	}

	conformance.RunSuite(t, adapter, cfg)
}

func TestASTParser_ImportsAndStructure(t *testing.T) {
	tempDir := t.TempDir()
	src := `"""Module docstring."""
import sys, os as operating_system
from math import sqrt, pi as PI
from . import local_helper
from ..parent_module import ParentClass
from typing import (
    List,
    Dict,
    Optional,
)

class DataProcessor(BaseClass):
    def __init__(self):
        pass

    def process(self, items):
        return [i * 2 for i in items]

def compute_total(a: int, b: int) -> int:
    return a + b
`
	filePath := filepath.Join(tempDir, "processor.py")
	require.NoError(t, os.WriteFile(filePath, []byte(src), 0644))

	parser := python.NewASTParser()
	info, err := parser.ParseFile(filePath)
	require.NoError(t, err)

	assert.Equal(t, filePath, info.FilePath)
	assert.Contains(t, info.Classes, "DataProcessor")
	assert.Contains(t, info.Functions, "compute_total")

	modulesImported := make(map[string]bool)
	symbolsImported := make(map[string]bool)
	for _, imp := range info.Imports {
		if imp.Module != "" {
			modulesImported[imp.Module] = true
		}
		for _, s := range imp.Symbols {
			symbolsImported[s] = true
		}
	}

	assert.True(t, modulesImported["sys"])
	assert.True(t, modulesImported["os"])
	assert.True(t, modulesImported["math"])
	assert.True(t, symbolsImported["local_helper"])
	assert.True(t, modulesImported["parent_module"])
	assert.True(t, modulesImported["typing"])
}

func TestDependencyDiscovery_TopologicalWaves(t *testing.T) {
	repoDir := createTestPythonRepo(t)
	discoverer := python.NewDependencyDiscoverer()

	ctx := context.Background()
	project, err := discoverer.Discover(ctx, repoDir)
	require.NoError(t, err)
	require.NotNil(t, project)

	assert.Equal(t, "sample-service", project.ProjectName)
	assert.NotEmpty(t, project.Units)

	// Leaf modules (utils/helpers and models/user) must be in Wave 0
	unitWaveMap := make(map[string]int)
	for _, u := range project.Units {
		unitWaveMap[u.ID] = u.Wave
	}

	assert.Equal(t, 0, unitWaveMap["utils/helpers"], "utils/helpers should be Wave 0 leaf")
	assert.Equal(t, 0, unitWaveMap["models/user"], "models/user should be Wave 0 leaf")

	// Dependent module (service/billing) must be in Wave 1 or higher
	assert.GreaterOrEqual(t, unitWaveMap["service/billing"], 1, "service/billing should be in Wave 1+")
}

func TestTestDiscovery_PytestAndUnittest(t *testing.T) {
	repoDir := createTestPythonRepo(t)
	td := python.NewTestDiscoverer()

	ctx := context.Background()
	res, err := td.DiscoverTests(ctx, repoDir, []string{"service/billing", "utils/helpers", "models/user"})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.GreaterOrEqual(t, res.TotalCases, 2)

	var caseNames []string
	for _, s := range res.Suites {
		for _, c := range s.Cases {
			caseNames = append(caseNames, c.Name)
		}
	}

	assert.Contains(t, caseNames, "test_process_payment")
	assert.Contains(t, caseNames, "test_standalone_checksum")
}

func TestHotPathExtractor_ParseReport(t *testing.T) {
	sampleCProfile := `         194 function calls (180 primitive calls) in 0.045 seconds

   Ordered by: cumulative time

   ncalls  tottime  percall  cumtime  percall filename:lineno(function)
        1    0.001    0.001    0.045    0.045 /app/main.py:1(<module>)
    10/5     0.010    0.001    0.030    0.006 /app/service/billing.py:12(process)
      100    0.005    0.000    0.010    0.000 /app/utils/helpers.py:5(calculate_checksum)
        1    0.000    0.000    0.000    0.000 {built-in method math.sqrt}
`

	extractor := python.NewHotPathExtractor()
	report, err := extractor.ParseReport(sampleCProfile, []string{"service/billing", "utils/helpers"})
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, int64(194), report.TotalFunctionCalls)
	assert.Equal(t, 0.045, report.TotalDurationSec)
	assert.NotEmpty(t, report.Entries)
	assert.NotEmpty(t, report.TopBottlenecks)

	top := report.TopBottlenecks[0]
	assert.Equal(t, "<module>", top.Function)
	assert.Equal(t, 0.045, top.CumulativeTime)
	assert.Equal(t, 1.0, top.HotnessScore)

	second := report.TopBottlenecks[1]
	assert.Equal(t, "process", second.Function)
	assert.Equal(t, int64(10), second.Calls)
	assert.Equal(t, int64(5), second.PrimitiveCalls)
	assert.Equal(t, "service/billing", second.UnitID)
}

func TestBridgeLimitsAndErrorMapping(t *testing.T) {
	limits := python.DefaultBridgeLimits()
	assert.NotEmpty(t, limits.ABI.SupportedRuntimes)
	assert.False(t, limits.MemoryOwnership.SharedMemoryAllowed)
	assert.True(t, limits.Concurrency.SubprocessParallelism)
	assert.NotEmpty(t, limits.ErrorMappings)

	// Test Error Mapping
	status, exitCode, err := python.MapPythonError("ValueError", "invalid numeric format")
	require.Error(t, err)
	assert.Equal(t, 400, status)
	assert.Equal(t, 2, exitCode)
	assert.Contains(t, err.Error(), "ValueError: invalid numeric format")

	status, exitCode, err = python.MapPythonError("FileNotFoundError", "missing config.json")
	require.Error(t, err)
	assert.Equal(t, 404, status)
	assert.Equal(t, 2, exitCode)

	status, exitCode, err = python.MapPythonError("TimeoutError", "socket deadline exceeded")
	require.Error(t, err)
	assert.Equal(t, 504, status)
	assert.Equal(t, 124, exitCode)
}

func TestReplayDrivers_SubprocessAndHTTP_Parity(t *testing.T) {
	ctx := context.Background()
	unitID := "test-unit"

	subDriver := python.NewSubprocessReplayDriver(unitID)
	defer subDriver.Close()

	httpDriver := python.NewHTTPReplayDriver(unitID)
	defer httpDriver.Close()

	testCase := &adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         "case-parity-01",
		UnitID:         unitID,
		Kind:           adapterkit.KindHappy,
		Runtime:        "python",
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input:          adapterkit.CaseInput{Method: "GET", Path: "/api/health"},
		ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200, Body: map[string]any{"status": "ok"}},
	}

	subOut, err := subDriver.ExecuteCase(ctx, testCase)
	require.NoError(t, err)

	httpOut, err := httpDriver.ExecuteCase(ctx, testCase)
	require.NoError(t, err)

	assert.Equal(t, subOut.StatusCode, httpOut.StatusCode)
	assert.Equal(t, subOut.ExitCode, httpOut.ExitCode)
	assert.Equal(t, subOut.Body, httpOut.Body)
	assert.Equal(t, subOut.Stdout, httpOut.Stdout)
	assert.Equal(t, subOut.Stderr, httpOut.Stderr)
}

func TestDetectorAndEnvironmentChecker(t *testing.T) {
	ctx := context.Background()
	adapter := python.NewAdapter()

	// 1. Detection on valid repo
	repoDir := createTestPythonRepo(t)
	detected, err := adapter.Detect(ctx, repoDir)
	require.NoError(t, err)
	assert.True(t, detected)

	// 2. Detection on empty temp dir (no python files)
	emptyDir := t.TempDir()
	detected, err = adapter.Detect(ctx, emptyDir)
	require.NoError(t, err)
	assert.False(t, detected)

	// 3. Detection on non-existent path
	detected, err = adapter.Detect(ctx, filepath.Join(emptyDir, "non_existent_folder_xyz"))
	require.NoError(t, err)
	assert.False(t, detected)

	// 4. Environment Check
	err = adapter.CheckEnvironment(ctx)
	assert.NoError(t, err, "python environment check should succeed when python is installed")
}
