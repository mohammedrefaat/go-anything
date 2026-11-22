package worker

import (
	"context"
	"time"
)

// UnitMode determines how an individual worker unit is routed during migration.
type UnitMode string

const (
	// ModeLegacy routes worker tasks to the original foreign worker sidecar.
	ModeLegacy UnitMode = "old"

	// ModeGo routes worker tasks to the migrated Go worker implementation.
	ModeGo UnitMode = "new"

	// ModeShadow routes to legacy sidecar while asynchronously executing the Go worker.
	ModeShadow UnitMode = "shadow"
)

// SidecarTransport identifies the communication transport between Go sidecar and workers.
type SidecarTransport string

const (
	TransportHTTP   SidecarTransport = "http"
	TransportRPC    SidecarTransport = "rpc"
	TransportSocket SidecarTransport = "socket"
	TransportStdio  SidecarTransport = "stdio"
	TransportBroker SidecarTransport = "broker"
)

// WorkerTask represents a job or message delivered to a worker unit.
type WorkerTask struct {
	ID        string            `json:"id"`
	UnitID    string            `json:"unit_id"`
	Name      string            `json:"name,omitempty"`
	Payload   any               `json:"payload"`
	Headers   map[string]string `json:"headers,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
}

// WorkerResult represents the execution outcome of a worker task.
type WorkerResult struct {
	TaskID       string            `json:"task_id"`
	UnitID       string            `json:"unit_id"`
	Status       string            `json:"status"` // "success", "failed", "retried"
	Output       any               `json:"output,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	DurationMS   float64           `json:"duration_ms,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// WorkerHandler executes a task in-process (for migrated Go worker units).
type WorkerHandler func(ctx context.Context, task WorkerTask) (*WorkerResult, error)

// HealthState represents the health status of a worker or sidecar connection.
type HealthState string

const (
	HealthStateHealthy   HealthState = "healthy"
	HealthStateDegraded  HealthState = "degraded"
	HealthStateUnhealthy HealthState = "unhealthy"
)

// HealthReport provides health status diagnostics for a sidecar transport.
type HealthReport struct {
	Status    HealthState      `json:"status"`
	Transport SidecarTransport `json:"transport"`
	Latency   time.Duration    `json:"latency"`
	Message   string           `json:"message,omitempty"`
	Details   map[string]any   `json:"details,omitempty"`
}
