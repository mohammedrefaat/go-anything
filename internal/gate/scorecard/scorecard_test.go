package scorecard_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/bench"
	"github.com/mohammedrefat/go-anything/internal/gate/scorecard"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScorecard_GenerateMarkdown(t *testing.T) {
	data := scorecard.ScorecardData{
		ProjectName: "Payments Service",
		CommitSHA:   "0123456789abcdef0123456789abcdef01234567",
		GeneratedAt: time.Date(2026, 11, 19, 14, 0, 0, 0, time.UTC),
		Units: []scorecard.UnitScorecard{
			{
				UnitID: "pkg/currency",
				State:  adapterkit.StateVerified,
				Cases: scorecard.CaseBreakdown{
					Happy:    1000,
					Edge:     400,
					Trap:     100,
					Fuzz:     50,
					Captured: 30,
				},
				Mismatches:         0,
				OriginalCoverage:   82.5,
				RaceDetectorStatus: "PASS",
				UnreplayableFields: []string{"timestamp", "uuid"},
			},
			{
				UnitID: "pkg/fraud",
				State:  adapterkit.StateCutover,
				Cases: scorecard.CaseBreakdown{
					Happy:    2000,
					Edge:     800,
					Trap:     200,
					Fuzz:     150,
					Captured: 90,
				},
				Mismatches:         0,
				OriginalCoverage:   75.0,
				RaceDetectorStatus: "PASS",
				UnreplayableFields: []string{"session_id"},
			},
		},
		Benchmarks: []bench.BenchmarkResult{
			{
				Name: "CurrencyConversion",
				Legacy: bench.Metric{
					DurationPerOp:   1500 * time.Nanosecond,
					AllocBytesPerOp: 128,
				},
				Modern: bench.Metric{
					DurationPerOp:   950 * time.Nanosecond,
					AllocBytesPerOp: 64,
				},
				LatencyDeltaPercent: -36.7,
				AllocDeltaPercent:   -50.0,
			},
		},
	}

	md := scorecard.GenerateMarkdown(data)
	require.NotEmpty(t, md)

	// Check Title & Header
	assert.Contains(t, md, "# Parity Scorecard — Payments Service")
	assert.Contains(t, md, "**Commit:** `0123456789abcdef0123456789abcdef01234567`")

	// Check Summary line matching v3 specification
	assert.Contains(t, md, "> **Summary:** Units 2/2 · Cases 4,820 (happy: 3000, edge: 1200, trap: 300, fuzz: 200, captured: 120) · Mismatches 0 · Original coverage 78.8% · Race detector: PASS")

	// Rule check: Never show "Mismatches: 0" without case breakdown and coverage beside it
	assert.Contains(t, md, "| `pkg/currency` | `Verified` | 1580 (H:1000, E:400, T:100, F:50, C:30) | 0 | 82.5% | PASS | timestamp, uuid |")
	assert.Contains(t, md, "| `pkg/fraud` | `Cutover` | 3240 (H:2000, E:800, T:200, F:150, C:90) | 0 | 75.0% | PASS | session_id |")

	// Check Case breakdown by kind
	assert.Contains(t, md, "## Case Breakdown by Kind")
	assert.Contains(t, md, "| **Happy** | 3,000 | 62.2% |")
	assert.Contains(t, md, "| **Edge** | 1,200 | 24.9% |")
	assert.Contains(t, md, "| **Trap** | 300 | 6.2% |")
	assert.Contains(t, md, "| **Fuzz** | 200 | 4.1% |")
	assert.Contains(t, md, "| **Captured** | 120 | 2.5% |")
	assert.Contains(t, md, "| **Total** | **4,820** | 100.0% |")

	// Check Unreplayable Fields list
	assert.Contains(t, md, "## Unreplayable Fields")
	assert.Contains(t, md, "- `session_id`")
	assert.Contains(t, md, "- `timestamp`")
	assert.Contains(t, md, "- `uuid`")

	// Check Benchmarks section
	assert.Contains(t, md, "## Benchmarks (Informational)")
	assert.Contains(t, md, "| `CurrencyConversion` | 1.5µs/op | 950ns/op | -36.7% | 128 B/op | 64 B/op | -50.0% |")
}

func TestScorecard_SaveScorecard(t *testing.T) {
	tmpDir := t.TempDir()
	scorecardFile := filepath.Join(tmpDir, ".go-anything", "reports", "scorecard.md")

	data := scorecard.ScorecardData{
		ProjectName: "Demo App",
		Units: []scorecard.UnitScorecard{
			{
				UnitID: "unit-demo",
				State:  adapterkit.StateVerified,
				Cases: scorecard.CaseBreakdown{
					Happy: 50,
				},
				Mismatches:         0,
				OriginalCoverage:   100.0,
				RaceDetectorStatus: "PASS",
			},
		},
	}

	err := scorecard.SaveScorecard(scorecardFile, data)
	require.NoError(t, err)

	content, err := os.ReadFile(scorecardFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "# Parity Scorecard — Demo App")
	assert.Contains(t, string(content), "Mismatches 0 · Original coverage 100.0%")
}

func TestScorecard_EmptyUnits(t *testing.T) {
	data := scorecard.ScorecardData{
		ProjectName: "Empty Project",
	}

	md := scorecard.GenerateMarkdown(data)
	assert.Contains(t, md, "Units 0/0")
	assert.Contains(t, md, "Cases 0")
	assert.Contains(t, md, "None declared. All outputs are deterministically verifiable.")
}
