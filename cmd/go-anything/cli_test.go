package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_VersionAndHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// Test version
	err := runCLI([]string{"version"}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "go-anything")

	// Test help
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"--help"}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Usage:")
	assert.Contains(t, stdout.String(), "init")
	assert.Contains(t, stdout.String(), "status")
	assert.Contains(t, stdout.String(), "doctor")
}

func TestCLI_GlobalFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// Custom jobs and profile
	err := runCLI([]string{"--jobs=8", "--profile=full", "version"}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "jobs: 8")
	assert.Contains(t, stdout.String(), "profile: full")

	// Shorthand flags
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"-j", "4", "-p", "full", "version"}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "jobs: 4")
	assert.Contains(t, stdout.String(), "profile: full")
}

func TestCLI_InitCreatesLayout(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-cli-init-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"init", "--source=python", "--name=billing-service", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Initialized empty go-anything workspace")
	assert.Contains(t, stdout.String(), "billing-service")
	assert.Contains(t, stdout.String(), "python")

	// Verify .go-anything/ directory structure
	gaDir := filepath.Join(tempDir, ".go-anything")
	assert.DirExists(t, gaDir)
	assert.DirExists(t, filepath.Join(gaDir, "units"))
	assert.DirExists(t, filepath.Join(gaDir, "local"))
	assert.DirExists(t, filepath.Join(gaDir, "cases"))
	assert.DirExists(t, filepath.Join(gaDir, "reports"))
	assert.DirExists(t, filepath.Join(gaDir, "evidence"))
	assert.FileExists(t, filepath.Join(gaDir, "manifest.json"))
	assert.FileExists(t, filepath.Join(gaDir, "rules.json"))

	// Verify .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	assert.FileExists(t, gitignorePath)
	giData, err := os.ReadFile(gitignorePath)
	require.NoError(t, err)
	assert.Contains(t, string(giData), ".go-anything/local/")
}

func TestCLI_ModernizeAlias(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-cli-mod-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"modernize", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Initialized empty go-anything workspace")
	assert.Contains(t, stdout.String(), "Source language: go")
}

func TestCLI_Status(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-cli-status-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer

	// Uninitialized status fails
	err = runCLI([]string{"status", tempDir}, &stdout, &stderr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no .go-anything workspace found")

	// Initialize
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"init", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	// Status on empty workspace
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"status", tempDir}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "No migration units registered yet")

	// Add units and an active lease
	store := state.NewStore(tempDir)
	unit1 := &adapterkit.Unit{
		ID:              "auth",
		Name:            "Auth",
		Wave:            0,
		State:           adapterkit.StateInProgress,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	unit2 := &adapterkit.Unit{
		ID:              "cart",
		Name:            "Shopping Cart",
		Wave:            1,
		State:           adapterkit.StateReady,
		EvidenceProfile: adapterkit.ProfileReplay,
	}
	require.NoError(t, store.SaveUnit(unit1))
	require.NoError(t, store.SaveUnit(unit2))

	lease, err := store.AcquireLease("auth", "agent-worker-1", 10*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, lease)

	// Status with registered units
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"status", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "auth")
	assert.Contains(t, output, "cart")
	assert.Contains(t, output, "In progress")
	assert.Contains(t, output, "Ready")
	assert.Contains(t, output, "agent-worker-1")
	assert.Contains(t, output, "Summary:")
}

func TestCLI_InitFollowedByDoctor(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-cli-doc-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer

	// 1. Run init
	err = runCLI([]string{"init", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	// 2. Run doctor in the freshly initialized directory
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"doctor", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	docOutput := stdout.String()
	assert.Contains(t, docOutput, "[PASS] gitignore")
	assert.Contains(t, docOutput, "[PASS] committed_secrets")
	assert.Contains(t, docOutput, "[PASS] unsafe_flags")
	assert.Contains(t, docOutput, "All health checks passed successfully")
}

func TestCLI_DoctorCatchesUnsafeFlag(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-cli-doc-flag-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	var stdout, stderr bytes.Buffer
	err = runCLI([]string{"init", tempDir}, &stdout, &stderr)
	require.NoError(t, err)

	// Passing unsafe flag to doctor triggers failure
	stdout.Reset()
	stderr.Reset()
	err = runCLI([]string{"doctor", "--unsafe-allow-unredacted", tempDir}, &stdout, &stderr)
	assert.Error(t, err)
	assert.Contains(t, stderr.String(), "[FAIL] unsafe_flags")
}
