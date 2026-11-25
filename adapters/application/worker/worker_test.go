package worker

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelperProcess implements the stdio worker subprocess helper.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_WORKER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var task WorkerTask
		if err := dec.Decode(&task); err != nil {
			break
		}

		res := WorkerResult{
			TaskID: task.ID,
			UnitID: task.UnitID,
			Status: "success",
			Output: map[string]any{
				"echo":   task.Payload,
				"worker": "stdio_legacy",
			},
		}
		if err := enc.Encode(res); err != nil {
			break
		}
	}
}

func TestHTTPSidecar(t *testing.T) {
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
			return
		}

		body, _ := io.ReadAll(r.Body)
		var task WorkerTask
		_ = json.Unmarshal(body, &task)

		res := WorkerResult{
			TaskID: task.ID,
			UnitID: task.UnitID,
			Status: "success",
			Output: map[string]any{
				"echo":   task.Payload,
				"worker": "http_legacy",
			},
		}
		out, _ := json.Marshal(res)
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}))
	defer ts.Close()

	sidecar, err := NewHTTPSidecar(HTTPConfig{
		BaseURL: ts.URL,
	})
	require.NoError(t, err)
	defer sidecar.Close()

	assert.Equal(t, TransportHTTP, sidecar.Transport())

	// HealthCheck
	hr, err := sidecar.HealthCheck(ctx)
	require.NoError(t, err)
	assert.Equal(t, HealthStateHealthy, hr.Status)

	// Forward
	task := WorkerTask{
		ID:      "t-1",
		UnitID:  "email_worker",
		Payload: map[string]any{"to": "user@example.com"},
	}
	res, err := sidecar.Forward(ctx, task)
	require.NoError(t, err)
	assert.Equal(t, "success", res.Status)
	assert.Equal(t, "http_legacy", res.Output.(map[string]any)["worker"])
}

func TestRPCSidecar(t *testing.T) {
	ctx := context.Background()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	handler := func(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
		return &WorkerResult{
			TaskID: task.ID,
			UnitID: task.UnitID,
			Status: "success",
			Output: map[string]any{
				"processed": true,
				"worker":    "rpc_legacy",
			},
		}, nil
	}

	srv := StartRPCServerHelper(l, handler)
	defer srv.Close()

	sidecar, err := NewRPCSidecar(RPCConfig{
		Address: l.Addr().String(),
	})
	require.NoError(t, err)
	defer sidecar.Close()

	assert.Equal(t, TransportRPC, sidecar.Transport())

	// Health check
	hr, err := sidecar.HealthCheck(ctx)
	require.NoError(t, err)
	assert.Equal(t, HealthStateHealthy, hr.Status)

	// Forward
	task := WorkerTask{
		ID:      "rpc-1",
		UnitID:  "payment_service",
		Payload: map[string]any{"amount": 100},
	}
	res, err := sidecar.Forward(ctx, task)
	require.NoError(t, err)
	assert.Equal(t, "success", res.Status)
	assert.Equal(t, "rpc_legacy", res.Output.(map[string]any)["worker"])
}

func TestSocketSidecar(t *testing.T) {
	ctx := context.Background()

	// Use loopback TCP for guaranteed cross-platform portability in socket tests
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	handler := func(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
		return &WorkerResult{
			TaskID: task.ID,
			UnitID: task.UnitID,
			Status: "success",
			Output: map[string]any{
				"socket_received": true,
				"worker":          "socket_legacy",
			},
		}, nil
	}

	srv := StartSocketServerHelper(l, handler)
	defer srv.Close()

	sidecar, err := NewSocketSidecar(SocketConfig{
		Network: "tcp",
		Address: l.Addr().String(),
	})
	require.NoError(t, err)
	defer sidecar.Close()

	assert.Equal(t, TransportSocket, sidecar.Transport())

	// Health check
	hr, err := sidecar.HealthCheck(ctx)
	require.NoError(t, err)
	assert.Equal(t, HealthStateHealthy, hr.Status)

	// Forward
	task := WorkerTask{
		ID:      "sock-1",
		UnitID:  "telemetry_worker",
		Payload: map[string]any{"metric": "cpu"},
	}
	res, err := sidecar.Forward(ctx, task)
	require.NoError(t, err)
	assert.Equal(t, "success", res.Status)
	assert.Equal(t, "socket_legacy", res.Output.(map[string]any)["worker"])
}

