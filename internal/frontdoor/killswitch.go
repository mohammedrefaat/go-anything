package frontdoor

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvDisableAll is the global kill switch environment variable.
	EnvDisableAll = "GO_ANYTHING_DISABLE_ALL"

	// EnvUnitPrefix is the per-unit kill switch environment variable prefix.
	EnvUnitPrefix = "GO_ANYTHING_UNIT_"

	// OverrideOriginal is the token indicating traffic should drop back to original.
	OverrideOriginal = "original"
)

// NormalizeUnitEnvKey converts a unit ID to an environment variable suffix.
// E.g. "unit-auth.service/v1" -> "UNIT_AUTH_SERVICE_V1".
func NormalizeUnitEnvKey(unitID string) string {
	s := strings.ToUpper(unitID)
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

// IsGlobalKillSwitchActive returns true if GO_ANYTHING_DISABLE_ALL is set to true, 1, or original.
func IsGlobalKillSwitchActive() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvDisableAll)))
	return v == "true" || v == "1" || v == OverrideOriginal
}

// IsUnitKillSwitchActive checks whether the kill switch is active for a specific unit.
// Checks:
// 1. Global disable (GO_ANYTHING_DISABLE_ALL=true)
// 2. Unit environment variable (GO_ANYTHING_UNIT_<id>=original or GO_ANYTHING_UNIT_<NORMALIZED>=original)
// 3. Local lockfile in .go-anything/local/<unit>.kill or .go-anything/local/<unit>.override
func IsUnitKillSwitchActive(workspaceRoot, unitID string) bool {
	if unitID == "" {
		return false
	}

	// 1. Global kill switch
	if IsGlobalKillSwitchActive() {
		return true
	}

	// 2. Direct unit ID environment variable
	vDirect := strings.ToLower(strings.TrimSpace(os.Getenv(EnvUnitPrefix + unitID)))
	if vDirect == OverrideOriginal || vDirect == "true" || vDirect == "1" {
		return true
	}

	// Normalized unit ID environment variable
	vNorm := strings.ToLower(strings.TrimSpace(os.Getenv(EnvUnitPrefix + NormalizeUnitEnvKey(unitID))))
	if vNorm == OverrideOriginal || vNorm == "true" || vNorm == "1" {
		return true
	}

	// 3. Local lockfile check in .go-anything/local/
	if workspaceRoot != "" {
		localDir := filepath.Join(workspaceRoot, ".go-anything", "local")
		killFile := filepath.Join(localDir, unitID+".kill")
		if _, err := os.Stat(killFile); err == nil {
			return true
		}
		overrideFile := filepath.Join(localDir, unitID+".override")
		if _, err := os.Stat(overrideFile); err == nil {
			return true
		}
	}

	return false
}
