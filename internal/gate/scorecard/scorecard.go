package scorecard

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mohammedrefat/go-anything/internal/bench"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CaseBreakdown aggregates case counts by category.
type CaseBreakdown struct {
	Happy    int `json:"happy"`
	Edge     int `json:"edge"`
	Trap     int `json:"trap"`
	Fuzz     int `json:"fuzz"`
	Captured int `json:"captured"`
}

// Total returns the sum of all cases across categories.
func (cb CaseBreakdown) Total() int {
	return cb.Happy + cb.Edge + cb.Trap + cb.Fuzz + cb.Captured
}

// String returns a compact formatted breakdown.
func (cb CaseBreakdown) String() string {
	return fmt.Sprintf("happy: %d, edge: %d, trap: %d, fuzz: %d, captured: %d",
		cb.Happy, cb.Edge, cb.Trap, cb.Fuzz, cb.Captured)
}

// UnitScorecard holds parity and verification metrics for a single migration unit.
type UnitScorecard struct {
	UnitID             string                 `json:"unit_id"`
	State              adapterkit.UnitState   `json:"state"`
	Cases              CaseBreakdown          `json:"cases"`
	Mismatches         int                    `json:"mismatches"`
	OriginalCoverage   float64                `json:"original_coverage"`    // percentage, e.g. 71.0
	RaceDetectorStatus string                 `json:"race_detector_status"` // "PASS" or "FAIL"
	UnreplayableFields []string               `json:"unreplayable_fields,omitempty"`
	Benchmark          *bench.BenchmarkResult `json:"benchmark,omitempty"`
	CommitSHA          string                 `json:"commit_sha,omitempty"`
}

// ScorecardData aggregates project-level parity metrics across all units.
type ScorecardData struct {
	ProjectName        string                  `json:"project_name"`
	CommitSHA          string                  `json:"commit_sha"`
	GeneratedAt        time.Time               `json:"generated_at"`
	TotalUnits         int                     `json:"total_units"`
	VerifiedUnits      int                     `json:"verified_units"`
	GlobalRaceStatus   string                  `json:"global_race_status"`
	TotalCases         int                     `json:"total_cases"`
	TotalMismatches    int                     `json:"total_mismatches"`
	OverallCoverage    float64                 `json:"overall_coverage"`
	CaseBreakdown      CaseBreakdown           `json:"case_breakdown"`
	Units              []UnitScorecard         `json:"units"`
	UnreplayableFields []string                `json:"unreplayable_fields,omitempty"`
	Benchmarks         []bench.BenchmarkResult `json:"benchmarks,omitempty"`
}

// RecalculateTotals computes aggregate statistics from the individual UnitScorecards.
func (sd *ScorecardData) RecalculateTotals() {
	sd.TotalUnits = len(sd.Units)
	sd.VerifiedUnits = 0
	sd.TotalCases = 0
	sd.TotalMismatches = 0
	sd.CaseBreakdown = CaseBreakdown{}

	var totalCoverage float64
	var coverageCount int
	unrepMap := make(map[string]bool)
	allRacePass := true

	for _, u := range sd.Units {
		if u.State == adapterkit.StateVerified || u.State == adapterkit.StateCutover || u.State == adapterkit.StateCompleted {
			sd.VerifiedUnits++
		}
		sd.TotalCases += u.Cases.Total()
		sd.TotalMismatches += u.Mismatches
		sd.CaseBreakdown.Happy += u.Cases.Happy
		sd.CaseBreakdown.Edge += u.Cases.Edge
		sd.CaseBreakdown.Trap += u.Cases.Trap
		sd.CaseBreakdown.Fuzz += u.Cases.Fuzz
		sd.CaseBreakdown.Captured += u.Cases.Captured

		if u.OriginalCoverage > 0 {
			totalCoverage += u.OriginalCoverage
			coverageCount++
		}

		if u.RaceDetectorStatus != "PASS" && u.RaceDetectorStatus != "" {
			allRacePass = false
		}

		for _, f := range u.UnreplayableFields {
			unrepMap[f] = true
		}
	}

	if coverageCount > 0 {
		sd.OverallCoverage = totalCoverage / float64(coverageCount)
	}

	if allRacePass && len(sd.Units) > 0 {
		sd.GlobalRaceStatus = "PASS"
	} else if len(sd.Units) > 0 {
		sd.GlobalRaceStatus = "FAIL"
	} else {
		sd.GlobalRaceStatus = "N/A"
	}

	sd.UnreplayableFields = make([]string, 0, len(unrepMap))
	for f := range unrepMap {
		sd.UnreplayableFields = append(sd.UnreplayableFields, f)
	}
	sort.Strings(sd.UnreplayableFields)
}

