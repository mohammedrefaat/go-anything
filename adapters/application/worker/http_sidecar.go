package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPConfig defines settings for communicating with a worker over HTTP.
type HTTPConfig struct {
	BaseURL    string
	Client     *http.Client
	Timeout    time.Duration
	TasksPath  string // default "/tasks"
	HealthPath string // default "/healthz"
}

// HTTPSidecar forwards worker tasks to an HTTP worker service.
type HTTPSidecar struct {
	baseURL    string
	client     *http.Client
	timeout    time.Duration
	tasksPath  string
	healthPath string
}

// NewHTTPSidecar creates a new HTTP worker sidecar client.
func NewHTTPSidecar(cfg HTTPConfig) (*HTTPSidecar, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("worker: HTTP BaseURL cannot be empty")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	tasksPath := cfg.TasksPath
	if tasksPath == "" {
		tasksPath = "/tasks"
	}
	healthPath := cfg.HealthPath
	if healthPath == "" {
		healthPath = "/healthz"
	}

	return &HTTPSidecar{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		client:     client,
		timeout:    timeout,
		tasksPath:  tasksPath,
		healthPath: healthPath,
	}, nil
}

// Transport returns TransportHTTP.
func (s *HTTPSidecar) Transport() SidecarTransport {
	return TransportHTTP
}

// Forward sends a task payload to the HTTP worker.
func (s *HTTPSidecar) Forward(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("worker: failed to serialize task: %w", err)
	}

	url := s.baseURL + s.tasksPath
	if task.UnitID != "" {
		url = fmt.Sprintf("%s/%s", url, task.UnitID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("worker: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTaskTimeout
		}
		return nil, fmt.Errorf("worker: HTTP forwarding failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("worker: failed reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &WorkerResult{
			TaskID:       task.ID,
			UnitID:       task.UnitID,
			Status:       "failed",
			ErrorMessage: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBytes)),
			DurationMS:   duration,
		}, nil
	}

	var result WorkerResult
	if err := json.Unmarshal(respBytes, &result); err != nil {
		// If response is not structured WorkerResult, use body directly as Output
		var genericOutput any
		if json.Unmarshal(respBytes, &genericOutput) == nil {
			return &WorkerResult{
				TaskID:     task.ID,
				UnitID:     task.UnitID,
				Status:     "success",
				Output:     genericOutput,
				DurationMS: duration,
			}, nil
		}
		return &WorkerResult{
			TaskID:     task.ID,
			UnitID:     task.UnitID,
			Status:     "success",
			Output:     string(respBytes),
			DurationMS: duration,
		}, nil
	}

	if result.DurationMS == 0 {
		result.DurationMS = duration
	}
	return &result, nil
}

// HealthCheck checks the /healthz endpoint on the HTTP worker.
func (s *HTTPSidecar) HealthCheck(ctx context.Context) (*HealthReport, error) {
	url := s.baseURL + s.healthPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return &HealthReport{
			Status:    HealthStateUnhealthy,
			Transport: TransportHTTP,
			Latency:   latency,
			Message:   err.Error(),
		}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &HealthReport{
			Status:    HealthStateDegraded,
			Transport: TransportHTTP,
			Latency:   latency,
			Message:   fmt.Sprintf("HTTP status %d", resp.StatusCode),
		}, nil
	}

	return &HealthReport{
		Status:    HealthStateHealthy,
		Transport: TransportHTTP,
		Latency:   latency,
		Message:   "HTTP worker healthy",
	}, nil
}

// Close releases any idle connections.
func (s *HTTPSidecar) Close() error {
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
	return nil
}
