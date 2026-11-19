package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CompareReplayOutputs compares replay outputs across transports for a given case.
// It returns a slice of descriptive discrepancy messages if any differences are detected.
func CompareReplayOutputs(caseID string, outputs map[adapterkit.TransportType]*adapterkit.CaseOutput) []string {
	if len(outputs) <= 1 {
		return nil
	}

	// Deterministic transport ordering
	transports := make([]adapterkit.TransportType, 0, len(outputs))
	for t := range outputs {
		transports = append(transports, t)
	}
	sort.Slice(transports, func(i, j int) bool {
		return string(transports[i]) < string(transports[j])
	})

	var discrepancies []string
	baseTransport := transports[0]
	baseOut := outputs[baseTransport]

	for i := 1; i < len(transports); i++ {
		t := transports[i]
		out := outputs[t]

		if baseOut == nil && out == nil {
			continue
		}
		if (baseOut == nil) != (out == nil) {
			discrepancies = append(discrepancies, fmt.Sprintf(
				"case %s: transport %s returned nil output while %s returned non-nil output",
				caseID, t, baseTransport,
			))
			continue
		}

		if baseOut.StatusCode != out.StatusCode {
			discrepancies = append(discrepancies, fmt.Sprintf(
				"case %s: status code divergence between %s (%d) and %s (%d)",
				caseID, baseTransport, baseOut.StatusCode, t, out.StatusCode,
			))
		}

		if baseOut.ExitCode != out.ExitCode {
			discrepancies = append(discrepancies, fmt.Sprintf(
				"case %s: exit code divergence between %s (%d) and %s (%d)",
				caseID, baseTransport, baseOut.ExitCode, t, out.ExitCode,
			))
		}

		if baseOut.Stdout != out.Stdout {
			discrepancies = append(discrepancies, fmt.Sprintf(
				"case %s: stdout divergence between %s (%q) and %s (%q)",
				caseID, baseTransport, baseOut.Stdout, t, out.Stdout,
			))
		}

		if baseOut.Stderr != out.Stderr {
			discrepancies = append(discrepancies, fmt.Sprintf(
				"case %s: stderr divergence between %s (%q) and %s (%q)",
				caseID, baseTransport, baseOut.Stderr, t, out.Stderr,
			))
		}

		if baseOut.ErrorMessage != out.ErrorMessage {
			discrepancies = append(discrepancies, fmt.Sprintf(
				"case %s: error message divergence between %s (%q) and %s (%q)",
				caseID, baseTransport, baseOut.ErrorMessage, t, out.ErrorMessage,
			))
		}

		if !equalCaseBodies(baseOut.Body, out.Body) {
			discrepancies = append(discrepancies, fmt.Sprintf(
				"case %s: body divergence between %s and %s",
				caseID, baseTransport, t,
			))
		}
	}

	return discrepancies
}

// CompareReplayDrivers executes testCase across the provided replay drivers and checks that
// results match identically across all transports.
func CompareReplayDrivers(ctx context.Context, testCase *adapterkit.Case, drivers map[adapterkit.TransportType]adapterkit.Replay) ([]string, error) {
	if len(drivers) <= 1 {
		return nil, nil
	}

	outputs := make(map[adapterkit.TransportType]*adapterkit.CaseOutput, len(drivers))
	for transport, driver := range drivers {
		out, err := driver.ExecuteCase(ctx, testCase)
		if err != nil {
			return nil, fmt.Errorf("driver %s (%s) failed on case %s: %w",
				driver.DriverName(), transport, testCase.CaseID, err)
		}
		outputs[transport] = out
	}

	discrepancies := CompareReplayOutputs(testCase.CaseID, outputs)
	return discrepancies, nil
}

func equalCaseBodies(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if reflect.DeepEqual(a, b) {
		return true
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA == nil && errB == nil && string(ja) == string(jb) {
		return true
	}
	return false
}
