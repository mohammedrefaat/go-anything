package shadow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mohammedrefat/go-anything/internal/redactor"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// MismatchRecord represents a behavioral discrepancy logged to shadow_mismatches.jsonl.
// Conforms strictly to schemas/v1alpha1/reports.schema.json.
type MismatchRecord struct {
	SchemaVersion string                `json:"schema_version"`
	ReportID      string                `json:"report_id"`
	UnitID        string                `json:"unit_id"`
	CaseID        string                `json:"case_id,omitempty"`
	ReportType    string                `json:"report_type"`
	Mode          string                `json:"mode,omitempty"`
	Status        string                `json:"status"`
	Mismatches    []adapterkit.Mismatch `json:"mismatches,omitempty"`
	CreatedAt     string                `json:"created_at"`
	DurationMS    float64               `json:"duration_ms,omitempty"`
	Summary       string                `json:"summary,omitempty"`
	Details       map[string]any        `json:"details,omitempty"`
}

// MismatchLogger handles thread-safe fail-closed in-memory redaction and appending
// to the shadow mismatches log file.
type MismatchLogger struct {
	mu          sync.Mutex
	reportsPath string
	redactor    *redactor.Redactor
}

// NewMismatchLogger creates a new MismatchLogger targeting the specified reportsPath.
func NewMismatchLogger(reportsPath string, red *redactor.Redactor) *MismatchLogger {
	if reportsPath == "" {
		reportsPath = DefaultReportsPath
	}
	if red == nil {
		red = redactor.New()
	}
	return &MismatchLogger{
		reportsPath: reportsPath,
		redactor:    red,
	}
}

// LogMismatch performs fail-closed in-memory secret redaction on the record and
// appends the redacted JSON line to shadow_mismatches.jsonl.
func (l *MismatchLogger) LogMismatch(rec MismatchRecord) error {
	if rec.SchemaVersion == "" {
		rec.SchemaVersion = adapterkit.SchemaVersionV1Alpha1
	}
	if rec.UnitID == "" {
		rec.UnitID = "global"
	}
	if rec.ReportType == "" {
		rec.ReportType = string(adapterkit.ReportTypeDiff)
	}
	if rec.Status == "" {
		rec.Status = string(adapterkit.StatusFail)
	}
	if rec.CreatedAt == "" {
		rec.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if rec.ReportID == "" {
		rec.ReportID = fmt.Sprintf("rep-shadow-%s-%d", rec.UnitID, time.Now().UnixNano())
	}

	// 1. Serialize in-memory to raw JSON
	rawBytes, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("shadow: failed to marshal mismatch record: %w", err)
	}

	// 2. Fail-closed in-memory redaction BEFORE writing to disk
	redactedBytes, _, err := l.redactor.RedactJSON(rawBytes)
	if err != nil {
		return fmt.Errorf("shadow: in-memory redaction failed: %w", err)
	}

	// 3. Atomically append under mutex
	l.mu.Lock()
	defer l.mu.Unlock()

	dir := filepath.Dir(l.reportsPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("shadow: failed to create reports directory %s: %w", dir, err)
	}

	f, err := os.OpenFile(l.reportsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("shadow: failed to open %s: %w", l.reportsPath, err)
	}
	defer f.Close()

	if _, err := f.Write(append(redactedBytes, '\n')); err != nil {
		return fmt.Errorf("shadow: failed to write mismatch line: %w", err)
	}

	return nil
}

// ReportsPath returns the path where mismatch lines are written.
func (l *MismatchLogger) ReportsPath() string {
	return l.reportsPath
}
