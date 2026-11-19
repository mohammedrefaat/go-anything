package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/mohammedrefat/go-anything/adapters/application/http"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllocateEphemeralPort(t *testing.T) {
	port, err := httpadapter.AllocateEphemeralPort()
	require.NoError(t, err)
	assert.Greater(t, port, 1024, "expected non-privileged port number")
}

func TestHTTPAdapter_ExecuteGetAndPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/info":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Custom-Server", "test-server")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"app":     "test-app",
				"version": "1.0.0",
			})

		case "/api/echo":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"echo":   body,
				"auth":   r.Header.Get("Authorization"),
				"status": "created",
			})

		case "/api/error":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "internal server error occurred")

		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "route not found")
		}
	}))
	defer server.Close()

	adapter := httpadapter.NewAdapter(
		httpadapter.WithBaseURL(server.URL),
		httpadapter.WithTimeout(5*time.Second),
	)
	defer adapter.Close()

	assert.Contains(t, adapter.DriverName(), "http:")

	// 1. Test GET request
	cGet := &adapterkit.Case{
		CaseID: "http-get-01",
		UnitID: "http-test",
		Kind:   adapterkit.KindHappy,
		Input: adapterkit.CaseInput{
			Method: "GET",
			Path:   "/api/info",
		},
	}

	outGet, err := adapter.ExecuteCase(context.Background(), cGet)
	require.NoError(t, err)

	assert.Equal(t, 200, outGet.StatusCode)
	assert.Equal(t, "test-server", outGet.Headers["X-Custom-Server"])
	bodyMap, ok := outGet.Body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test-app", bodyMap["app"])

	// 2. Test POST request with body and headers
	cPost := &adapterkit.Case{
		CaseID: "http-post-01",
		UnitID: "http-test",
		Kind:   adapterkit.KindHappy,
		Input: adapterkit.CaseInput{
			Method: "POST",
			Path:   "/api/echo",
			Headers: map[string]string{
				"Authorization": "Bearer secret-token-xyz",
			},
			Body: map[string]any{
				"message": "hello server",
			},
		},
	}

	outPost, err := adapter.ExecuteCase(context.Background(), cPost)
	require.NoError(t, err)

	assert.Equal(t, 201, outPost.StatusCode)
	postBodyMap, ok := outPost.Body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "created", postBodyMap["status"])
	assert.Equal(t, "Bearer secret-token-xyz", postBodyMap["auth"])

	// 3. Test Error response
	cErr := &adapterkit.Case{
		CaseID: "http-err-01",
		UnitID: "http-test",
		Kind:   adapterkit.KindTrap,
		Input: adapterkit.CaseInput{
			Method: "GET",
			Path:   "/api/error",
		},
	}

	outErr, err := adapter.ExecuteCase(context.Background(), cErr)
	require.NoError(t, err)

	assert.Equal(t, 500, outErr.StatusCode)
	assert.NotEmpty(t, outErr.ErrorMessage)
	assert.Contains(t, outErr.Stdout, "internal server error occurred")
}
