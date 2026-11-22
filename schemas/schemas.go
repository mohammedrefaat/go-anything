// Package schemas contains JSON Schemas and versioned data contract definitions
// for go-anything workspaces.
package schemas

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// DefaultSchemaVersion specifies the active schema version for workspace files.
const DefaultSchemaVersion = "v1"

// VersionV1Alpha1 is the legacy v1alpha1 schema version.
const VersionV1Alpha1 = "v1alpha1"

// VersionV1 is the stable v1 schema version.
const VersionV1 = "v1"

//go:embed v1alpha1/*.json v1/*.json
var FS embed.FS

// GetSchema returns the raw JSON schema bytes for a given version and schema name.
func GetSchema(version, name string) ([]byte, error) {
	if version == "" {
		version = DefaultSchemaVersion
	}
	filePath := path.Join(version, name)
	data, err := fs.ReadFile(FS, filePath)
	if err != nil {
		return nil, fmt.Errorf("schema %s not found in version %s: %w", name, version, err)
	}
	return data, nil
}

// CompileSchema compiles a schema from embedded storage.
func CompileSchema(version, name string) (*jsonschema.Schema, error) {
	data, err := GetSchema(version, name)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	if err := compiler.AddResource(name, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("failed to add schema resource %s: %w", name, err)
	}
	schema, err := compiler.Compile(name)
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema %s: %w", name, err)
	}
	return schema, nil
}

// ValidateJSON validates arbitrary JSON data against the named schema in the given version.
func ValidateJSON(version, name string, rawJSON []byte) error {
	schema, err := CompileSchema(version, name)
	if err != nil {
		return err
	}
	var val any
	if err := json.Unmarshal(rawJSON, &val); err != nil {
		return fmt.Errorf("invalid json payload: %w", err)
	}
	return schema.Validate(val)
}