func TestStdioSidecar(t *testing.T) {
	ctx := context.Background()

	sidecar, err := NewStdioSidecar(StdioConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env: map[string]string{
			"GO_WANT_WORKER_PROCESS": "1",
		},
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer sidecar.Close()

	assert.Equal(t, TransportStdio, sidecar.Transport())

	// HealthCheck
	hr, err := sidecar.HealthCheck(ctx)
	require.NoError(t, err)
	assert.Equal(t, HealthStateHealthy, hr.Status)

	// Forward
	task := WorkerTask{
		ID:      "stdio-1",
		UnitID:  "parser_worker",
		Payload: map[string]any{"file": "data.csv"},
	}
	res, err := sidecar.Forward(ctx, task)
	require.NoError(t, err)
	assert.Equal(t, "success", res.Status)
	assert.Equal(t, "stdio_legacy", res.Output.(map[string]any)["worker"])
}

func TestBrokerSidecar(t *testing.T) {
	ctx := context.Background()

	broker := NewInMemoryBroker()
	defer broker.Close()

	// Register subscriber for worker queue
	_, err := broker.Subscribe(ctx, "tasks.queue_worker", func(payload []byte) ([]byte, error) {
		var task WorkerTask
		_ = json.Unmarshal(payload, &task)
		res := WorkerResult{
			TaskID: task.ID,
			UnitID: task.UnitID,
			Status: "success",
			Output: map[string]any{
				"queued": true,
				"worker": "broker_legacy",
			},
		}
		return json.Marshal(res)
	})
	require.NoError(t, err)

	sidecar, err := NewBrokerSidecar(BrokerConfig{
		Broker:    broker,
		QueueName: "tasks",
	})
	require.NoError(t, err)
	defer sidecar.Close()

	assert.Equal(t, TransportBroker, sidecar.Transport())

	// Health check
	hr, err := sidecar.HealthCheck(ctx)
	require.NoError(t, err)
	assert.Equal(t, HealthStateHealthy, hr.Status)

	// Forward
	task := WorkerTask{
		ID:      "broker-1",
		UnitID:  "queue_worker",
		Payload: map[string]any{"job": "reindex"},
	}
	res, err := sidecar.Forward(ctx, task)
	require.NoError(t, err)
	assert.Equal(t, "success", res.Status)
	assert.Equal(t, "broker_legacy", res.Output.(map[string]any)["worker"])
}

func TestSidecar_GracefulStartupShutdown(t *testing.T) {
	ctx := context.Background()

	sidecar := NewSidecar("test-cluster")
	err := sidecar.Start(ctx)
	require.NoError(t, err)

	var activeCount int64
	slowHandler := func(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
		atomic.AddInt64(&activeCount, 1)
		defer atomic.AddInt64(&activeCount, -1)
		time.Sleep(100 * time.Millisecond)
		return &WorkerResult{
			TaskID: task.ID,
			Status: "success",
		}, nil
	}
	sidecar.RegisterGo("slow_unit", slowHandler)
	sidecar.SetUnitMode("slow_unit", ModeGo)

	// Launch async task
	go func() {
		_, _ = sidecar.Forward(ctx, WorkerTask{ID: "task-slow", UnitID: "slow_unit"})
	}()

	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, int64(1), atomic.LoadInt64(&activeCount))

	// Graceful shutdown waits for task to drain
	shutdownCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	err = sidecar.Shutdown(shutdownCtx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), atomic.LoadInt64(&activeCount))
}

