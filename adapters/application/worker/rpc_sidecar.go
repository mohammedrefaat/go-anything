package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// RPCConfig defines settings for communicating with a worker over RPC JSON.
type RPCConfig struct {
	Address string
	Timeout time.Duration
}

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RPCSidecar forwards worker tasks to a worker process over RPC JSON (TCP).
type RPCSidecar struct {
	addr    string
	timeout time.Duration
	reqID   uint64
	mu      sync.Mutex
	conn    net.Conn
	closed  bool
}

// NewRPCSidecar creates a new RPC JSON worker sidecar client.
func NewRPCSidecar(cfg RPCConfig) (*RPCSidecar, error) {
	if cfg.Address == "" {
		return nil, errors.New("worker: RPC Address cannot be empty")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &RPCSidecar{
		addr:    cfg.Address,
		timeout: timeout,
	}, nil
}

// Transport returns TransportRPC.
func (s *RPCSidecar) Transport() SidecarTransport {
	return TransportRPC
}

func (s *RPCSidecar) getConnLocked(ctx context.Context) (net.Conn, error) {
	if s.closed {
		return nil, ErrSidecarClosed
	}
	if s.conn != nil {
		return s.conn, nil
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("worker: RPC dial failed to %s: %w", s.addr, err)
	}
	s.conn = conn
	return conn, nil
}

// Forward sends a task over RPC JSON to the worker.
func (s *RPCSidecar) Forward(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, err := s.getConnLocked(ctx)
	if err != nil {
		return nil, err
	}

	id := atomic.AddUint64(&s.reqID, 1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "Worker.Execute",
		Params:  task,
	}

	data, err := json.Marshal(req)
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
		return nil, fmt.Errorf("worker: RPC write failed: %w", err)
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
		return nil, fmt.Errorf("worker: RPC read failed: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("worker: invalid RPC JSON response: %w", err)
	}

	if resp.Error != nil {
		return &WorkerResult{
			TaskID:       task.ID,
			UnitID:       task.UnitID,
			Status:       "failed",
			ErrorMessage: resp.Error.Message,
			DurationMS:   duration,
		}, nil
	}

	var result WorkerResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		var generic any
		_ = json.Unmarshal(resp.Result, &generic)
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

// HealthCheck performs an RPC ping to probe worker responsiveness.
func (s *RPCSidecar) HealthCheck(ctx context.Context) (*HealthReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, err := s.getConnLocked(ctx)
	if err != nil {
		return &HealthReport{
			Status:    HealthStateUnhealthy,
			Transport: TransportRPC,
			Message:   err.Error(),
		}, err
	}

	id := atomic.AddUint64(&s.reqID, 1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "Worker.Ping",
		Params:  map[string]any{},
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')

	start := time.Now()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(data); err != nil {
		_ = conn.Close()
		s.conn = nil
		latency := time.Since(start)
		return &HealthReport{
			Status:    HealthStateUnhealthy,
			Transport: TransportRPC,
			Latency:   latency,
			Message:   err.Error(),
		}, err
	}

	reader := bufio.NewReader(conn)
	_, err = reader.ReadBytes('\n')
	latency := time.Since(start)
	if err != nil {
		_ = conn.Close()
		s.conn = nil
		return &HealthReport{
			Status:    HealthStateUnhealthy,
			Transport: TransportRPC,
			Latency:   latency,
			Message:   err.Error(),
		}, err
	}

	return &HealthReport{
		Status:    HealthStateHealthy,
		Transport: TransportRPC,
		Latency:   latency,
		Message:   "RPC worker healthy",
	}, nil
}

// Close closes the underlying TCP connection.
func (s *RPCSidecar) Close() error {
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

// RPCServerHelper runs a lightweight JSON-RPC TCP worker server for hosting or testing.
type RPCServerHelper struct {
	listener net.Listener
	handler  WorkerHandler
	closed   bool
	mu       sync.Mutex
}

// StartRPCServerHelper creates and starts an RPC server on listener.
func StartRPCServerHelper(listener net.Listener, handler WorkerHandler) *RPCServerHelper {
	srv := &RPCServerHelper{
		listener: listener,
		handler:  handler,
	}
	go srv.serve()
	return srv
}

func (s *RPCServerHelper) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *RPCServerHelper) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		var req jsonrpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		var resp jsonrpcResponse
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "Worker.Ping":
			resp.Result = json.RawMessage(`{"status":"pong"}`)
		case "Worker.Execute":
			var task WorkerTask
			taskBytes, _ := json.Marshal(req.Params)
			_ = json.Unmarshal(taskBytes, &task)

			res, err := s.handler(context.Background(), task)
			if err != nil {
				resp.Error = &jsonrpcError{Code: -32000, Message: err.Error()}
			} else {
				outBytes, _ := json.Marshal(res)
				resp.Result = outBytes
			}
		default:
			resp.Error = &jsonrpcError{Code: -32601, Message: "Method not found"}
		}

		outLine, _ := json.Marshal(resp)
		outLine = append(outLine, '\n')
		_, _ = conn.Write(outLine)
	}
}

// Close stops the RPC server helper.
func (s *RPCServerHelper) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
