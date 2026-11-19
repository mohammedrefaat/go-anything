package facade

import (
	"os"
	"strings"
)

// Mode represents the active routing mode for a facade.
type Mode string

const (
	// ModeOld routes traffic to the legacy implementation.
	ModeOld Mode = "old"

	// ModeShadow routes traffic to legacy implementation synchronously,
	// while asynchronously evaluating the new implementation in the background.
	ModeShadow Mode = "shadow"

	// ModeNew routes traffic to the new Go implementation.
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

// GetGlobalMode reads the global mode from GO_ANYTHING_MODE.
func GetGlobalMode() Mode {
	env := os.Getenv("GO_ANYTHING_MODE")
	if env == "" {
		return ModeOld
	}
	return ParseMode(env)
}

// GetUnitMode reads the mode for a specific unit, checking unit-specific env var first.
// E.g. GO_ANYTHING_UNIT_AUTH=new overrides GO_ANYTHING_MODE=old.
func GetUnitMode(unitID string) Mode {
	normalized := strings.ToUpper(unitID)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, "/", "_")
	normalized = strings.ReplaceAll(normalized, ".", "_")

	if env := os.Getenv("GO_ANYTHING_UNIT_" + normalized); env != "" {
		return ParseMode(env)
	}
	return GetGlobalMode()
}