// TestWorker_StranglerUnitByUnit proves migrating background worker units one unit at a time.
func TestWorker_StranglerUnitByUnit(t *testing.T) {
	ctx := context.Background()

	// 1. Create a mock legacy worker HTTP service representing the legacy Python worker
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var task WorkerTask
		_ = json.Unmarshal(body, &task)

		output := make(map[string]any)
		switch task.UnitID {
		case "email_notifier":
			output["email_sent"] = true
			output["engine"] = "legacy_python_celery"
		case "report_generator":
			output["report_url"] = "https://s3.example.com/reports/123.pdf"
			output["engine"] = "legacy_python_celery"
		case "image_processor":
			output["thumbnail_url"] = "https://cdn.example.com/thumbs/123.jpg"
			output["engine"] = "legacy_python_celery"
		default:
			output["engine"] = "legacy_unknown"
		}

		res := WorkerResult{
			TaskID: task.ID,
			UnitID: task.UnitID,
			Status: "success",
			Output: output,
		}
		out, _ := json.Marshal(res)
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}))
	defer legacyServer.Close()

	legacySidecar, err := NewHTTPSidecar(HTTPConfig{
		BaseURL: legacyServer.URL,
	})
	require.NoError(t, err)
	defer legacySidecar.Close()

	// 2. Initialize Go Sidecar orchestrator
	sidecar := NewSidecar("prod-strangler-sidecar")
	defer sidecar.Close()

	err = sidecar.Start(ctx)
	require.NoError(t, err)

	// Register all 3 units initially as legacy
	sidecar.RegisterLegacy("email_notifier", legacySidecar)
	sidecar.RegisterLegacy("report_generator", legacySidecar)
	sidecar.RegisterLegacy("image_processor", legacySidecar)

	// Step 1: Verify all 3 units execute via legacy sidecar
	t1 := WorkerTask{ID: "w-1", UnitID: "email_notifier", Payload: map[string]any{"to": "alice@corp.com"}}
	r1, err := sidecar.Forward(ctx, t1)
	require.NoError(t, err)
	assert.Equal(t, "legacy_python_celery", r1.Output.(map[string]any)["engine"])

	t2 := WorkerTask{ID: "w-2", UnitID: "report_generator", Payload: map[string]any{"month": "nov"}}
	r2, err := sidecar.Forward(ctx, t2)
	require.NoError(t, err)
	assert.Equal(t, "legacy_python_celery", r2.Output.(map[string]any)["engine"])

	t3 := WorkerTask{ID: "w-3", UnitID: "image_processor", Payload: map[string]any{"image_id": "img-99"}}
	r3, err := sidecar.Forward(ctx, t3)
	require.NoError(t, err)
	assert.Equal(t, "legacy_python_celery", r3.Output.(map[string]any)["engine"])

	// Step 2: Migrate Unit 1 ("email_notifier") to native Go worker handler
	goEmailWorker := func(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
		return &WorkerResult{
			TaskID: task.ID,
			UnitID: task.UnitID,
			Status: "success",
			Output: map[string]any{
				"email_sent": true,
				"engine":     "go_modern_sidecar",
			},
		}, nil
	}
	sidecar.MigrateUnit("email_notifier", goEmailWorker)

	// Step 3: Verify email_notifier executes in Go, while units 2 and 3 still forward to legacy!
	r1Migrated, err := sidecar.Forward(ctx, t1)
	require.NoError(t, err)
	assert.Equal(t, "go_modern_sidecar", r1Migrated.Output.(map[string]any)["engine"])

	r2StillLegacy, err := sidecar.Forward(ctx, t2)
	require.NoError(t, err)
	assert.Equal(t, "legacy_python_celery", r2StillLegacy.Output.(map[string]any)["engine"])

	r3StillLegacy, err := sidecar.Forward(ctx, t3)
	require.NoError(t, err)
	assert.Equal(t, "legacy_python_celery", r3StillLegacy.Output.(map[string]any)["engine"])

	// Step 4: Test per-unit kill switch fallback: GO_ANYTHING_UNIT_EMAIL_NOTIFIER=original
	t.Setenv("GO_ANYTHING_UNIT_EMAIL_NOTIFIER", "original")
	assert.Equal(t, ModeLegacy, sidecar.GetEffectiveMode("email_notifier"))

	r1Fallback, err := sidecar.Forward(ctx, t1)
	require.NoError(t, err)
	assert.Equal(t, "legacy_python_celery", r1Fallback.Output.(map[string]any)["engine"])
	assert.Equal(t, int64(1), sidecar.FallbackCount("email_notifier"))

	// Clear kill switch override
	t.Setenv("GO_ANYTHING_UNIT_EMAIL_NOTIFIER", "")
	assert.Equal(t, ModeGo, sidecar.GetEffectiveMode("email_notifier"))

	// Step 5: Migrate Unit 2 ("report_generator") to Go worker handler
	goReportWorker := func(ctx context.Context, task WorkerTask) (*WorkerResult, error) {
		return &WorkerResult{
			TaskID: task.ID,
			UnitID: task.UnitID,
			Status: "success",
			Output: map[string]any{
				"report_url": "https://s3.example.com/modern-reports/123.pdf",
				"engine":     "go_modern_sidecar",
			},
		}, nil
	}
	sidecar.MigrateUnit("report_generator", goReportWorker)

	// Verify units 1 and 2 run Go modern worker, and unit 3 runs legacy
	r1Final, _ := sidecar.Forward(ctx, t1)
	assert.Equal(t, "go_modern_sidecar", r1Final.Output.(map[string]any)["engine"])

	r2Final, _ := sidecar.Forward(ctx, t2)
	assert.Equal(t, "go_modern_sidecar", r2Final.Output.(map[string]any)["engine"])

	r3Final, _ := sidecar.Forward(ctx, t3)
	assert.Equal(t, "legacy_python_celery", r3Final.Output.(map[string]any)["engine"])

	// Step 6: Verify replay interface with adapterkit.Case
	replayCase := &adapterkit.Case{
		CaseID: "case-replay-1",
		UnitID: "email_notifier",
		Input: adapterkit.CaseInput{
			Body: map[string]any{"to": "verify@test.com"},
		},
	}
	caseOut, err := sidecar.ExecuteCase(ctx, replayCase)
	require.NoError(t, err)
	assert.Equal(t, 0, caseOut.ExitCode)
	require.NotNil(t, caseOut.Body)
	assert.Equal(t, "go_modern_sidecar", caseOut.Body.(map[string]any)["engine"])

	// Step 7: Check aggregate health diagnostics
	healthReports := sidecar.HealthCheck(ctx)
	require.NotEmpty(t, healthReports)
	assert.Equal(t, HealthStateHealthy, healthReports["email_notifier"].Status)
	assert.Equal(t, HealthStateHealthy, healthReports["image_processor"].Status)
}

