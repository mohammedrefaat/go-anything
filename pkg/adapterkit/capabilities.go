package adapterkit

import (
	"errors"
	"fmt"
)

// Capability defines privileges an adapter may request.
// External adapters run as subprocesses and must declare capabilities up front.
// The Core refuses any action an adapter did not declare.
type Capability string

const (
	// CapabilityReadRepo authorizes the adapter to read repository files and directories.
	CapabilityReadRepo Capability = "read_repo"
	// CapabilityRunOriginal authorizes the adapter to execute the original runtime in a restricted sandbox.
	CapabilityRunOriginal Capability = "run_original"
	// CapabilityNetwork authorizes the adapter or runtime to make network calls (disabled by default).
	CapabilityNetwork Capability = "network"
)

// ValidCapabilities lists all supported capability tokens.
var ValidCapabilities = map[Capability]struct{}{
	CapabilityReadRepo:    {},
	CapabilityRunOriginal: {},
	CapabilityNetwork:     {},
}

// HasCapability checks whether a slice of declared capabilities contains the target capability.
func HasCapability(caps []Capability, target Capability) bool {
	for _, c := range caps {
		if c == target {
			return true
		}
	}
	return false
}

// ValidateCapabilities validates that all declared capabilities are known tokens.
func ValidateCapabilities(caps []Capability) error {
	seen := make(map[Capability]struct{}, len(caps))
	for _, c := range caps {
		if _, ok := ValidCapabilities[c]; !ok {
			return fmt.Errorf("unknown capability declared: %q", c)
		}
		if _, ok := seen[c]; ok {
			return fmt.Errorf("duplicate capability declared: %q", c)
		}
		seen[c] = struct{}{}
	}
	return nil
}

// SemanticKind represents field semantic designations.
// Adapters declare what each field means; Core owns how to process it.
type SemanticKind string

const (
	// SemanticSecret marks fields containing sensitive credentials for fail-closed in-memory redaction.
	SemanticSecret SemanticKind = "secret"
	// SemanticVolatile marks dynamic fields (timestamps, UUIDs) that differ across identical runs.
	SemanticVolatile SemanticKind = "volatile"
	// SemanticOrdered marks array fields where element order is semantically significant.
	SemanticOrdered SemanticKind = "ordered"
)

// ValidSemanticKinds lists all supported field semantic kinds.
var ValidSemanticKinds = map[SemanticKind]struct{}{
	SemanticSecret:   {},
	SemanticVolatile: {},
	SemanticOrdered:  {},
}

// FieldSemantic maps a field path to its designated semantic behavior.
type FieldSemantic struct {
	Path        string       `json:"path"`
	Semantic    SemanticKind `json:"semantic"`
	Description string       `json:"description,omitempty"`
}

// ValidateFieldSemantics validates that declared field semantics have non-empty paths and valid kinds.
func ValidateFieldSemantics(semantics []FieldSemantic) error {
	for i, s := range semantics {
		if s.Path == "" {
			return fmt.Errorf("field semantic at index %d has empty path", i)
		}
		if _, ok := ValidSemanticKinds[s.Semantic]; !ok {
			return fmt.Errorf("field semantic at %q has invalid semantic kind %q", s.Path, s.Semantic)
		}
	}
	return nil
}

// ErrUndeclaredCapability is returned when an adapter attempts an action it did not declare.
var ErrUndeclaredCapability = errors.New("action rejected: undeclared capability")
