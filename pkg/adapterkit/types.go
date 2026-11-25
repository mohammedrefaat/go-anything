package adapterkit

// UnitState defines the 10 lifecycle states of a migration unit.
type UnitState string

const (
	StatePlanned    UnitState = "Planned"
	StateRecorded   UnitState = "Recorded"
	StateReady      UnitState = "Ready"
	StateInProgress UnitState = "In progress"
	StateVerified   UnitState = "Verified"
	StateShadow     UnitState = "Shadow"
	StateApproved   UnitState = "Approved"
	StateCutover    UnitState = "Cutover"
	StateCompleted  UnitState = "Completed"
	StateBlocked    UnitState = "Blocked"
)

// EvidenceProfile specifies the evidence verification depth required for a unit.
type EvidenceProfile string

const (
	// ProfileReplay requires zero mismatches on recorded test cases (M0 through M4.2).
	ProfileReplay EvidenceProfile = "replay"
	// ProfileFull requires all Quality Gate checks and zero mismatches (M4.3 onwards).
	ProfileFull EvidenceProfile = "full"
)

// Manifest represents .go-anything/manifest.json indexing discovered units and waves.
type Manifest struct {
	SchemaVersion  string         `json:"schema_version"`
	ProjectName    string         `json:"project_name,omitempty"`
	SourceLanguage string         `json:"source_language,omitempty"`
	TargetLanguage string         `json:"target_language,omitempty"`
	Units          []string       `json:"units"`
	Waves          []Wave         `json:"waves"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Wave represents a group of units that can be migrated concurrently.
type Wave struct {
	Wave    int      `json:"wave"`
	UnitIDs []string `json:"unit_ids"`
}

// Unit represents a durable unit state file under .go-anything/units/<id>.json.
type Unit struct {
	SchemaVersion      string          `json:"schema_version"`
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	PackagePath        string          `json:"package_path,omitempty"`
	Wave               int             `json:"wave,omitempty"`
	State              UnitState       `json:"state"`
	Dependencies       []string        `json:"dependencies,omitempty"`
	Risk               string          `json:"risk,omitempty"`
	EvidenceProfile    EvidenceProfile `json:"evidence_profile"`
	UnreplayableFields []string        `json:"unreplayable_fields,omitempty"`
	FallbackCount      int             `json:"fallback_count,omitempty"`
	LastUpdated        string          `json:"last_updated,omitempty"`
	Metadata           map[string]any  `json:"metadata,omitempty"`
}

// Rules represents .go-anything/rules.json containing wildcards and normalizers.
type Rules struct {
	SchemaVersion  string           `json:"schema_version"`
	Wildcards      []WildcardRule   `json:"wildcards"`
	Normalizers    []NormalizerRule `json:"normalizers"`
	FieldSemantics []FieldSemantic  `json:"field_semantics,omitempty"`
	Metadata       map[string]any   `json:"metadata,omitempty"`
}

// WildcardRule identifies dynamic fields excluded or generalized during comparison.
type WildcardRule struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Pattern     string `json:"pattern,omitempty"`
	Description string `json:"description,omitempty"`
}

// NormalizerRule identifies transformations applied prior to comparison.
type NormalizerRule struct {
	Path   string         `json:"path"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// CaseKind categorizes recorded behavioral cases.
type CaseKind string

const (
	KindHappy    CaseKind = "happy"
	KindEdge     CaseKind = "edge"
	KindTrap     CaseKind = "trap"
	KindFuzz     CaseKind = "fuzz"
	KindCaptured CaseKind = "captured"
	KindFromTest CaseKind = "from-test"
)

// CaseInput defines the input payload delivered to the unit.
type CaseInput struct {
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    any               `json:"body,omitempty"`
	Argv    []string          `json:"argv,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Files   map[string]string `json:"files,omitempty"`
}

// Effect captures an observed outbound side effect (HTTP request, SMTP email, etc.).
type Effect struct {
	Type         string            `json:"type"` // "http", "smtp", "generic"
	Order        int               `json:"order"`
	Target       string            `json:"target"`
	Method       string            `json:"method,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyHash     string            `json:"body_hash,omitempty"`
	EnvelopeFrom string            `json:"envelope_from,omitempty"`
	EnvelopeTo   []string          `json:"envelope_to,omitempty"`
	Subject      string            `json:"subject,omitempty"`
	Count        int               `json:"count,omitempty"`
}

// CaseOutput defines the recorded output observed from execution.
type CaseOutput struct {
	StatusCode   int               `json:"status_code,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         any               `json:"body,omitempty"`
	Stdout       string            `json:"stdout,omitempty"`
	Stderr       string            `json:"stderr,omitempty"`
	ExitCode     int               `json:"exit_code,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	Effects      []Effect          `json:"effects,omitempty"`
}

// Case represents a recorded behavioral case stored in .go-anything/cases/<unit>.jsonl.
type Case struct {
	SchemaVersion      string         `json:"schema_version"`
	CaseID             string         `json:"case_id"`
	UnitID             string         `json:"unit_id"`
	Kind               CaseKind       `json:"kind"`
	SourceRevision     string         `json:"source_revision"`
	Runtime            string         `json:"runtime"`
	CapturedAt         string         `json:"captured_at"`
	TrapType           string         `json:"trap_type,omitempty"`
	Input              CaseInput      `json:"input"`
	ExpectedOutput     CaseOutput     `json:"expected_output"`
	Effects            []Effect       `json:"effects,omitempty"`
	Wildcards          []string       `json:"wildcards,omitempty"`
	UnreplayableFields []string       `json:"unreplayable_fields,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

// ReportType specifies the kind of evaluation report.
type ReportType string

const (
	ReportTypeDiff      ReportType = "diff"
	ReportTypeReplay    ReportType = "replay"
	ReportTypeBench     ReportType = "bench"
	ReportTypeGate      ReportType = "gate"
	ReportTypeScorecard ReportType = "scorecard"
)

// ReportStatus indicates the outcome of a report or check.
type ReportStatus string

const (
	StatusPass    ReportStatus = "pass"
	StatusFail    ReportStatus = "fail"
	StatusSkipped ReportStatus = "skipped"
)

// Mismatch details a single field discrepancy between original and ported output.
type Mismatch struct {
	Path     string `json:"path"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
	Message  string `json:"message"`
}

// Report represents comparison, diff, and benchmark reports under .go-anything/reports/.
type Report struct {
	SchemaVersion string         `json:"schema_version"`
	ReportID      string         `json:"report_id"`
	UnitID        string         `json:"unit_id"`
	CaseID        string         `json:"case_id,omitempty"`
	ReportType    ReportType     `json:"report_type"`
	Mode          string         `json:"mode,omitempty"`
	Status        ReportStatus   `json:"status"`
	Mismatches    []Mismatch     `json:"mismatches,omitempty"`
	CreatedAt     string         `json:"created_at"`
	DurationMS    float64        `json:"duration_ms,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
}

// GateCheck represents an individual check executed as part of the Quality Gate.
type GateCheck struct {
	Name       string       `json:"name"`
	Status     ReportStatus `json:"status"`
	Output     string       `json:"output,omitempty"`
	DurationMS float64      `json:"duration_ms,omitempty"`
}

// Evidence represents cryptographic verification records stored under .go-anything/evidence/.
type Evidence struct {
	SchemaVersion      string          `json:"schema_version"`
	EvidenceID         string          `json:"evidence_id"`
	UnitID             string          `json:"unit_id"`
	CommitSHA          string          `json:"commit_sha"`
	Profile            EvidenceProfile `json:"profile"`
	VerifiedAt         string          `json:"verified_at"`
	VerifiedBy         string          `json:"verified_by"`
	Checks             []GateCheck     `json:"checks"`
	MismatchesCount    int             `json:"mismatches_count"`
	UnreplayableFields []string        `json:"unreplayable_fields,omitempty"`
	Metadata           map[string]any  `json:"metadata,omitempty"`
}

// AdapterManifest describes static metadata declared by a language adapter,
// including the source language and file extensions it handles.
type AdapterManifest struct {
	Name           string   `json:"name"`
	SourceLanguage string   `json:"source_language"`
	Extensions     []string `json:"extensions"`
}
