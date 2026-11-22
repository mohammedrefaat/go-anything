package capture_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/recorder/capture"
	"github.com/mohammedrefat/go-anything/internal/redactor"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root with go.mod")
		}
		dir = parent
	}
}

func compileCaseSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	repoRoot := findRepoRoot(t)
	schemaPath := filepath.Join(repoRoot, "schemas", "v1alpha1", "cases.schema.json")

	f, err := os.Open(schemaPath)
	require.NoError(t, err, "failed to open cases schema file at %s", schemaPath)
	defer f.Close()

	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	err = compiler.AddResource("cases.schema.json", f)
	require.NoError(t, err)

	schema, err := compiler.Compile("cases.schema.json")
	require.NoError(t, err)
	return schema
}

func validateCaseAgainstSchema(t *testing.T, schema *jsonschema.Schema, c *adapterkit.Case) {
	t.Helper()
	data, err := json.Marshal(c)
	require.NoError(t, err)

	var val any
	err = json.Unmarshal(data, &val)
	require.NoError(t, err)

	err = schema.Validate(val)
	assert.NoError(t, err, "case %s failed schema validation: %v", c.CaseID, err)
}

func setupTestIngester(t *testing.T) (*capture.Ingester, *capture.MemorySink) {
	t.Helper()
	sink := capture.NewMemorySink()
	fixedTime := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	cfg := capture.Config{
		Sink:           sink,
		Redactor:       redactor.New(),
		SourceRevision: "a1b2c3d4e5f60123456789abcdef0123456789ab",
		Runtime:        "go1.22",
		TimeNow: func() time.Time {
			return fixedTime
		},
	}
	ing := capture.NewIngester(cfg)
	return ing, sink
}

