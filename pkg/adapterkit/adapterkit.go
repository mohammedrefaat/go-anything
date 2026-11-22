package adapterkit

// Version is the current frozen and stable version of the adapterkit specification and its schema contracts.
const Version = "v1"

// SchemaVersionV1 is the constant tag expected in the schema_version field of all v1 records.
const SchemaVersionV1 = "v1"

// SchemaVersionV1Alpha1 is the constant tag expected in the schema_version field of all legacy v1alpha1 records.
const SchemaVersionV1Alpha1 = "v1alpha1"

// DefaultSchemaVersion specifies the active schema version for new workspace files.
const DefaultSchemaVersion = SchemaVersionV1

// Stable schema $id references for v1 and v1alpha1 JSON schemas.
const (
	SchemaIDManifestV1 = "https://schemas.go-anything.dev/v1/manifest.schema.json"
	SchemaIDUnitsV1    = "https://schemas.go-anything.dev/v1/units.schema.json"
	SchemaIDRulesV1    = "https://schemas.go-anything.dev/v1/rules.schema.json"
	SchemaIDCasesV1    = "https://schemas.go-anything.dev/v1/cases.schema.json"
	SchemaIDReportsV1  = "https://schemas.go-anything.dev/v1/reports.schema.json"
	SchemaIDEvidenceV1 = "https://schemas.go-anything.dev/v1/evidence.schema.json"

	SchemaIDManifestV1Alpha1 = "https://schemas.go-anything.dev/v1alpha1/manifest.schema.json"
	SchemaIDUnitsV1Alpha1    = "https://schemas.go-anything.dev/v1alpha1/units.schema.json"
	SchemaIDRulesV1Alpha1    = "https://schemas.go-anything.dev/v1alpha1/rules.schema.json"
	SchemaIDCasesV1Alpha1    = "https://schemas.go-anything.dev/v1alpha1/cases.schema.json"
	SchemaIDReportsV1Alpha1  = "https://schemas.go-anything.dev/v1alpha1/reports.schema.json"
	SchemaIDEvidenceV1Alpha1 = "https://schemas.go-anything.dev/v1alpha1/evidence.schema.json"
)
