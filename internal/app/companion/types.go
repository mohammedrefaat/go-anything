package companion

// ToggleInput specifies parameters for enable and disable commands.
type ToggleInput struct {
	UnitID   string `json:"unit_id"`
	Function string `json:"function,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Enable   bool   `json:"enable"`
}

// ToggleOutput reports the resulting routing status after enable/disable.
type ToggleOutput struct {
	SchemaVersion string `json:"schema_version,omitempty"`
	UnitID        string `json:"unit_id"`
	Function      string `json:"function,omitempty"`
	Routing       string `json:"routing"` // "go" or "original"
	Message       string `json:"message"`
	Timestamp     string `json:"timestamp"`
}

// RefreshInput specifies parameters for the refresh command.
type RefreshInput struct {
	UnitID string `json:"unit_id"`
	Dir    string `json:"dir,omitempty"`
}

// RefreshOutput reports the outcome of re-recording and diff comparison.
type RefreshOutput struct {
	SchemaVersion  string `json:"schema_version,omitempty"`
	UnitID         string `json:"unit_id"`
	CasesRefreshed int    `json:"cases_refreshed"`
	DiffStatus     string `json:"diff_status"`
	Message        string `json:"message"`
	Timestamp      string `json:"timestamp"`
}

// CleanInput specifies parameters for the clean command.
type CleanInput struct {
	UnitID string `json:"unit_id"`
	All    bool   `json:"all,omitempty"`
	Force  bool   `json:"force,omitempty"`
	Dir    string `json:"dir,omitempty"`
}

// CleanOutput reports the files removed and preserved by clean.
type CleanOutput struct {
	SchemaVersion  string   `json:"schema_version,omitempty"`
	UnitID         string   `json:"unit_id"`
	FilesRemoved   []string `json:"files_removed"`
	PreservedFiles []string `json:"preserved_files"`
	Message        string   `json:"message"`
	Timestamp      string   `json:"timestamp"`
}

// ReportInput specifies parameters for the report command.
type ReportInput struct {
	UnitID string `json:"unit_id"`
	Dir    string `json:"dir,omitempty"`
	Format string `json:"format,omitempty"` // "markdown" or "json"
}

// BenchmarkSummary holds benchmark metrics for the report.
type BenchmarkSummary struct {
	SpeedupRatio  float64 `json:"speedup_ratio,omitempty"`
	OriginalP50MS float64 `json:"original_p50_ms,omitempty"`
	GoP50MS       float64 `json:"go_p50_ms,omitempty"`
	AllocsPerOp   int     `json:"allocs_per_op,omitempty"`
}

// ReportOutput encapsulates a comprehensive migration status report for a unit.
type ReportOutput struct {
	SchemaVersion   string            `json:"schema_version,omitempty"`
	UnitID          string            `json:"unit_id"`
	State           string            `json:"state"`
	EvidenceProfile string            `json:"evidence_profile"`
	CasesCount      int               `json:"cases_count"`
	ReplayPassRate  float64           `json:"replay_pass_rate"`
	DiffStatus      string            `json:"diff_status"`
	GateStatus      string            `json:"gate_status"`
	Benchmark       *BenchmarkSummary `json:"benchmark,omitempty"`
	Markdown        string            `json:"markdown,omitempty"`
	GeneratedAt     string            `json:"generated_at"`
}

// MapInput specifies parameters for the map command.
type MapInput struct {
	UnitID string `json:"unit_id"`
	Dir    string `json:"dir,omitempty"`
}

// FunctionEntry represents a mapped function in a unit.
type FunctionEntry struct {
	Name       string `json:"name"`
	Status     string `json:"status"`  // "original", "scaffolded", "implemented", "verified", "shadow", "cutover"
	Routing    string `json:"routing"` // "go", "original", "shadow", "unknown"
	Idempotent bool   `json:"idempotent"`
	CasesCount int    `json:"cases_count"`
}

// MapOutput encapsulates the function map for a unit.
type MapOutput struct {
	SchemaVersion  string          `json:"schema_version,omitempty"`
	UnitID         string          `json:"unit_id"`
	TotalFunctions int             `json:"total_functions"`
	Functions      []FunctionEntry `json:"functions"`
	Timestamp      string          `json:"timestamp"`
}
