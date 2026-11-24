package profile

// Input defines parameters for the profile use-case.
type Input struct {
	UnitID string `json:"unit_id"`
	Top    int    `json:"top"` // default: 10
	Dir    string `json:"dir"`
}

// Candidate represents a profiled hot function evaluated for migration suitability.
type Candidate struct {
	Rank              int     `json:"rank"`
	FunctionName      string  `json:"function_name"`
	File              string  `json:"file"`
	Line              int     `json:"line,omitempty"`
	SelfPercent       float64 `json:"self_percent"`
	CumulativePercent float64 `json:"cumulative_percent"`
	CallCount         int     `json:"call_count"`
	IsPureHeuristic   bool    `json:"is_pure_heuristic"`
	PurityReason      string  `json:"purity_reason"`
}

// Output encapsulates the hot function candidate profiling report.
type Output struct {
	SchemaVersion   string      `json:"schema_version,omitempty"`
	UnitID          string      `json:"unit_id"`
	Language        string      `json:"language"`
	Profiler        string      `json:"profiler"`
	TotalDurationMS float64     `json:"total_duration_ms"`
	Top             int         `json:"top"`
	Candidates      []Candidate `json:"candidates"`
	Summary         string      `json:"summary"`
	Timestamp       string      `json:"timestamp"`
}
