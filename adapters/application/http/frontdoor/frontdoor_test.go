package frontdoor_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mohammedrefaat/go-anything/adapters/application/http/frontdoor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrontDoor_MixedPortedAndUnportedRoutes(t *testing.T) {
	// 1. Original (legacy) server handling unported routes
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "legacy-original")
		switch r.URL.Path {
		case "/legacy/users":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id": 1, "name": "legacy_user"}]`))
		case "/legacy/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status": "legacy_ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "legacy not found"}`))
		}
	}))
	defer legacyServer.Close()

	targetURL, err := url.Parse(legacyServer.URL)
	require.NoError(t, err)

	// 2. Front Door setup
	fd, err := frontdoor.NewFrontDoor(targetURL)
	require.NoError(t, err)

	// Register ported routes
	fd.RegisterUnitRoute("unit-users", "/ported/v2/users", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "go-ported")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id": 1, "name": "modern_go_user"}]`))
	}))

	fd.RegisterPortedFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "go-ported")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"health": "ok"}`))
	})

	fdServer := httptest.NewServer(fd)
	defer fdServer.Close()

	// 3. Test Ported Route: /ported/v2/users -> served directly by Go handler
	resp, err := http.Get(fdServer.URL + "/ported/v2/users")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "go-ported", resp.Header.Get("X-Backend"))
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "modern_go_user")

	// 4. Test Unported Route: /legacy/users -> proxied to original server
	respLegacy, err := http.Get(fdServer.URL + "/legacy/users")
	require.NoError(t, err)
	defer respLegacy.Body.Close()

	assert.Equal(t, http.StatusOK, respLegacy.StatusCode)
	assert.Equal(t, "legacy-original", respLegacy.Header.Get("X-Backend"))
	legacyBody, _ := io.ReadAll(respLegacy.Body)
	assert.Contains(t, string(legacyBody), "legacy_user")

	// 5. Test Another Unported Route: /legacy/status -> proxied to original server
	respStatus, err := http.Get(fdServer.URL + "/legacy/status")
	require.NoError(t, err)
	defer respStatus.Body.Close()

	assert.Equal(t, http.StatusOK, respStatus.StatusCode)
	assert.Equal(t, "legacy-original", respStatus.Header.Get("X-Backend"))
	statusBody, _ := io.ReadAll(respStatus.Body)
	assert.Contains(t, string(statusBody), "legacy_ok")

	// 6. Test Ported Health Route: /health -> served directly
	respHealth, err := http.Get(fdServer.URL + "/health")
	require.NoError(t, err)
	defer respHealth.Body.Close()

	assert.Equal(t, http.StatusOK, respHealth.StatusCode)
	assert.Equal(t, "go-ported", respHealth.Header.Get("X-Backend"))
}

func TestFrontDoor_SharedAuthMiddleware(t *testing.T) {
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "legacy")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": "legacy_protected"}`))
	}))
	defer legacyServer.Close()

	targetURL, _ := url.Parse(legacyServer.URL)
	secretToken := "secret-jwt-token-12345"

	fd, err := frontdoor.NewFrontDoor(
		targetURL,
		frontdoor.WithTokenAuth(secretToken),
	)
	require.NoError(t, err)

	fd.RegisterPortedFunc("/ported/secure", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "go-ported")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": "ported_protected"}`))
	})

	fdServer := httptest.NewServer(fd)
	defer fdServer.Close()

	client := fdServer.Client()

	// A. Ported Route without Token -> 401 Unauthorized
	req1, _ := http.NewRequest(http.MethodGet, fdServer.URL+"/ported/secure", nil)
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp1.StatusCode)

	// B. Ported Route with Valid Token -> 200 OK
	req2, _ := http.NewRequest(http.MethodGet, fdServer.URL+"/ported/secure", nil)
	req2.Header.Set("Authorization", "Bearer "+secretToken)
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	body2, _ := io.ReadAll(resp2.Body)
	assert.Contains(t, string(body2), "ported_protected")

	// C. Unported Route without Token -> 401 Unauthorized (proves unported is wrapped by shared auth)
	req3, _ := http.NewRequest(http.MethodGet, fdServer.URL+"/legacy/data", nil)
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp3.StatusCode)

	// D. Unported Route with Valid Token -> 200 OK (proxied)
	req4, _ := http.NewRequest(http.MethodGet, fdServer.URL+"/legacy/data", nil)
	req4.Header.Set("Authorization", "Bearer "+secretToken)
	resp4, err := client.Do(req4)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp4.StatusCode)
	body4, _ := io.ReadAll(resp4.Body)
	assert.Contains(t, string(body4), "legacy_protected")
}

func TestFrontDoor_SharedCORSMiddleware(t *testing.T) {
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"origin": "legacy"}`))
	}))
	defer legacyServer.Close()

	targetURL, _ := url.Parse(legacyServer.URL)
	corsCfg := frontdoor.DefaultCORSConfig()

	fd, err := frontdoor.NewFrontDoor(targetURL, frontdoor.WithCORS(corsCfg))
	require.NoError(t, err)

	fd.RegisterPortedFunc("/ported/items", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"origin": "ported"}`))
	})

	fdServer := httptest.NewServer(fd)
	defer fdServer.Close()

	client := fdServer.Client()

	// 1. Preflight OPTIONS on Ported Route
	optPorted, _ := http.NewRequest(http.MethodOptions, fdServer.URL+"/ported/items", nil)
	optPorted.Header.Set("Origin", "http://frontend.local")
	respOpt1, err := client.Do(optPorted)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, respOpt1.StatusCode)
	assert.Equal(t, "*", respOpt1.Header.Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, respOpt1.Header.Get("Access-Control-Allow-Methods"))

	// 2. Preflight OPTIONS on Unported Route (Reverse Proxied)
	optUnported, _ := http.NewRequest(http.MethodOptions, fdServer.URL+"/legacy/anything", nil)
	optUnported.Header.Set("Origin", "http://frontend.local")
	respOpt2, err := client.Do(optUnported)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, respOpt2.StatusCode)
	assert.Equal(t, "*", respOpt2.Header.Get("Access-Control-Allow-Origin"))

	// 3. Simple GET request receives CORS headers
	getReq, _ := http.NewRequest(http.MethodGet, fdServer.URL+"/ported/items", nil)
	getReq.Header.Set("Origin", "http://frontend.local")
	respGet, err := client.Do(getReq)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, respGet.StatusCode)
	assert.Equal(t, "*", respGet.Header.Get("Access-Control-Allow-Origin"))
}

func TestFrontDoor_RouteInspection(t *testing.T) {
	targetURL, _ := url.Parse("http://127.0.0.1:9999")
	fd, err := frontdoor.NewFrontDoor(targetURL)
	require.NoError(t, err)

	assert.Equal(t, targetURL, fd.TargetURL())

	fd.RegisterUnitRoute("unit-auth", "/auth/login", http.NotFoundHandler())
	fd.RegisterUnitRoute("unit-billing", "/billing/checkout", http.NotFoundHandler())

	assert.True(t, fd.IsPorted("/auth/login"))
	assert.True(t, fd.IsPorted("/billing/checkout"))
	assert.False(t, fd.IsPorted("/unported/random"))

	routes := fd.ListPortedRoutes()
	assert.Equal(t, 2, len(routes))

	unitMap := make(map[string]string)
	for _, r := range routes {
		unitMap[r.Pattern] = r.UnitID
	}
	assert.Equal(t, "unit-auth", unitMap["/auth/login"])
	assert.Equal(t, "unit-billing", unitMap["/billing/checkout"])
}
