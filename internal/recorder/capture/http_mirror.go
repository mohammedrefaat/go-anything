package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
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
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "invalid mirror query", http.StatusBadRequest)
		return
	}
	unitID := r.Header.Get("X-Go-Anything-Unit")
	if unitID == "" {
		unitID = query.Get("unit_id")
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
		if err := json.Unmarshal(bodyBytes, &env); err != nil {
			http.Error(w, fmt.Sprintf("invalid mirror envelope: %v", err), http.StatusBadRequest)
			return
		}
		if env.Traffic.Request.Path == "" && env.Traffic.Request.Method == "" {
			http.Error(w, "missing request data in mirror envelope", http.StatusBadRequest)
			return
		}
		if env.UnitID != "" {
			unitID = env.UnitID
		}
		cleanUnit := filepath.Clean(filepath.FromSlash(unitID))
		if strings.HasPrefix(cleanUnit, "..") || filepath.IsAbs(cleanUnit) || cleanUnit == "." || cleanUnit == "" {
			http.Error(w, "invalid unit_id: path traversal forbidden", http.StatusBadRequest)
			return
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

	cleanUnit := filepath.Clean(filepath.FromSlash(unitID))
	if unitID != "" && (strings.HasPrefix(cleanUnit, "..") || filepath.IsAbs(cleanUnit) || cleanUnit == ".") {
		http.Error(w, "invalid unit_id: path traversal forbidden", http.StatusBadRequest)
		return
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
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
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

	reqPath := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		pairs := strings.Split(r.URL.RawQuery, "&")
		kept := pairs[:0]
		for _, pair := range pairs {
			rawKey, _, _ := strings.Cut(pair, "=")
			key, _ := url.QueryUnescape(rawKey) // RawQuery was validated above.
			if key != "unit_id" && key != "unit" {
				kept = append(kept, pair)
			}
		}
		if filtered := strings.Join(kept, "&"); filtered != "" {
			reqPath += "?" + filtered
		}
	}

	endpoint := *r.URL
	endpoint.RawQuery = ""
	endpoint.ForceQuery = false

	traffic := HTTPTraffic{
		Request: HTTPRequest{
			Method:     r.Method,
			Path:       reqPath,
			Headers:    headers,
			Body:       reqBody,
			RemoteAddr: r.RemoteAddr,
		},
		Response: HTTPResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
		},
		Metadata: map[string]any{
			"mirror_endpoint": endpoint.String(),
		},
	}

	if unitID == "" {
		http.Error(w, "missing unit_id", http.StatusBadRequest)
		return
	}

	_, err = h.ingester.IngestHTTP(r.Context(), unitID, traffic)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"captured"}`))
}