// GenerateMarkdown formats the scorecard into GitHub-flavored Markdown for PR comments and reports.
// Execution Rule: Never show "Mismatches: 0" without case breakdown and coverage beside it.
func GenerateMarkdown(data ScorecardData) string {
	data.RecalculateTotals()

	var buf bytes.Buffer

	projName := data.ProjectName
	if projName == "" {
		projName = "Migration Project"
	}

	buf.WriteString(fmt.Sprintf("# Parity Scorecard — %s\n\n", projName))

	// Highlight Summary Line matching v3 architecture specification
	buf.WriteString(fmt.Sprintf("> **Summary:** Units %d/%d · Cases %s (%s) · Mismatches %d · Original coverage %.1f%% · Race detector: %s\n\n",
		data.VerifiedUnits,
		data.TotalUnits,
		formatNumber(data.TotalCases),
		data.CaseBreakdown.String(),
		data.TotalMismatches,
		data.OverallCoverage,
		data.GlobalRaceStatus,
	))

	if data.CommitSHA != "" {
		buf.WriteString(fmt.Sprintf("**Commit:** `%s` · **Generated:** %s\n\n", data.CommitSHA, data.GeneratedAt.UTC().Format(time.RFC3339)))
	}

	// 1. Units Summary Table
	buf.WriteString("## Units Summary\n\n")
	buf.WriteString("| Unit | State | Cases (Breakdown) | Mismatches | Original Coverage | Race Detector | Unreplayable Fields |\n")
	buf.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

	for _, u := range data.Units {
		unrep := "none"
		if len(u.UnreplayableFields) > 0 {
			unrep = strings.Join(u.UnreplayableFields, ", ")
		}

		breakdownStr := fmt.Sprintf("%d (H:%d, E:%d, T:%d, F:%d, C:%d)",
			u.Cases.Total(), u.Cases.Happy, u.Cases.Edge, u.Cases.Trap, u.Cases.Fuzz, u.Cases.Captured)

		raceStr := u.RaceDetectorStatus
		if raceStr == "" {
			raceStr = "PASS"
		}

		buf.WriteString(fmt.Sprintf("| `%s` | `%s` | %s | %d | %.1f%% | %s | %s |\n",
			u.UnitID,
			u.State,
			breakdownStr,
			u.Mismatches,
			u.OriginalCoverage,
			raceStr,
			unrep,
		))
	}
	buf.WriteString("\n")

	// 2. Case Breakdown Table
	buf.WriteString("## Case Breakdown by Kind\n\n")
	buf.WriteString("| Kind | Count | Proportion | Description |\n")
	buf.WriteString("| :--- | :--- | :--- | :--- |\n")

	tot := data.TotalCases
	pct := func(n int) string {
		if tot == 0 {
			return "0.0%"
		}
		return fmt.Sprintf("%.1f%%", (float64(n)/float64(tot))*100.0)
	}

	buf.WriteString(fmt.Sprintf("| **Happy** | %s | %s | Standard happy-path behavioral cases |\n", formatNumber(data.CaseBreakdown.Happy), pct(data.CaseBreakdown.Happy)))
	buf.WriteString(fmt.Sprintf("| **Edge** | %s | %s | Boundary inputs and corner cases |\n", formatNumber(data.CaseBreakdown.Edge), pct(data.CaseBreakdown.Edge)))
	buf.WriteString(fmt.Sprintf("| **Trap** | %s | %s | Semantic traps and panic characterizations |\n", formatNumber(data.CaseBreakdown.Trap), pct(data.CaseBreakdown.Trap)))
	buf.WriteString(fmt.Sprintf("| **Fuzz** | %s | %s | Mutated inputs from differential fuzzer |\n", formatNumber(data.CaseBreakdown.Fuzz), pct(data.CaseBreakdown.Fuzz)))
	buf.WriteString(fmt.Sprintf("| **Captured** | %s | %s | Real-world captured production traffic |\n", formatNumber(data.CaseBreakdown.Captured), pct(data.CaseBreakdown.Captured)))
	buf.WriteString(fmt.Sprintf("| **Total** | **%s** | 100.0%% | Recorded & replayed case suite |\n\n", formatNumber(tot)))

	// 3. Unreplayable Fields
	buf.WriteString("## Unreplayable Fields\n\n")
	if len(data.UnreplayableFields) == 0 {
		buf.WriteString("None declared. All outputs are deterministically verifiable.\n\n")
	} else {
		buf.WriteString("The following volatile fields are masked during replay:\n")
		for _, f := range data.UnreplayableFields {
			buf.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		buf.WriteString("\n")
	}

	// 4. Benchmarks (Informational, Non-blocking)
	if len(data.Benchmarks) > 0 {
		buf.WriteString("## Benchmarks (Informational)\n\n")
		buf.WriteString("> Note: Performance benchmarks are informational and never block quality gates or promotions.\n\n")
		buf.WriteString("| Target | Legacy Latency | Modern Latency | Latency Delta | Legacy Alloc | Modern Alloc | Alloc Delta |\n")
		buf.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

		for _, b := range data.Benchmarks {
			buf.WriteString(fmt.Sprintf("| `%s` | %s/op | %s/op | %+.1f%% | %d B/op | %d B/op | %+.1f%% |\n",
				b.Name,
				b.Legacy.DurationPerOp,
				b.Modern.DurationPerOp,
				b.LatencyDeltaPercent,
				b.Legacy.AllocBytesPerOp,
				b.Modern.AllocBytesPerOp,
				b.AllocDeltaPercent,
			))
		}
		buf.WriteString("\n")
	}

	return buf.String()
}

// SaveScorecard writes the generated scorecard Markdown to filePath.
func SaveScorecard(filePath string, data ScorecardData) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for scorecard %s: %w", filePath, err)
	}

	content := GenerateMarkdown(data)
	return os.WriteFile(filePath, []byte(content), 0644)
}

func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}
