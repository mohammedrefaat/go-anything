package shadow

import (
	"os"
	"strings"
)

// Mode represents the active routing mode for shadow execution.
type Mode string

const (
	// ModeOld routes traffic to the legacy implementation only.
	ModeOld Mode = "old"

	// ModeShadow routes traffic to legacy implementation synchronously,
	// while asynchronously evaluating the new Go implementation in the background.
	ModeShadow Mode = "shadow"

	// ModeNew routes traffic to the new Go implementation only.
	ModeNew Mode = "new"
)

// ParseMode parses a mode string into a Mode constant. Defaults to ModeOld.
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "new":
		return ModeNew
	case "shadow":
		return ModeShadow
	default:
		return ModeOld
	}
}

// NormalizeUnitID normalizes a unit identifier for environment variable naming.
// e.g., "auth" -> "AUTH", "user-service" -> "USER_SERVICE", "pkg/api.v1" -> "PKG_API_V1"
func NormalizeUnitID(unitID string) string {
	normalized := strings.ToUpper(unitID)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, "/", "_")
	normalized = strings.ReplaceAll(normalized, ".", "_")
	return normalized
}

// GetGlobalMode reads the global routing mode from GO_ANYTHING_MODE. Defaults to ModeOld.
func GetGlobalMode() Mode {
	env := os.Getenv("GO_ANYTHING_MODE")
	if env == "" {
		return ModeOld
	}
	return ParseMode(env)
}

// GetUnitMode reads the mode for a specific unit, checking unit-specific env vars first:
// 1. GO_ANYTHING_MODE_<unitID>
// 2. GO_ANYTHING_UNIT_<unitID>
// 3. Fallback to GO_ANYTHING_MODE (GetGlobalMode())
func GetUnitMode(unitID string) Mode {
	if unitID != "" {
		normalized := NormalizeUnitID(unitID)
		if env := os.Getenv("GO_ANYTHING_MODE_" + normalized); env != "" {
			return ParseMode(env)
		}
		if env := os.Getenv("GO_ANYTHING_UNIT_" + normalized); env != "" {
			return ParseMode(env)
		}
	}
	return GetGlobalMode()
}
