package gate

// Input defines the parameters for the gate use-case.
type Input struct {
	UnitID            string `json:"unit_id"`
	Profile           string `json:"profile"`
	AllowMissingTools bool   `json:"allow_missing_tools"`
	Dir               string `json:"dir"`
	Scorecard         bool   `json:"scorecard"`
}

// CheckResult details an individual check executed in the quality gate.
type CheckResult struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"` // "pass", "fail", "skipped"
	DurationMS float64  `json:"duration_ms"`
	Issues     []string `json:"issues,omitempty"`
	Output     string   `json:"output,omitempty"`
}

// Output encapsulates the quality gate verification and scorecard results.
type Output struct {
	SchemaVersion string        `json:"schema_version,omitempty"`
	UnitID        string        `json:"unit_id"`
	Profile       string        `json:"profile"`
	Passed        bool          `json:"passed"`
	CommitSHA     string        `json:"commit_sha,omitempty"`
	DurationMS    float64       `json:"duration_ms,omitempty"`
	Checks        []CheckResult `json:"checks"`
	ScorecardPath string        `json:"scorecard_path,omitempty"`
	EvidenceSaved bool          `json:"evidence_saved"`
	Summary       string        `json:"summary,omitempty"`
}
