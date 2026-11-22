package capture

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// HTTPMirrorSource ingests mirrored HTTP traffic from gateways or sidecars.
// It is strictly read-only: it absorbs mirrored traffic asynchronously and has
// NO control over live request routing or upstream forwarding.
type HTTPMirrorSource struct {
	ingester      *Ingester
	defaultUnitID string
}

// NewHTTPMirrorSource creates a new HTTPMirrorSource backed by the given Ingester.
func NewHTTPMirrorSource(ingester *Ingester, defaultUnitID string) *HTTPMirrorSource {
	return &HTTPMirrorSource{
		ingester:      ingester,
		defaultUnitID: defaultUnitID,
	}
}

// Ingest programmatically feeds an HTTP traffic exchange into the ingester.
func (h *HTTPMirrorSource) Ingest(ctx context.Context, unitID string, traffic HTTPTraffic) (*adapterkit.Case, error) {
	if unitID == "" {
		unitID = h.defaultUnitID
	}
	return h.ingester.IngestHTTP(ctx, unitID, traffic)
}

// MirrorEnvelope represents a structured payload submitted by a gateway or sidecar logger.
type MirrorEnvelope struct {
	UnitID   string         `json:"unit_id,omitempty"`
	Traffic  HTTPTraffic    `json:"traffic"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ServeHTTP implements http.Handler to receive mirrored traffic over HTTP.
// It absorbs mirrored requests, returns a fast 200/204 response, and never proxies or routes.
func (h *HTTPMirrorSource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	unitID := r.Header.Get("X-Go-Anything-Unit")
	if unitID == "" {
		unitID = r.URL.Query().Get("unit_id")
	}
	if unitID == "" {
		unitID = h.defaultUnitID
	}

	contentType := r.Header.Get("Content-Type")

	// If payload is a structured JSON mirror envelope
	if strings.Contains(contentType, "application/json") && r.Header.Get("X-Go-Anything-Envelope") == "true" {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()

		var env MirrorEnvelope
		if err := json.Unmarshal(bodyBytes, &env); err == nil && (env.Traffic.Request.Path != "" || env.Traffic.Request.Method != "") {
			if env.UnitID != "" {
				unitID = env.UnitID
			}
			if env.Metadata != nil {
				if env.Traffic.Metadata == nil {
					env.Traffic.Metadata = make(map[string]any)
				}
				for k, v := range env.Metadata {
					env.Traffic.Metadata[k] = v
				}
			}
			_, err = h.ingester.IngestHTTP(r.Context(), unitID, env.Traffic)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"captured"}`))
			return
		}
	}

	// Otherwise, ingest the live incoming mirrored request directly
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	var reqBody any
	if r.Body != nil {
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if len(bodyBytes) > 0 {
			var parsed any
			if json.Unmarshal(bodyBytes, &parsed) == nil {
				reqBody = parsed
			} else {
				reqBody = string(bodyBytes)
			}
		}
	}

	traffic := HTTPTraffic{
		Request: HTTPRequest{
			Method:     r.Method,
			Path:       r.URL.Path,
			Headers:    headers,
			Body:       reqBody,
			RemoteAddr: r.RemoteAddr,
		},
		Response: HTTPResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
		},
		Metadata: map[string]any{
			"mirror_endpoint": r.URL.String(),
		},
	}

	if unitID == "" {
		http.Error(w, "missing unit_id", http.StatusBadRequest)
		return
	}

	_, err := h.ingester.IngestHTTP(r.Context(), unitID, traffic)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"captured"}`))
}
