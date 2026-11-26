package diff

import (
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// Input defines the parameters for the diff use-case.
type Input struct {
	UnitID     string `json:"unit_id"`
	Target     string `json:"target"`
	Mode       string `json:"mode"`
	Jobs       int    `json:"jobs"`
	Dir        string `json:"dir"`
	CaseFilter string `json:"case_filter"`
}

// MismatchDetail provides structured details of a behavioral discrepancy in a case.
type MismatchDetail struct {
	CaseID  string                `json:"case_id"`
	Diffs   []adapterkit.Mismatch `json:"diffs"`
	Inputs  any                   `json:"inputs,omitempty"`
	Outputs any                   `json:"outputs,omitempty"`
	Message string                `json:"message,omitempty"`
	Error   string                `json:"error,omitempty"`
}

// Output encapsulates the diff and replay comparison results.
type Output struct {
	SchemaVersion   string           `json:"schema_version,omitempty"`
	UnitID          string           `json:"unit_id"`
	Target          string           `json:"target"`
	Mode            string           `json:"mode,omitempty"`
	TotalCases      int              `json:"total_cases"`
	MatchedCases    int              `json:"matched_cases"`
	Matched         int              `json:"matched"`
	MismatchedCases int              `json:"mismatched_cases"`
	Mismatched      int              `json:"mismatched"`
	Passed          bool             `json:"passed"`
	DurationMS      float64          `json:"duration_ms,omitempty"`
	Mismatches      []MismatchDetail `json:"mismatches"`
	MismatchList    []MismatchDetail `json:"mismatch_list,omitempty"`
	Summary         string           `json:"summary,omitempty"`
}
