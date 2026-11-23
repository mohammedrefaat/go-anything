package shadow

// Input defines parameters for the shadow use-case.
type Input struct {
	UnitID     string  `json:"unit_id"`
	Action     string  `json:"action"` // "start", "status", "stop"
	Port       int     `json:"port"`
	SampleRate float64 `json:"sample_rate"`
	Dir        string  `json:"dir"`
}

// Output encapsulates the shadow execution or status results.
type Output struct {
	SchemaVersion    string  `json:"schema_version,omitempty"`
	UnitID           string  `json:"unit_id"`
	Action           string  `json:"action"`
	Active           bool    `json:"active"`
	Port             int     `json:"port"`
	SampleRate       float64 `json:"sample_rate"`
	RequestsTotal    int64   `json:"requests_total"`
	TotalRequests    int64   `json:"total_requests"`
	RequestsShadowed int64   `json:"requests_shadowed"`
	Shadowed         int64   `json:"shadowed"`
	OverflowDropped  int64   `json:"overflow_dropped"`
	MismatchesCount  int64   `json:"mismatches_count"`
	Mismatches       int64   `json:"mismatches"`
	Summary          string  `json:"summary,omitempty"`
}
