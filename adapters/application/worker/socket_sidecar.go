package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// SocketConfig defines configuration for socket/IPC worker communication.
type SocketConfig struct {
	Network string // "unix" (default) or "tcp"
	Address string // socket path or host:port
	Timeout time.Duration
}

// SocketSidecar forwards worker tasks over a Unix domain socket or named socket.
type SocketSidecar struct {
	network string
	address string
	timeout time.Duration
	mu      sync.Mutex
	conn    net.Conn
	closed  bool
}

// NewSocketSidecar creates a new socket worker sidecar client.
func NewSocketSidecar(cfg SocketConfig) (*SocketSidecar, error) {
	if cfg.Address == "" {
		return nil, errors.New("worker: socket Address cannot be empty")
	}
	network := cfg.Network
	if network == "" {
		network = "unix"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &SocketSidecar{
		network: network,
		address: cfg.Address,
		timeout: timeout,
	}, nil
}

// Transport returns TransportSocket.
func (s *SocketSidecar) Transport() SidecarTransport {
	return TransportSocket
}

func (s *SocketSidecar) getConnLocked(ctx context.Context) (net.Conn, error) {
	if s.closed {
		return nil, ErrSidecarClosed
	}
	if s.conn != nil {
		return s.conn, nil
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, s.network, s.address)
	if err != nil {
		return nil, fmt.Errorf("worker: socket dial failed (%s %s): %w", s.network, s.address, err)
	}
	s.conn = conn
	return conn, nil
}

// Forward sends a task over socket to the worker.
func (s *SocketSidecar) Forward(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, err := s.getConnLocked(ctx)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	deadline := time.Now().Add(s.timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	start := time.Now()
	if _, err := conn.Write(data); err != nil {
		_ = conn.Close()
		s.conn = nil
		return nil, fmt.Errorf("worker: socket write failed: %w", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		_ = conn.Close()
		s.conn = nil
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTaskTimeout
		}
		return nil, fmt.Errorf("worker: socket read failed: %w", err)
	}

	var result WorkerResult
	if err := json.Unmarshal(line, &result); err != nil {
		var generic any
		_ = json.Unmarshal(line, &generic)
		return &WorkerResult{
			TaskID:     task.ID,
			UnitID:     task.UnitID,
			Status:     "success",
			Output:     generic,
			DurationMS: duration,
		}, nil
	}

	if result.DurationMS == 0 {
		result.DurationMS = duration
	}
	return &result, nil
}

// HealthCheck checks the socket connection by dialing or sending a ping.
func (s *SocketSidecar) HealthCheck(ctx context.Context) (*HealthReport, error) {
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, s.network, s.address)
	latency := time.Since(start)

	if err != nil {
		return &HealthReport{
			Status:    HealthStateUnhealthy,
			Transport: TransportSocket,
			Latency:   latency,
			Message:   err.Error(),
		}, err
	}
	_ = conn.Close()

	return &HealthReport{
		Status:    HealthStateHealthy,
		Transport: TransportSocket,
		Latency:   latency,
		Message:   "Socket worker connected",
	}, nil
}

// Close closes the underlying socket connection.
func (s *SocketSidecar) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn != nil {
		err := s.conn.Close()
		s.conn = nil
		return err
	}
	return nil
}

// SocketServerHelper runs a framed JSON worker server listening on network/address.
type SocketServerHelper struct {
	listener net.Listener
	handler  WorkerHandler
	closed   bool
	mu       sync.Mutex
}

// StartSocketServerHelper starts a framed JSON socket server.
func StartSocketServerHelper(listener net.Listener, handler WorkerHandler) *SocketServerHelper {
	srv := &SocketServerHelper{
		listener: listener,
		handler:  handler,
	}
	go srv.serve()
	return srv
}

func (s *SocketServerHelper) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *SocketServerHelper) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		var task WorkerTask
		if err := json.Unmarshal(line, &task); err != nil {
			continue
		}

		res, err := s.handler(context.Background(), task)
		var outBytes []byte
		if err != nil {
			res = &WorkerResult{
				TaskID:       task.ID,
				UnitID:       task.UnitID,
				Status:       "failed",
				ErrorMessage: err.Error(),
			}
		}
		outBytes, _ = json.Marshal(res)
		outBytes = append(outBytes, '\n')
		_, _ = conn.Write(outBytes)
	}
}

// Close stops the socket server.
func (s *SocketServerHelper) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
