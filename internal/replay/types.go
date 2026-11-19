package replay

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CaseResult encapsulates the deterministic replay result for a single case.
type CaseResult struct {
	CaseID     string                `json:"case_id"`
	UnitID     string                `json:"unit_id"`
	Kind       adapterkit.CaseKind   `json:"kind"`
	Passed     bool                  `json:"passed"`
	DurationMS float64               `json:"duration_ms"`
	Mismatches []adapterkit.Mismatch `json:"mismatches,omitempty"`
	Error      string                `json:"error,omitempty"`
}

// ReplayReport summarizes concurrent replay outcomes across all cases for a unit.
// All Results are strictly sorted by case_id in accordance with Rule 2.
type ReplayReport struct {
	SchemaVersion string       `json:"schema_version"`
	UnitID        string       `json:"unit_id"`
	TotalCases    int          `json:"total_cases"`
	PassedCases   int          `json:"passed_cases"`
	FailedCases   int          `json:"failed_cases"`
	Results       []CaseResult `json:"results"` // Strictly sorted by CaseID!
	Summary       string       `json:"summary"`
}

// ToJSON returns a deterministic JSON representation of the replay report.
func (r *ReplayReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ToMarkdown returns a deterministic formatted markdown scorecard representation.
func (r *ReplayReport) ToMarkdown() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Replay Report: %s\n\n", r.UnitID))
	sb.WriteString(fmt.Sprintf("- **Total Cases:** %d\n", r.TotalCases))
	sb.WriteString(fmt.Sprintf("- **Passed:** %d\n", r.PassedCases))
	sb.WriteString(fmt.Sprintf("- **Failed:** %d\n", r.FailedCases))
	sb.WriteString(fmt.Sprintf("- **Status:** %s\n\n", r.Summary))

	if len(r.Results) > 0 {
		sb.WriteString("| Case ID | Kind | Status | Duration (ms) | Discrepancies |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- | :--- |\n")
		for _, res := range r.Results {
			statusStr := "PASS"
			if !res.Passed {
				statusStr = "**FAIL**"
			}
			discStr := "-"
			if len(res.Mismatches) > 0 {
				discStr = fmt.Sprintf("%d mismatch(es)", len(res.Mismatches))
			} else if res.Error != "" {
				discStr = res.Error
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %.2f | %s |\n",
				res.CaseID,
				res.Kind,
				statusStr,
				res.DurationMS,
				discStr,
			))
		}
	}

	return sb.String()
}
