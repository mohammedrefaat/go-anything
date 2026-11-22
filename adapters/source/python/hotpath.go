package python

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// HotPathEntry represents an individual profiled Python function execution statistic.
type HotPathEntry struct {
	Function         string  `json:"function"`
	Filename         string  `json:"filename"`
	LineNumber       int     `json:"line_number"`
	Calls            int64   `json:"calls"`
	PrimitiveCalls   int64   `json:"primitive_calls,omitempty"`
	TotalTime        float64 `json:"total_time"`          // tottime (seconds)
	TotalTimePerCall float64 `json:"total_time_per_call"` // percall
	CumulativeTime   float64 `json:"cumulative_time"`     // cumtime (seconds)
	CumTimePerCall   float64 `json:"cum_time_per_call"`   // percall
	UnitID           string  `json:"unit_id,omitempty"`
	HotnessScore     float64 `json:"hotness_score"` // 0.0 to 1.0 normalized
}

// HotPathReport encapsulates the parsed and analyzed cProfile profiling outcome.
type HotPathReport struct {
	TotalFunctionCalls int64          `json:"total_function_calls"`
	TotalDurationSec   float64        `json:"total_duration_sec"`
	Entries            []HotPathEntry `json:"entries"`
	TopBottlenecks     []HotPathEntry `json:"top_bottlenecks"`
}

// HotPathExtractor parses cProfile text reports and extracts prioritized performance profiles.
type HotPathExtractor struct{}

// NewHotPathExtractor creates a new hot path extractor.
func NewHotPathExtractor() *HotPathExtractor {
	return &HotPathExtractor{}
}

// Regex matching cProfile stats lines:
// e.g. "   100/50    0.002    0.000    0.015    0.000 /path/to/mod.py:42(compute)"
// or   "      100    0.002    0.000    0.015    0.000 /path/to/mod.py:42(compute)"
// or   "        1    0.000    0.000    0.000    0.000 {built-in method math.sqrt}"
var (
	cprofileHeaderRegex = regexp.MustCompile(`(?i)^\s*(\d+)\s+function\s+calls(?:\s+\((\d+)\s+primitive\s+calls\))?\s+in\s+([\d.]+)\s+seconds`)
	cprofileRowRegex    = regexp.MustCompile(`^\s*(\d+(?:/\d+)?)\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+(.*)`)
	funcLocationRegex   = regexp.MustCompile(`^(.*):(\d+)\((.*)\)$`)
)

// ParseReport parses cProfile output text into a structured HotPathReport.
func (e *HotPathExtractor) ParseReport(content string, knownUnits []string) (*HotPathReport, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	report := &HotPathReport{}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Header line: "194 function calls in 0.001 seconds"
		if matches := cprofileHeaderRegex.FindStringSubmatch(trimmed); len(matches) > 3 {
			calls, _ := strconv.ParseInt(matches[1], 10, 64)
			report.TotalFunctionCalls = calls
			dur, _ := strconv.ParseFloat(matches[3], 64)
			report.TotalDurationSec = dur
			continue
		}

		// Stat row
		if matches := cprofileRowRegex.FindStringSubmatch(trimmed); len(matches) > 6 {
			callsStr := matches[1]
			tottime, _ := strconv.ParseFloat(matches[2], 64)
			percall1, _ := strconv.ParseFloat(matches[3], 64)
			cumtime, _ := strconv.ParseFloat(matches[4], 64)
			percall2, _ := strconv.ParseFloat(matches[5], 64)
			funcDesc := matches[6]

			var calls int64
			var primCalls int64
			if strings.Contains(callsStr, "/") {
				parts := strings.Split(callsStr, "/")
				c1, _ := strconv.ParseInt(parts[0], 10, 64)
				c2, _ := strconv.ParseInt(parts[1], 10, 64)
				calls = c1
				primCalls = c2
			} else {
				c, _ := strconv.ParseInt(callsStr, 10, 64)
				calls = c
				primCalls = c
			}

			fnName := funcDesc
			filename := ""
			lineno := 0

			if locMatches := funcLocationRegex.FindStringSubmatch(funcDesc); len(locMatches) > 3 {
				filename = locMatches[1]
				lineno, _ = strconv.Atoi(locMatches[2])
				fnName = locMatches[3]
			}

			unitID := deduceUnitFromPath(filename, knownUnits)

			report.Entries = append(report.Entries, HotPathEntry{
				Function:         fnName,
				Filename:         filename,
				LineNumber:       lineno,
				Calls:            calls,
				PrimitiveCalls:   primCalls,
				TotalTime:        tottime,
				TotalTimePerCall: percall1,
				CumulativeTime:   cumtime,
				CumTimePerCall:   percall2,
				UnitID:           unitID,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan cprofile output: %w", err)
	}

	// Calculate total cumulative time for normalization
	maxCumTime := 0.0
	for _, entry := range report.Entries {
		if entry.CumulativeTime > maxCumTime {
			maxCumTime = entry.CumulativeTime
		}
	}

	// Compute hotness score (0.0 to 1.0)
	for i := range report.Entries {
		if maxCumTime > 0 {
			report.Entries[i].HotnessScore = report.Entries[i].CumulativeTime / maxCumTime
		}
	}

	// Sort entries by cumulative time descending
	sort.Slice(report.Entries, func(i, j int) bool {
		return report.Entries[i].CumulativeTime > report.Entries[j].CumulativeTime
	})

	// Top bottlenecks (top 10 or all if fewer)
	topCount := 10
	if len(report.Entries) < topCount {
		topCount = len(report.Entries)
	}
	report.TopBottlenecks = append([]HotPathEntry{}, report.Entries[:topCount]...)

	return report, nil
}

func deduceUnitFromPath(filename string, knownUnits []string) string {
	if filename == "" || strings.HasPrefix(filename, "{") {
		return ""
	}
	norm := strings.ReplaceAll(filename, "\\", "/")
	for _, u := range knownUnits {
		if strings.Contains(norm, u) {
			return u
		}
	}
	return ""
}
