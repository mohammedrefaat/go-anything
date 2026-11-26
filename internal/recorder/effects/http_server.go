package effects

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"

	"github.com/mohammedrefaat/go-anything/internal/redactor"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// HTTPServer intercepts and records outbound HTTP requests as side effects.
type HTTPServer struct {
	server   *httptest.Server
	orderSeq *int64
	effects  *[]adapterkit.Effect
	mu       *sync.Mutex
	red      *redactor.Redactor
}

// NewHTTPServer creates and starts a loopback HTTP recording server.
func NewHTTPServer(orderSeq *int64, effects *[]adapterkit.Effect, mu *sync.Mutex) *HTTPServer {
	h := &HTTPServer{
		orderSeq: orderSeq,
		effects:  effects,
		mu:       mu,
		red:      redactor.New(),
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		hash := sha256.Sum256(bodyBytes)
		bodyHash := hex.EncodeToString(hash[:])

		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		redactedHeaders, _, _ := h.red.RedactHeaders(headers)

		order := int(atomic.AddInt64(h.orderSeq, 1))

		eff := adapterkit.Effect{
			Type:     "http",
			Order:    order,
			Target:   r.URL.String(),
			Method:   r.Method,
			Headers:  redactedHeaders,
			BodyHash: bodyHash,
			Count:    1,
		}

		h.mu.Lock()
		*h.effects = append(*h.effects, eff)
		h.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"recorded"}`))
	})

	h.server = httptest.NewServer(handler)
	return h
}

// URL returns the loopback base URL of the HTTP recording server.
func (h *HTTPServer) URL() string {
	if h.server == nil {
		return ""
	}
	return h.server.URL
}

// Close terminates the HTTP recording server.
func (h *HTTPServer) Close() {
	if h.server != nil {
		h.server.Close()
	}
}
