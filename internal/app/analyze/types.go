package analyze

import (
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// Input defines the parameters for the analyze use-case.
type Input struct {
	Dir    string `json:"dir"`
	Source string `json:"source"`
	JSON   bool   `json:"json"`
}

// WaveInfo represents an execution wave and the units assigned to it.
type WaveInfo struct {
	Wave    int      `json:"wave"`
	UnitIDs []string `json:"unit_ids"`
}

// Output encapsulates the static analysis and dependency planning results.
type Output struct {
	SchemaVersion  string              `json:"schema_version,omitempty"`
	Project        string              `json:"project"`
	SourceLanguage string              `json:"source_language"`
	Source         string              `json:"source,omitempty"`
	Units          []*adapterkit.Unit  `json:"units"`
	Waves          []WaveInfo          `json:"waves"`
	Cycles         [][]string          `json:"cycles"`
	Graph          map[string][]string `json:"graph"`
	TotalUnits     int                 `json:"total_units,omitempty"`
	MaxWave        int                 `json:"max_wave,omitempty"`
}
