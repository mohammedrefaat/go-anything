// Package schemas contains JSON Schemas and versioned data contract definitions
// for go-anything workspaces.
package schemas

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

var (
	schemaCacheMu sync.RWMutex
	schemaCache   = make(map[string]*jsonschema.Schema)
)

// DefaultSchemaVersion specifies the active schema version for workspace files.
const DefaultSchemaVersion = "v1"

// VersionV1Alpha1 is the legacy v1alpha1 schema version.
const VersionV1Alpha1 = "v1alpha1"

// VersionV1 is the stable v1 schema version.
const VersionV1 = "v1"

// VersionV2 is the v2 schema version for CLI command output contracts.
const VersionV2 = "v2"

//go:embed v1alpha1/*.json v1/*.json v2/*.json
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

// CompileSchema compiles a schema from embedded storage, using an in-memory cache.
func CompileSchema(version, name string) (*jsonschema.Schema, error) {
	if version == "" {
		version = DefaultSchemaVersion
	}
	cacheKey := version + "/" + name

	schemaCacheMu.RLock()
	cached, ok := schemaCache[cacheKey]
	schemaCacheMu.RUnlock()
	if ok {
		return cached, nil
	}

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

	schemaCacheMu.Lock()
	schemaCache[cacheKey] = schema
	schemaCacheMu.Unlock()

	return schema, nil
}

// ValidateJSON validates arbitrary JSON data against the named schema in the given version.
func ValidateJSON(version, name string, rawJSON []byte) error {
	schema, err := CompileSchema(version, name)
	if err != nil {
		return err
	}
	var val any
	dec := json.NewDecoder(bytes.NewReader(rawJSON))
	dec.UseNumber()
	if err := dec.Decode(&val); err != nil {
		return fmt.Errorf("invalid json payload: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid json payload: unexpected data after the first value")
	}
	return schema.Validate(val)
}

// ValidateV2 validates JSON data against a schema in the v2/ directory.
// schemaFileName can be provided with or without ".schema.json" or ".json" suffix.
func ValidateV2(schemaFileName string, data []byte) error {
	target := schemaFileName
	if _, err := GetSchema(VersionV2, target); err != nil {
		if _, err2 := GetSchema(VersionV2, schemaFileName+".schema.json"); err2 == nil {
			target = schemaFileName + ".schema.json"
		} else if _, err3 := GetSchema(VersionV2, schemaFileName+".json"); err3 == nil {
			target = schemaFileName + ".json"
		}
	}
	return ValidateJSON(VersionV2, target, data)
}
