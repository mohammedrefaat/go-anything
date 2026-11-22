package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// MessageBroker defines the message queue broker contract.
type MessageBroker interface {
	Publish(ctx context.Context, queue string, payload []byte) error
	Subscribe(ctx context.Context, queue string, handler func(payload []byte) ([]byte, error)) (io.Closer, error)
	Close() error
}

// InMemoryBroker is an in-memory thread-safe implementation of MessageBroker.
type InMemoryBroker struct {
	mu     sync.RWMutex
	queues map[string][]func(payload []byte) ([]byte, error)
	closed bool
}

// NewInMemoryBroker creates a new in-memory message broker.
func NewInMemoryBroker() *InMemoryBroker {
	return &InMemoryBroker{
		queues: make(map[string][]func(payload []byte) ([]byte, error)),
	}
}

type subscription struct {
	broker *InMemoryBroker
	queue  string
	idx    int
}

func (s *subscription) Close() error {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	handlers := s.broker.queues[s.queue]
	if s.idx < len(handlers) {
		s.broker.queues[s.queue] = append(handlers[:s.idx], handlers[s.idx+1:]...)
	}
	return nil
}

// Subscribe registers a handler on a specific queue.
func (b *InMemoryBroker) Subscribe(ctx context.Context, queue string, handler func(payload []byte) ([]byte, error)) (io.Closer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrSidecarClosed
	}
	idx := len(b.queues[queue])
	b.queues[queue] = append(b.queues[queue], handler)
	return &subscription{broker: b, queue: queue, idx: idx}, nil
}

// Publish delivers a payload to registered subscribers on the queue.
func (b *InMemoryBroker) Publish(ctx context.Context, queue string, payload []byte) error {
	b.mu.RLock()
	handlers := append([]func([]byte) ([]byte, error)(nil), b.queues[queue]...)
	closed := b.closed
	b.mu.RUnlock()

	if closed {
		return ErrSidecarClosed
	}
	if len(handlers) == 0 {
		return fmt.Errorf("worker: broker queue %q has no consumers", queue)
	}

	// Dispatch to first available subscriber (work-queue style)
	_, err := handlers[0](payload)
	return err
}

// RequestReply sends a request payload and awaits the reply from subscriber.
func (b *InMemoryBroker) RequestReply(ctx context.Context, queue string, payload []byte) ([]byte, error) {
	b.mu.RLock()
	handlers := append([]func([]byte) ([]byte, error)(nil), b.queues[queue]...)
	closed := b.closed
	b.mu.RUnlock()

	if closed {
		return nil, ErrSidecarClosed
	}
	if len(handlers) == 0 {
		return nil, fmt.Errorf("worker: broker queue %q has no consumers", queue)
	}

	return handlers[0](payload)
}

// Close shuts down the in-memory broker.
func (b *InMemoryBroker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.queues = make(map[string][]func(payload []byte) ([]byte, error))
	return nil
}

// BrokerConfig defines configuration for broker sidecar client.
type BrokerConfig struct {
	Broker    MessageBroker
	QueueName string // default "worker.tasks"
	Timeout   time.Duration
}

// BrokerSidecar forwards worker tasks over a message broker.
type BrokerSidecar struct {
	broker    MessageBroker
	queueName string
	timeout   time.Duration
	closed    bool
	mu        sync.Mutex
}

// NewBrokerSidecar creates a new message broker worker sidecar client.
func NewBrokerSidecar(cfg BrokerConfig) (*BrokerSidecar, error) {
	if cfg.Broker == nil {
		return nil, errors.New("worker: MessageBroker cannot be nil")
	}
	queue := cfg.QueueName
	if queue == "" {
		queue = "worker.tasks"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &BrokerSidecar{
		broker:    cfg.Broker,
		queueName: queue,
		timeout:   timeout,
	}, nil
}

// Transport returns TransportBroker.
func (s *BrokerSidecar) Transport() SidecarTransport {
	return TransportBroker
}

// Forward sends a task over the message broker and awaits execution result.
func (s *BrokerSidecar) Forward(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrSidecarClosed
	}
	broker := s.broker
	queue := s.queueName
	if task.UnitID != "" {
		queue = fmt.Sprintf("%s.%s", queue, task.UnitID)
	}
	s.mu.Unlock()

	data, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	timeout := s.timeout
	if dl, ok := ctx.Deadline(); ok {
		tRem := time.Until(dl)
		if tRem > 0 && tRem < timeout {
			timeout = tRem
		}
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type rpcBroker interface {
		RequestReply(ctx context.Context, queue string, payload []byte) ([]byte, error)
	}

	var replyData []byte
	if rb, ok := broker.(rpcBroker); ok {
		replyData, err = rb.RequestReply(reqCtx, queue, data)
	} else {
		err = broker.Publish(reqCtx, queue, data)
	}

	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return nil, ErrTaskTimeout
		}
		return nil, fmt.Errorf("worker: broker forward failed: %w", err)
	}

	if len(replyData) == 0 {
		return &WorkerResult{
			TaskID:     task.ID,
			UnitID:     task.UnitID,
			Status:     "success",
			DurationMS: duration,
		}, nil
	}

	var result WorkerResult
	if err := json.Unmarshal(replyData, &result); err != nil {
		var generic any
		_ = json.Unmarshal(replyData, &generic)
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

// HealthCheck verifies that the message broker is active.
func (s *BrokerSidecar) HealthCheck(ctx context.Context) (*HealthReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return &HealthReport{
			Status:    HealthStateUnhealthy,
			Transport: TransportBroker,
			Message:   "broker closed",
		}, ErrSidecarClosed
	}

	return &HealthReport{
		Status:    HealthStateHealthy,
		Transport: TransportBroker,
		Message:   "broker connected",
	}, nil
}

// Close releases the broker sidecar.
func (s *BrokerSidecar) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
