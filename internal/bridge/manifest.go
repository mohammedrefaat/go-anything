package bridge

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Manifest represents the function contract defined in bridge.yaml (ADR-005).
type Manifest struct {
	Version   string          `json:"version" yaml:"version"`
	Unit      string          `json:"unit" yaml:"unit"`
	Service   string          `json:"service" yaml:"service"`
	Transport TransportConfig `json:"transport,omitempty" yaml:"transport,omitempty"`
	Functions []Function      `json:"functions" yaml:"functions"`
}

// TransportConfig defines transport options for the sidecar bridge.
type TransportConfig struct {
	Default    string `json:"default" yaml:"default"`
	SocketPath string `json:"socket_path,omitempty" yaml:"socket_path,omitempty"`
	PipeName   string `json:"pipe_name,omitempty" yaml:"pipe_name,omitempty"`
}

// Function defines a single callable bridge procedure.
type Function struct {
	Name        string        `json:"name" yaml:"name"`
	Description string        `json:"description,omitempty" yaml:"description,omitempty"`
	Idempotent  bool          `json:"idempotent,omitempty" yaml:"idempotent,omitempty"`
	TimeoutMS   int           `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	Params      []Param       `json:"params,omitempty" yaml:"params,omitempty"`
	Returns     ReturnsConfig `json:"returns,omitempty" yaml:"returns,omitempty"`
	Errors      []ErrorConfig `json:"errors,omitempty" yaml:"errors,omitempty"`
}

// Param defines an input argument for a bridge function.
type Param struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// ReturnsConfig defines the output return type.
type ReturnsConfig struct {
	Type        string            `json:"type" yaml:"type"`
	Properties  map[string]string `json:"properties,omitempty" yaml:"properties,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
}

// ErrorConfig defines an application domain error (-32000 to -32099).
type ErrorConfig struct {
	Code        int    `json:"code" yaml:"code"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Validate verifies that the manifest contains required fields and valid definitions.
func (m *Manifest) Validate() error {
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("manifest version is required")
	}
	if strings.TrimSpace(m.Unit) == "" {
		return fmt.Errorf("manifest unit is required")
	}
	if strings.TrimSpace(m.Service) == "" {
		// Default to sanitized unit name if empty
		parts := strings.Split(m.Unit, "/")
		m.Service = parts[len(parts)-1] + "-bridge"
	}
	if len(m.Functions) == 0 {
		return fmt.Errorf("manifest must declare at least one function")
	}

	for i, fn := range m.Functions {
		if strings.TrimSpace(fn.Name) == "" {
			return fmt.Errorf("function[%d] must have a name", i)
		}
		for j, p := range fn.Params {
			if strings.TrimSpace(p.Name) == "" {
				return fmt.Errorf("function %q param[%d] must have a name", fn.Name, j)
			}
			if strings.TrimSpace(p.Type) == "" {
				return fmt.Errorf("function %q param %q must have a type", fn.Name, p.Name)
			}
		}
		if strings.TrimSpace(fn.Returns.Type) == "" {
			return fmt.Errorf("function %q must have a returns type", fn.Name)
		}
	}

	return nil
}

// LoadManifest reads and parses a bridge.yaml manifest from disk.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read bridge manifest at %q: %w", path, err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse bridge manifest at %q: %w", path, err)
	}

	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid bridge manifest at %q: %w", path, err)
	}

	return &m, nil
}

// SaveManifest serializes and writes a bridge.yaml manifest to disk.
func SaveManifest(path string, m *Manifest) error {
	if err := m.Validate(); err != nil {
		return fmt.Errorf("invalid bridge manifest: %w", err)
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to serialize bridge manifest: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write bridge manifest to %q: %w", path, err)
	}

	return nil
}
