// Package recorder provides lightweight test observation hooks for Go tests
// to record inputs and outputs during test execution when GO_ANYTHING_RECORD is enabled.
package recorder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	recordMu sync.Mutex
)

// ObservationRecord encapsulates an observed function invocation from a test.
type ObservationRecord struct {
	UnitID    string    `json:"unit_id"`
	FnName    string    `json:"fn_name"`
	TestName  string    `json:"test_name"`
	Input     any       `json:"input"`
	Output    any       `json:"output"`
	Timestamp time.Time `json:"timestamp"`
}

// Observe records a function invocation if the GO_ANYTHING_RECORD environment variable is set.
// If GO_ANYTHING_RECORD is unset, Observe is a zero-overhead no-op.
func Observe(unitID string, fnName string, in any, out any) error {
	dest := os.Getenv("GO_ANYTHING_RECORD")
	if dest == "" {
		return nil
	}

	filterUnit := os.Getenv("GO_ANYTHING_UNIT")
	if filterUnit != "" && filterUnit != unitID {
		return nil
	}

	testName := detectCallerTestName()

	rec := ObservationRecord{
		UnitID:    unitID,
		FnName:    fnName,
		TestName:  testName,
		Input:     in,
		Output:    out,
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to marshal observation record: %w", err)
	}

	recordMu.Lock()
	defer recordMu.Unlock()

	var targetFile string
	sanitizedUnit := strings.ReplaceAll(unitID, "/", "_")
	fi, err := os.Stat(dest)
	if err == nil && fi.IsDir() {
		targetFile = filepath.Join(dest, fmt.Sprintf("%s_observed.jsonl", sanitizedUnit))
	} else if err != nil && (filepath.Ext(dest) == "" || strings.HasSuffix(dest, string(filepath.Separator))) {
		_ = os.MkdirAll(dest, 0755)
		targetFile = filepath.Join(dest, fmt.Sprintf("%s_observed.jsonl", sanitizedUnit))
	} else {
		targetFile = dest
	}
	dir := filepath.Dir(targetFile)
	_ = os.MkdirAll(dir, 0755)

	f, err := os.OpenFile(targetFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open record target file %s: %w", targetFile, err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write record line: %w", err)
	}

	return nil
}

func detectCallerTestName() string {
	var pcs [32]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])

	for {
		frame, more := frames.Next()
		fn := frame.Function
		// Look for testing functions like TestXxx, BenchmarkXxx, ExampleXxx
		parts := strings.Split(fn, ".")
		shortName := parts[len(parts)-1]
		if strings.HasPrefix(shortName, "Test") || strings.HasPrefix(shortName, "Benchmark") || strings.HasPrefix(shortName, "Example") {
			return shortName
		}
		if !more {
			break
		}
	}
	return "test"
}
