// Package frontdoor implements the strangler pattern HTTP front door (reverse proxy)
// routing ported requests to native Go handlers and unported requests to the legacy server.
package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

// Authenticator evaluates whether an incoming request is authenticated.
type Authenticator func(r *http.Request) (bool, error)

// CORSConfig specifies Cross-Origin Resource Sharing policy parameters.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultCORSConfig returns a permissive development CORS configuration.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"},
		AllowedHeaders: []string{"Content-Type", "Authorization", "X-Request-ID", "Accept"},
		MaxAge:         86400,
	}
}

// RouteInfo describes a registered ported route.
type RouteInfo struct {
	Pattern     string `json:"pattern"`
	UnitID      string `json:"unit_id,omitempty"`
	Description string `json:"description,omitempty"`
}

// Option configures the FrontDoor instance.
type Option func(*FrontDoor)

// WithCORS configures CORS middleware.
func WithCORS(cfg CORSConfig) Option {
	return func(fd *FrontDoor) {
		fd.corsConfig = &cfg
	}
}

// WithAuthenticator configures authentication middleware.
func WithAuthenticator(auth Authenticator) Option {
	return func(fd *FrontDoor) {
		fd.authenticator = auth
	}
}

// WithTokenAuth configures simple Bearer token authentication.
func WithTokenAuth(validToken string) Option {
	return func(fd *FrontDoor) {
		fd.authenticator = func(r *http.Request) (bool, error) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				return false, nil
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				return parts[1] == validToken, nil
			}
			return false, nil
		}
	}
}

// WithReverseProxy sets a custom ReverseProxy instance.
func WithReverseProxy(proxy *httputil.ReverseProxy) Option {
	return func(fd *FrontDoor) {
		fd.proxy = proxy
	}
}

// FrontDoor routes HTTP requests between ported native Go handlers and the legacy server.
type FrontDoor struct {
	targetURL     *url.URL
	proxy         *httputil.ReverseProxy
	mux           *http.ServeMux
	routes        map[string]RouteInfo
	corsConfig    *CORSConfig
	authenticator Authenticator
	mu            sync.RWMutex
}

// NewFrontDoor creates a new FrontDoor routing unported requests to targetURL.
func NewFrontDoor(targetURL *url.URL, opts ...Option) (*FrontDoor, error) {
	if targetURL == nil {
		return nil, errors.New("frontdoor: targetURL cannot be nil")
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
	}

	fd := &FrontDoor{
		targetURL: targetURL,
		proxy:     proxy,
		mux:       http.NewServeMux(),
		routes:    make(map[string]RouteInfo),
	}

	for _, opt := range opts {
		opt(fd)
	}

	// Default unported fallback route: proxy to legacy server
	fd.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fd.proxy.ServeHTTP(w, r)
	})

	return fd, nil
}

// RegisterPorted registers a ported Go handler for the given pattern.
func (fd *FrontDoor) RegisterPorted(pattern string, handler http.Handler) {
	fd.RegisterUnitRoute("", pattern, handler)
}

// RegisterPortedFunc registers a ported handler function for the given pattern.
func (fd *FrontDoor) RegisterPortedFunc(pattern string, fn http.HandlerFunc) {
	fd.RegisterUnitRoute("", pattern, fn)
}

// RegisterUnitRoute registers a ported handler tied to a specific migration unit.
func (fd *FrontDoor) RegisterUnitRoute(unitID, pattern string, handler http.Handler) {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	fd.routes[pattern] = RouteInfo{
		Pattern: pattern,
		UnitID:  unitID,
	}

	// Register on ServeMux
	fd.mux.Handle(pattern, handler)
}

// ListPortedRoutes returns information on all registered ported routes.
func (fd *FrontDoor) ListPortedRoutes() []RouteInfo {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	result := make([]RouteInfo, 0, len(fd.routes))
	for _, info := range fd.routes {
		result = append(result, info)
	}
	return result
}

// IsPorted checks whether a pattern is handled by a ported Go handler.
func (fd *FrontDoor) IsPorted(pattern string) bool {
	fd.mu.RLock()
	defer fd.mu.RUnlock()
	_, ok := fd.routes[pattern]
	return ok
}

// TargetURL returns the legacy target server URL.
func (fd *FrontDoor) TargetURL() *url.URL {
	return fd.targetURL
}

// ServeHTTP implements http.Handler, applying CORS and Auth middleware before routing.
func (fd *FrontDoor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. CORS Middleware (wraps both ported and proxied routes)
	if fd.corsConfig != nil {
		fd.applyCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// 2. Authentication Middleware (wraps both ported and proxied routes)
	if fd.authenticator != nil {
		ok, err := fd.authenticator(r)
		if err != nil || !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","message":"invalid or missing credentials"}`))
			return
		}
	}

	// 3. Routing (Ported routes served directly, unported routed to reverse proxy)
	fd.mux.ServeHTTP(w, r)
}

func (fd *FrontDoor) applyCORSHeaders(w http.ResponseWriter, r *http.Request) {
	cfg := fd.corsConfig
	if cfg == nil {
		return
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}

	allowOrigin := "*"
	if len(cfg.AllowedOrigins) > 0 && cfg.AllowedOrigins[0] != "*" {
		matched := false
		for _, o := range cfg.AllowedOrigins {
			if o == origin || o == "*" {
				matched = true
				break
			}
		}
		if matched {
			allowOrigin = origin
		} else {
			allowOrigin = cfg.AllowedOrigins[0]
		}
	}

	w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
	if len(cfg.AllowedMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ", "))
	}
	if len(cfg.AllowedHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ", "))
	}
	if cfg.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	if cfg.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", cfg.MaxAge))
	}
}

// WrapWithContext wraps execution with a custom request context.
func WrapWithContext(ctx context.Context, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
