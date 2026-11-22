package worker

import (
	"context"
	"errors"
)

var (
	// ErrSidecarClosed indicates the sidecar connection or server is closed.
	ErrSidecarClosed = errors.New("worker: sidecar client is closed")

	// ErrTaskTimeout indicates a worker task exceeded the specified timeout.
	ErrTaskTimeout = errors.New("worker: task execution timed out")

	// ErrWorkerUnhealthy indicates the sidecar endpoint is not healthy.
	ErrWorkerUnhealthy = errors.New("worker: sidecar health check failed")

	// ErrUnitNotConfigured indicates no legacy sidecar or Go handler exists for the unit.
	ErrUnitNotConfigured = errors.New("worker: unit not configured")
)

// SidecarClient connects to a background worker or service process over a specific transport.
type SidecarClient interface {
	// Transport returns the transport type of this client.
	Transport() SidecarTransport

	// Forward forwards a task payload to the worker and returns the execution result.
	Forward(ctx context.Context, task WorkerTask) (*WorkerResult, error)

	// HealthCheck performs an active health probe against the worker endpoint.
	HealthCheck(ctx context.Context) (*HealthReport, error)

	// Close releases network connections, subprocesses, or open channels.
	Close() error
}
