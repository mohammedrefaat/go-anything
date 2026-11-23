package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mohammedrefat/go-anything/internal/frontdoor"
)

// ShadowPort abstracts starting, inspecting, and stopping shadow traffic operations.
type ShadowPort interface {
	StartShadow(ctx context.Context, dir, unitID string, port int, sampleRate float64) (*Output, error)
	GetShadowStatus(ctx context.Context, dir, unitID string) (*Output, error)
	StopShadow(ctx context.Context, dir, unitID string) (*Output, error)
}

// shadowStateRecord stores active shadow daemon or listener state under .go-anything/local/.
type shadowStateRecord struct {
	UnitID          string    `json:"unit_id"`
	Active          bool      `json:"active"`
	Port            int       `json:"port"`
	SampleRate      float64   `json:"sample_rate"`
	StartedAt       time.Time `json:"started_at"`
	RequestsTotal   int64     `json:"requests_total"`
	Shadowed        int64     `json:"requests_shadowed"`
	OverflowDropped int64     `json:"overflow_dropped"`
	Mismatches      int64     `json:"mismatches_count"`
}

// DefaultShadowPort implements ShadowPort using local state files and internal/frontdoor.
type DefaultShadowPort struct {
	mu sync.Mutex
}

// NewDefaultShadowPort creates a new DefaultShadowPort.
func NewDefaultShadowPort() *DefaultShadowPort {
	return &DefaultShadowPort{}
}

func shadowFilePath(dir, unitID string) string {
	return filepath.Join(dir, ".go-anything", "local", fmt.Sprintf("%s.shadow.json", unitID))
}

// StartShadow persists active shadow state and ensures the local directory exists.
func (p *DefaultShadowPort) StartShadow(ctx context.Context, dir, unitID string, port int, sampleRate float64) (*Output, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sf := shadowFilePath(dir, unitID)
	if err := os.MkdirAll(filepath.Dir(sf), 0755); err != nil {
		return nil, fmt.Errorf("failed to create local dir: %w", err)
	}

	rec := shadowStateRecord{
		UnitID:     unitID,
		Active:     true,
		Port:       port,
		SampleRate: sampleRate,
		StartedAt:  time.Now().UTC(),
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize shadow state: %w", err)
	}

	if err := os.WriteFile(sf, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write shadow state file: %w", err)
	}

	return &Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "start",
		Active:           true,
		Port:             port,
		SampleRate:       sampleRate,
		RequestsTotal:    0,
		TotalRequests:    0,
		RequestsShadowed: 0,
		Shadowed:         0,
		OverflowDropped:  0,
		MismatchesCount:  0,
		Mismatches:       0,
		Summary:          fmt.Sprintf("Shadow mode started for unit %s on port %d with sample rate %.2f", unitID, port, sampleRate),
	}, nil
}

// GetShadowStatus reads current shadow state from the local store and checks frontdoor.
func (p *DefaultShadowPort) GetShadowStatus(ctx context.Context, dir, unitID string) (*Output, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	recPath := shadowFilePath(dir, unitID)
	data, err := os.ReadFile(recPath)
	if err != nil {
		// Not active or never started
		killActive := frontdoor.IsUnitKillSwitchActive(dir, unitID)
		active := false
		summary := fmt.Sprintf("Shadow mode inactive for unit %s", unitID)
		if killActive {
			summary += " (kill switch active)"
		}
		return &Output{
			SchemaVersion:    "v2",
			UnitID:           unitID,
			Action:           "status",
			Active:           active,
			Port:             0,
			SampleRate:       0.0,
			RequestsTotal:    0,
			TotalRequests:    0,
			RequestsShadowed: 0,
			Shadowed:         0,
			OverflowDropped:  0,
			MismatchesCount:  0,
			Mismatches:       0,
			Summary:          summary,
		}, nil
	}

	var rec shadowStateRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("failed to parse shadow state file: %w", err)
	}

	summary := fmt.Sprintf("Shadow mode active: %d requests, %d shadowed, %d mismatches",
		rec.RequestsTotal, rec.Shadowed, rec.Mismatches)
	if !rec.Active {
		summary = fmt.Sprintf("Shadow mode stopped for unit %s", unitID)
	}

	return &Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "status",
		Active:           rec.Active,
		Port:             rec.Port,
		SampleRate:       rec.SampleRate,
		RequestsTotal:    rec.RequestsTotal,
		TotalRequests:    rec.RequestsTotal,
		RequestsShadowed: rec.Shadowed,
		Shadowed:         rec.Shadowed,
		OverflowDropped:  rec.OverflowDropped,
		MismatchesCount:  rec.Mismatches,
		Mismatches:       rec.Mismatches,
		Summary:          summary,
	}, nil
}

// StopShadow marks the shadow evaluation as inactive.
func (p *DefaultShadowPort) StopShadow(ctx context.Context, dir, unitID string) (*Output, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	recPath := shadowFilePath(dir, unitID)
	var rec shadowStateRecord
	if data, err := os.ReadFile(recPath); err == nil {
		_ = json.Unmarshal(data, &rec)
	}

	rec.UnitID = unitID
	rec.Active = false

	if data, err := json.MarshalIndent(rec, "", "  "); err == nil {
		_ = os.MkdirAll(filepath.Dir(recPath), 0755)
		_ = os.WriteFile(recPath, data, 0644)
	}

	return &Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "stop",
		Active:           false,
		Port:             rec.Port,
		SampleRate:       rec.SampleRate,
		RequestsTotal:    rec.RequestsTotal,
		TotalRequests:    rec.RequestsTotal,
		RequestsShadowed: rec.Shadowed,
		Shadowed:         rec.Shadowed,
		OverflowDropped:  rec.OverflowDropped,
		MismatchesCount:  rec.Mismatches,
		Mismatches:       rec.Mismatches,
		Summary:          fmt.Sprintf("Shadow mode stopped for unit %s", unitID),
	}, nil
}
