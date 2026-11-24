package fuzz

import (
	"time"
)

// Input defines the parameters for the differential fuzz use-case.
type Input struct {
	UnitID          string        `json:"unit_id"`
	Time            time.Duration `json:"time"`
	Runs            int           `json:"runs"`
	Seed            int64         `json:"seed"`
	MaxInputBytes   int           `json:"max_input_bytes"`
	PromoteFindings bool          `json:"promote_findings"`
	Dir             string        `json:"dir"`
	Target          string        `json:"target,omitempty"`
}

// DivergenceRecord details a single behavioral divergence discovered by the fuzzer.
type DivergenceRecord struct {
	CaseID       string `json:"case_id"`
	Input        any    `json:"input"`
	LegacyOutput any    `json:"legacy_output"`
	ModernOutput any    `json:"modern_output"`
	Diff         any    `json:"diff"`
}

// Output encapsulates the differential fuzzing execution metrics and discovered divergences.
type Output struct {
	SchemaVersion    string             `json:"schema_version,omitempty"`
	UnitID           string             `json:"unit_id"`
	DurationMS       float64            `json:"duration_ms"`
	TotalRuns        int                `json:"total_runs"`
	DivergencesCount int                `json:"divergences_count"`
	Divergences      []DivergenceRecord `json:"divergences"`
	PromotedCount    int                `json:"promoted_count"`
	PromotedCases    []string           `json:"promoted_cases"`
	CorpusPath       string             `json:"corpus_path"`
	Passed           bool               `json:"passed"`
	Summary          string             `json:"summary,omitempty"`
}