type sidecarRoundTripFunc func(*http.Request) (*http.Response, error)

func (f sidecarRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type sidecarBlockingBody struct{ ctx context.Context }

func (b sidecarBlockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (sidecarBlockingBody) Close() error { return nil }

func TestHTTPSidecarCustomClientPolicy(t *testing.T) {
	var redirectCalls int
	client := &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			redirectCalls++
			return nil
		},
		Transport: sidecarRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/tasks/body":
				return &http.Response{StatusCode: http.StatusOK, Body: sidecarBlockingBody{req.Context()}}, nil
			case "/tasks/redirect":
				return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"/unexpected"}}}, nil
			case "/tasks/headers", "/healthz":
				<-req.Context().Done()
				return nil, req.Context().Err()
			default:
				t.Errorf("unexpected request path: %s", req.URL.Path)
				return nil, context.Canceled
			}
		}),
	}
	sidecar, err := NewHTTPSidecar(HTTPConfig{BaseURL: "http://sidecar.invalid", Client: client, Timeout: 25 * time.Millisecond})
	require.NoError(t, err)

	ctx := context.Background()
	for _, phase := range []string{"headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			_, err := sidecar.Forward(ctx, WorkerTask{UnitID: phase})
			require.ErrorIs(t, err, ErrTaskTimeout)
		})
	}
	report, err := sidecar.HealthCheck(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, HealthStateUnhealthy, report.Status)

	_, err = sidecar.Forward(ctx, WorkerTask{UnitID: "redirect"})
	require.ErrorContains(t, err, "redirects disabled")
	assert.Zero(t, redirectCalls)
	assert.Equal(t, time.Second, client.Timeout)
	require.NoError(t, client.CheckRedirect(nil, nil))
	assert.Equal(t, 1, redirectCalls)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = sidecar.Forward(canceled, WorkerTask{UnitID: "headers"})
	require.ErrorIs(t, err, context.Canceled)

	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	_, err = sidecar.Forward(expired, WorkerTask{UnitID: "headers"})
	require.ErrorIs(t, err, ErrTaskTimeout)
}
