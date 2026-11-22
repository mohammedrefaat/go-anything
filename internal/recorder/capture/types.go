package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

var (
	// ErrRoutingControlForbidden is returned when an observability or capture payload
	// attempts to specify or mutate routing. Observability sources are strictly read-only.
	ErrRoutingControlForbidden = errors.New("capture: observability source is read-only; routing control operations are forbidden")

	// ErrEmptyUnitID is returned when an ingestion call does not specify a target unit ID.
	ErrEmptyUnitID = errors.New("capture: unit ID cannot be empty")

	// ErrNilTraffic is returned when an ingestion payload is nil or empty.
	ErrNilTraffic = errors.New("capture: traffic payload cannot be nil")
)

// HTTPRequest represents an ingested HTTP request from gateway/sidecar mirroring.
type HTTPRequest struct {
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       any               `json:"body,omitempty"`
	RemoteAddr string            `json:"remote_addr,omitempty"`
}

// HTTPResponse represents an observed HTTP response associated with a mirrored request.
type HTTPResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       any               `json:"body,omitempty"`
	DurationMS float64           `json:"duration_ms,omitempty"`
}

// HTTPTraffic encapsulates an observed HTTP request/response exchange.
type HTTPTraffic struct {
	Request  HTTPRequest    `json:"request"`
	Response HTTPResponse   `json:"response"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// BrokerMessage represents an ingested message from a message broker consumer.
type BrokerMessage struct {
	Topic           string            `json:"topic"`
	Key             string            `json:"key,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Payload         any               `json:"payload"`
	ResponsePayload any               `json:"response_payload,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	StatusCode      int               `json:"status_code,omitempty"`
	Metadata        map[string]any    `json:"metadata,omitempty"`
}

// TraceEvent represents an annotated event within a distributed trace span.
type TraceEvent struct {
	Name       string         `json:"name"`
	Timestamp  time.Time      `json:"timestamp"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// TraceSpan represents an ingested distributed trace span (e.g. OpenTelemetry, Jaeger, W3C).
type TraceSpan struct {
	TraceID      string         `json:"trace_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind,omitempty"` // SERVER, CLIENT, CONSUMER, PRODUCER
	StartTime    time.Time      `json:"start_time"`
	EndTime      time.Time      `json:"end_time"`
	Attributes   map[string]any `json:"attributes,omitempty"`
	Events       []TraceEvent   `json:"events,omitempty"`
	HTTP         *HTTPTraffic   `json:"http,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// DecodedTraffic represents a decoded traffic record supplied by a read-only observability tool (e.g. platform-mcp).
type DecodedTraffic struct {
	Source    string         `json:"source"`             // e.g. "platform-mcp"
	Operation string         `json:"operation"`          // e.g. "auth.login", "GET /users"
	Protocol  string         `json:"protocol,omitempty"` // "http", "grpc", "message"
	Input     any            `json:"input"`              // decoded request/arguments
	Output    any            `json:"output"`             // decoded response/result
	Metadata  map[string]any `json:"metadata,omitempty"` // arbitrary metadata (strictly read-only)
}

// CaseSink defines the persistence destination for captured and redacted cases.
type CaseSink interface {
	SaveCase(unitID string, c *adapterkit.Case) error
}

// FileSink writes captured cases to .go-anything/cases/<unit_id>.jsonl files.
type FileSink struct {
	casesDir string
	mu       sync.Mutex
}

// NewFileSink creates a FileSink writing to the specified cases directory.
func NewFileSink(casesDir string) *FileSink {
	return &FileSink{casesDir: casesDir}
}

// caseFilename returns a local path relative to the configured cases directory.
func caseFilename(unitID string) (string, error) {
	relUnit := filepath.Clean(filepath.FromSlash(unitID))
	if !filepath.IsLocal(relUnit) || strings.HasPrefix(relUnit, "..") || relUnit == "." {
		return "", fmt.Errorf("invalid unit ID %q: path traversal forbidden", unitID)
	}
	return relUnit + ".jsonl", nil
}

func (f *FileSink) openRoot() (*os.Root, error) {
	// The configured parent is trusted; the cases directory itself must be a
	// real directory. Verify that the opened directory matches its entry.
	dir := filepath.Clean(f.casesDir)
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cases path is not a directory: %s", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("cases directory changed while opening: %s", dir)
	}
	return root, nil
}

func (f *FileSink) readCases(unitID string) ([]byte, error) {
	name, err := caseFilename(unitID)
	if err != nil {
		return nil, err
	}
	root, err := f.openRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(name)
}

// SaveCase appends a case to .go-anything/cases/<unit_id>.jsonl atomically and flushes to disk.
func (f *FileSink) SaveCase(unitID string, c *adapterkit.Case) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	name, err := caseFilename(unitID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal case: %w", err)
	}
	if err := os.MkdirAll(filepath.Clean(f.casesDir), 0755); err != nil {
		return fmt.Errorf("failed to create cases directory: %w", err)
	}
	root, err := f.openRoot()
	if err != nil {
		return fmt.Errorf("failed to open cases directory: %w", err)
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return fmt.Errorf("failed to create case subdirectory: %w", err)
	}

	file, err := root.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open case file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write case to %s: %w", name, err)
	}
	return file.Sync()
}

// MemorySink holds captured cases in memory for inspection and testing.
type MemorySink struct {
	mu    sync.RWMutex
	cases map[string][]*adapterkit.Case
}

// NewMemorySink creates a new in-memory CaseSink.
func NewMemorySink() *MemorySink {
	return &MemorySink{
		cases: make(map[string][]*adapterkit.Case),
	}
}

// SaveCase records a case in memory.
func (m *MemorySink) SaveCase(unitID string, c *adapterkit.Case) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cases[unitID] = append(m.cases[unitID], c)
	return nil
}

// CasesForUnit returns all stored cases for a given unit.
func (m *MemorySink) CasesForUnit(unitID string) []*adapterkit.Case {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stored := m.cases[unitID]
	res := make([]*adapterkit.Case, len(stored))
	copy(res, stored)
	return res
}

// AllCases returns all stored cases across all units.
func (m *MemorySink) AllCases() []*adapterkit.Case {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []*adapterkit.Case
	for _, cases := range m.cases {
		res = append(res, cases...)
	}
	return res
}