func TestHTTPMirrorSource_DirectAndHandler(t *testing.T) {
	schema := compileCaseSchema(t)
	ing, sink := setupTestIngester(t)
	httpSource := capture.NewHTTPMirrorSource(ing, "auth-unit")

	t.Run("programmatic direct ingestion", func(t *testing.T) {
		traffic := capture.HTTPTraffic{
			Request: capture.HTTPRequest{
				Method: "POST",
				Path:   "/api/v1/login",
				Headers: map[string]string{
					"Content-Type": "application/json",
					"User-Agent":   "TestAgent/1.0",
				},
				Body:       map[string]any{"username": "johndoe"},
				RemoteAddr: "192.168.1.100:44321",
			},
			Response: capture.HTTPResponse{
				StatusCode: 200,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body:       map[string]any{"status": "ok"},
				DurationMS: 12.5,
			},
			Metadata: map[string]any{
				"gateway": "envoy-edge-01",
			},
		}

		c, err := httpSource.Ingest(context.Background(), "auth-unit", traffic)
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, adapterkit.SchemaVersionV1Alpha1, c.SchemaVersion)
		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, "auth-unit", c.UnitID)
		assert.Equal(t, "case-auth-unit-cap-0001", c.CaseID)
		assert.Equal(t, "POST", c.Input.Method)
		assert.Equal(t, "/api/v1/login", c.Input.Path)
		assert.Equal(t, 200, c.ExpectedOutput.StatusCode)
		assert.Equal(t, "gateway_mirror", c.Metadata["source"])
		assert.Equal(t, "envoy-edge-01", c.Metadata["gateway"])

		validateCaseAgainstSchema(t, schema, c)
	})

	t.Run("HTTP handler live request mirroring", func(t *testing.T) {
		reqBody := `{"action":"sync","item_count":5}`
		req := httptest.NewRequest(http.MethodPost, "/sync?unit_id=sync-unit", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "10.0.0.1")

		rr := httptest.NewRecorder()
		httpSource.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"status":"captured"`)

		cases := sink.CasesForUnit("sync-unit")
		require.Len(t, cases, 1)
		c := cases[0]
		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, "sync-unit", c.UnitID)
		assert.Equal(t, "case-sync-unit-cap-0001", c.CaseID)
		assert.Equal(t, "POST", c.Input.Method)
		assert.Equal(t, "/sync", c.Input.Path)

		validateCaseAgainstSchema(t, schema, c)
	})

	t.Run("HTTP handler structured envelope mirroring", func(t *testing.T) {
		env := capture.MirrorEnvelope{
			UnitID: "billing-unit",
			Traffic: capture.HTTPTraffic{
				Request: capture.HTTPRequest{
					Method:  "GET",
					Path:    "/invoices/inv-001",
					Headers: map[string]string{"Accept": "application/json"},
				},
				Response: capture.HTTPResponse{
					StatusCode: 200,
					Body:       map[string]any{"invoice_id": "inv-001", "amount": 42.0},
				},
			},
			Metadata: map[string]any{"cluster": "prod-us-east"},
		}
		data, err := json.Marshal(env)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mirror", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Go-Anything-Envelope", "true")

		rr := httptest.NewRecorder()
		httpSource.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		cases := sink.CasesForUnit("billing-unit")
		require.Len(t, cases, 1)
		c := cases[0]
		assert.Equal(t, "case-billing-unit-cap-0001", c.CaseID)
		assert.Equal(t, "prod-us-east", c.Metadata["cluster"])

		validateCaseAgainstSchema(t, schema, c)
	})
}

func TestBrokerConsumerSource(t *testing.T) {
	schema := compileCaseSchema(t)
	ing, sink := setupTestIngester(t)
	brokerSource := capture.NewBrokerConsumerSource(ing)

	t.Run("single message consumption", func(t *testing.T) {
		msg := capture.BrokerMessage{
			Topic:   "orders.placed",
			Key:     "ord-12345",
			Headers: map[string]string{"X-Correlation-ID": "corr-999"},
			Payload: map[string]any{
				"order_id": "ord-12345",
				"items":    []any{"item-A", "item-B"},
			},
			ResponsePayload: map[string]any{"ack": true, "processed": true},
			StatusCode:      200,
			Metadata:        map[string]any{"partition": 3, "offset": 1042},
		}

		c, err := brokerSource.Ingest(context.Background(), "order-consumer", msg)
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, "order-consumer", c.UnitID)
		assert.Equal(t, "MESSAGE", c.Input.Method)
		assert.Equal(t, "topic:orders.placed", c.Input.Path)
		assert.Equal(t, "ord-12345", c.Input.Headers["message_key"])
		assert.Equal(t, "corr-999", c.Input.Headers["X-Correlation-ID"])
		assert.Equal(t, "broker_consumer", c.Metadata["source"])
		assert.Equal(t, "orders.placed", c.Metadata["topic"])

		validateCaseAgainstSchema(t, schema, c)
	})

	t.Run("batch message consumption", func(t *testing.T) {
		msgs := []capture.BrokerMessage{
			{Topic: "events.audit", Payload: map[string]any{"event": "login"}},
			{Topic: "events.audit", Payload: map[string]any{"event": "logout"}},
		}

		cases, err := brokerSource.IngestBatch(context.Background(), "audit-unit", msgs)
		require.NoError(t, err)
		require.Len(t, cases, 2)

		assert.Equal(t, "case-audit-unit-cap-0001", cases[0].CaseID)
		assert.Equal(t, "case-audit-unit-cap-0002", cases[1].CaseID)

		stored := sink.CasesForUnit("audit-unit")
		assert.Len(t, stored, 2)
		validateCaseAgainstSchema(t, schema, cases[0])
		validateCaseAgainstSchema(t, schema, cases[1])
	})
}

func TestTraceSource(t *testing.T) {
	schema := compileCaseSchema(t)
	ing, sink := setupTestIngester(t)
	traceSource := capture.NewTraceSource(ing)

	t.Run("OpenTelemetry trace span with attributes", func(t *testing.T) {
		span := capture.TraceSpan{
			TraceID:      "4bf92f3577b34da6a3ce929d0e0e4736",
			SpanID:       "00f067aa0ba902b7",
			ParentSpanID: "5fb397be34d23b0f",
			Name:         "HTTP GET /users/123",
			Kind:         "SERVER",
			StartTime:    time.Now().Add(-50 * time.Millisecond),
			EndTime:      time.Now(),
			Attributes: map[string]any{
				"http.method":                  "GET",
				"http.target":                  "/users/123",
				"http.status_code":             200,
				"http.request.header.accept":   "application/json",
				"http.response.header.x-cache": "HIT",
				"http.response.body":           `{"user_id": 123, "name": "Alice"}`,
			},
			Metadata: map[string]any{"environment": "staging"},
		}

		c, err := traceSource.IngestSpan(context.Background(), "users-unit", span)
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, "GET", c.Input.Method)
		assert.Equal(t, "/users/123", c.Input.Path)
		assert.Equal(t, "application/json", c.Input.Headers["accept"])
		assert.Equal(t, 200, c.ExpectedOutput.StatusCode)
		assert.Equal(t, "HIT", c.ExpectedOutput.Headers["x-cache"])
		assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", c.Metadata["trace_id"])
		assert.Equal(t, "00f067aa0ba902b7", c.Metadata["span_id"])
		assert.Equal(t, "distributed_trace", c.Metadata["source"])

		validateCaseAgainstSchema(t, schema, c)
	})

	t.Run("trace span with embedded HTTP traffic", func(t *testing.T) {
		span := capture.TraceSpan{
			TraceID: "trace-xyz-100",
			SpanID:  "span-abc-200",
			Name:    "UpdateProfile",
			HTTP: &capture.HTTPTraffic{
				Request: capture.HTTPRequest{
					Method:  "PATCH",
					Path:    "/profile",
					Headers: map[string]string{"Content-Type": "application/json"},
					Body:    map[string]any{"display_name": "NewName"},
				},
				Response: capture.HTTPResponse{
					StatusCode: 204,
				},
			},
		}

		c, err := traceSource.IngestSpan(context.Background(), "profile-unit", span)
		require.NoError(t, err)
		assert.Equal(t, "PATCH", c.Input.Method)
		assert.Equal(t, "/profile", c.Input.Path)
		assert.Equal(t, 204, c.ExpectedOutput.StatusCode)

		validateCaseAgainstSchema(t, schema, c)
	})

	t.Run("batch span ingestion", func(t *testing.T) {
		spans := []capture.TraceSpan{
			{Name: "span1", Attributes: map[string]any{"http.target": "/step1"}},
			{Name: "span2", Attributes: map[string]any{"http.target": "/step2"}},
		}
		cases, err := traceSource.IngestSpans(context.Background(), "workflow-unit", spans)
		require.NoError(t, err)
		assert.Len(t, cases, 2)
		assert.Len(t, sink.CasesForUnit("workflow-unit"), 2)
	})
}

func TestObservabilitySource_PlatformMCP(t *testing.T) {
	schema := compileCaseSchema(t)
	ing, sink := setupTestIngester(t)
	obsSource := capture.NewObservabilitySource(ing)

	t.Run("decoded traffic ingestion from platform-mcp", func(t *testing.T) {
		decoded := capture.DecodedTraffic{
			Source:    "platform-mcp",
			Operation: "auth.verifyToken",
			Protocol:  "grpc",
			Input: map[string]any{
				"token_id": "tok-789",
				"scope":    "read:profile",
			},
			Output: map[string]any{
				"valid":   true,
				"user_id": "usr-456",
			},
			Metadata: map[string]any{
				"sampler_rate": 0.1,
				"collector":    "mcp-agent-1",
			},
		}

		c, err := obsSource.Ingest(context.Background(), "auth-service", decoded)
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, adapterkit.KindCaptured, c.Kind)
		assert.Equal(t, "auth-service", c.UnitID)
		assert.Equal(t, "auth.verifyToken", c.Input.Path)
		assert.Equal(t, "platform-mcp", c.Metadata["source"])
		assert.Equal(t, "grpc", c.Metadata["protocol"])

		validateCaseAgainstSchema(t, schema, c)
	})

	t.Run("batch decoded traffic ingestion", func(t *testing.T) {
		records := []capture.DecodedTraffic{
			{Source: "platform-mcp", Operation: "get_status", Output: "running"},
			{Source: "platform-mcp", Operation: "get_metrics", Output: map[string]any{"cpu": 12.4}},
		}
		cases, err := obsSource.IngestBatch(context.Background(), "stats-unit", records)
		require.NoError(t, err)
		assert.Len(t, cases, 2)
		assert.Len(t, sink.CasesForUnit("stats-unit"), 2)
	})
}

func TestObservabilitySource_RoutingControlStrictlyForbidden(t *testing.T) {
	ing, sink := setupTestIngester(t)
	obsSource := capture.NewObservabilitySource(ing)

	forbiddenAttempts := []struct {
		name     string
		metadata map[string]any
	}{
		{
			name:     "route_to injection",
			metadata: map[string]any{"route_to": "https://malicious.example.com"},
		},
		{
			name:     "reroute injection",
			metadata: map[string]any{"reroute": true},
		},
		{
			name:     "routing_override injection",
			metadata: map[string]any{"routing_override": "canary_v2"},
		},
		{
			name:     "routing_rule injection",
			metadata: map[string]any{"routing_rule": "bypass_shadow"},
		},
		{
			name:     "routing_action injection",
			metadata: map[string]any{"routing_action": "promote"},
		},
		{
			name:     "traffic_weight injection",
			metadata: map[string]any{"traffic_weight": 100},
		},
		{
			name:     "traffic_split injection",
			metadata: map[string]any{"traffic_split": 50},
		},
		{
			name:     "kill_switch injection",
			metadata: map[string]any{"kill_switch": true},
		},
	}

	for _, tc := range forbiddenAttempts {
		t.Run(tc.name, func(t *testing.T) {
			decoded := capture.DecodedTraffic{
				Source:    "platform-mcp",
				Operation: "doSomething",
				Input:     map[string]any{"arg": "value"},
				Output:    map[string]any{"result": "success"},
				Metadata:  tc.metadata,
			}

			c, err := obsSource.Ingest(context.Background(), "target-unit", decoded)
			assert.Error(t, err)
			assert.Nil(t, c)
			assert.ErrorIs(t, err, capture.ErrRoutingControlForbidden)
		})
	}

	// Verify that zero cases were persisted
	assert.Empty(t, sink.CasesForUnit("target-unit"))
}

func TestFailClosedRedaction_SecretsNeverStored(t *testing.T) {
	schema := compileCaseSchema(t)
	ing, sink := setupTestIngester(t)
	httpSource := capture.NewHTTPMirrorSource(ing, "secure-unit")

	// Raw sensitive credentials that MUST NEVER leak to disk or in-memory case
	rawBearerToken := "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkFsaWNlIn0.dozS6-m4Z_fake_jwt_signature_xyz"
	rawBasicAuth := "Basic YWRtaW46c3VwZXJfc2VjcmV0X3Bhc3N3b3Jk"
	rawCookie := "session_token=s_sec_token_987654321; tracking_id=track_123"
	rawPassword := "SuperSecretPassword123!@#"
	rawCreditCard := "4000 0012 3456 7899" // Valid Luhn card
	rawAWSKey := "AKIAIOSFODNN7EXAMPLE"
	rawGitHubToken := "ghp_1234567890abcdef1234567890abcdef1234"
	rawStripeKey := "sk_test_" + "1234567890abcdef12345678"
	rawSignatureHeader := "sha256=abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	traffic := capture.HTTPTraffic{
		Request: capture.HTTPRequest{
			Method: "POST",
			Path:   "/api/checkout",
			Headers: map[string]string{
				"Authorization":       rawBearerToken,
				"Proxy-Authorization": rawBasicAuth,
				"Cookie":              rawCookie,
				"X-Api-Key":           "raw-api-key-secret-99999",
				"X-Signature-SHA256":  rawSignatureHeader, // Should become [unreplayable]
			},
			Body: map[string]any{
				"password":    rawPassword,
				"credit_card": rawCreditCard,
				"aws_key":     rawAWSKey,
				"github_tok":  rawGitHubToken,
				"stripe_key":  rawStripeKey,
				"order_id":    "order-clean-123",
			},
		},
		Response: capture.HTTPResponse{
			StatusCode: 200,
			Headers: map[string]string{
				"Set-Cookie": "auth_cookie=auth_val_12345; Secure; HttpOnly",
			},
			Body: map[string]any{
				"receipt_id": "rec-555",
				"card_used":  rawCreditCard,
			},
		},
	}

	c, err := httpSource.Ingest(context.Background(), "secure-unit", traffic)
	require.NoError(t, err)
	require.NotNil(t, c)

	// Serialize case to JSON string to inspect full payload
	caseJSON, err := json.Marshal(c)
	require.NoError(t, err)
	caseStr := string(caseJSON)

	// 1. Verify secrets NEVER appear anywhere in the case record or serialized JSON
	assert.NotContains(t, caseStr, rawPassword, "password must be redacted")
	assert.NotContains(t, caseStr, rawCreditCard, "credit card must be redacted")
	assert.NotContains(t, caseStr, rawAWSKey, "AWS key must be redacted")
	assert.NotContains(t, caseStr, rawGitHubToken, "GitHub token must be redacted")
	assert.NotContains(t, caseStr, rawStripeKey, "Stripe key must be redacted")
	assert.NotContains(t, caseStr, "raw-api-key-secret-99999", "API key must be redacted")
	assert.NotContains(t, caseStr, "s_sec_token_987654321", "cookie session token must be redacted")
	assert.NotContains(t, caseStr, "super_secret_password", "basic auth password must be redacted")

	// 2. Verify format-preserving synthetic replacements
	assert.Contains(t, c.Input.Headers["Authorization"], "Bearer ")
	assert.NotEqual(t, rawBearerToken, c.Input.Headers["Authorization"])

	// 3. Verify signature header is marked as unreplayable
	assert.Equal(t, "[unreplayable]", c.Input.Headers["X-Signature-SHA256"])
	assert.Contains(t, c.UnreplayableFields, "input.headers.X-Signature-SHA256")

	// 4. Verify clean non-secret fields remain intact
	assert.Equal(t, "/api/checkout", c.Input.Path)
	reqBodyMap, ok := c.Input.Body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "order-clean-123", reqBodyMap["order_id"])

	// 5. Verify against cases schema
	validateCaseAgainstSchema(t, schema, c)

	// 6. Verify stored case in sink also has no leaked secrets
	storedCases := sink.CasesForUnit("secure-unit")
	require.Len(t, storedCases, 1)
	storedJSON, err := json.Marshal(storedCases[0])
	require.NoError(t, err)
	assert.NotContains(t, string(storedJSON), rawPassword)
	assert.NotContains(t, string(storedJSON), rawCreditCard)
}

func TestFailClosedRedaction_MalformedJSONFailsClosed(t *testing.T) {
	ing, sink := setupTestIngester(t)
	httpSource := capture.NewHTTPMirrorSource(ing, "fail-closed-unit")

	// Malformed JSON body starting with {
	traffic := capture.HTTPTraffic{
		Request: capture.HTTPRequest{
			Method: "POST",
			Path:   "/broken",
			Body:   `{"broken_json": [missing closing brace`,
		},
		Response: capture.HTTPResponse{
			StatusCode: 500,
		},
	}

	c, err := httpSource.Ingest(context.Background(), "fail-closed-unit", traffic)
	assert.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "fail-closed redaction failed")

	// Zero cases persisted to sink
	assert.Empty(t, sink.CasesForUnit("fail-closed-unit"))
}

func TestFileSink_DurablePersistence(t *testing.T) {
	schema := compileCaseSchema(t)
	tmpDir, err := os.MkdirTemp("", "go-anything-capture-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	casesDir := filepath.Join(tmpDir, ".go-anything", "cases")
	fileSink := capture.NewFileSink(casesDir)

	cfg := capture.Config{
		WorkspaceRoot:  tmpDir,
		Sink:           fileSink,
		Redactor:       redactor.New(),
		SourceRevision: "deadbeef0123456789abcdef0123456789abcdef",
		Runtime:        "go1.22",
	}
	ing := capture.NewIngester(cfg)
	httpSource := capture.NewHTTPMirrorSource(ing, "file-unit")

	for i := 1; i <= 3; i++ {
		traffic := capture.HTTPTraffic{
			Request: capture.HTTPRequest{
				Method: "GET",
				Path:   "/items",
			},
			Response: capture.HTTPResponse{
				StatusCode: 200,
				Body:       map[string]any{"count": i},
			},
		}
		c, err := httpSource.Ingest(context.Background(), "file-unit", traffic)
		require.NoError(t, err)
		assert.Equal(t, "case-file-unit-cap-000"+string(rune('0'+i)), c.CaseID)
		validateCaseAgainstSchema(t, schema, c)
	}

	// Verify file on disk
	caseFile := filepath.Join(casesDir, "file-unit.jsonl")
	assert.FileExists(t, caseFile)

	content, err := os.ReadFile(caseFile)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	assert.Len(t, lines, 3)

	for _, line := range lines {
		var parsed adapterkit.Case
		err := json.Unmarshal([]byte(line), &parsed)
		require.NoError(t, err)
		assert.Equal(t, adapterkit.KindCaptured, parsed.Kind)
		assert.Equal(t, "file-unit", parsed.UnitID)
	}
}

func TestConcurrency_RaceFreeIngestion(t *testing.T) {
	schema := compileCaseSchema(t)
	ing, sink := setupTestIngester(t)
	brokerSource := capture.NewBrokerConsumerSource(ing)

	const goroutines = 10
	const msgsPerGoroutine = 10
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for m := 0; m < msgsPerGoroutine; m++ {
				msg := capture.BrokerMessage{
					Topic:   "concurrent.events",
					Payload: map[string]any{"g": gid, "m": m},
				}
				c, err := brokerSource.Ingest(context.Background(), "conc-unit", msg)
				assert.NoError(t, err)
				assert.NotNil(t, c)
			}
		}(g)
	}

	wg.Wait()

	cases := sink.CasesForUnit("conc-unit")
	assert.Len(t, cases, goroutines*msgsPerGoroutine)

	// Verify all case IDs are unique
	seenIDs := make(map[string]bool)
	for _, c := range cases {
		assert.False(t, seenIDs[c.CaseID], "duplicate case ID: %s", c.CaseID)
		seenIDs[c.CaseID] = true
		validateCaseAgainstSchema(t, schema, c)
	}
}
