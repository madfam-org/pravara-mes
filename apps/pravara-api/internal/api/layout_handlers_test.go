package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLayoutServiceRoutes_Answer503WithoutUpstream: GET /v1/layouts, GET
// /v1/layouts/:id and PUT /v1/layouts/:id answer 503 and never reach the
// layout service.
func TestLayoutServiceRoutes_Answer503WithoutUpstream(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	previous := vizEngineURL
	vizEngineURL = upstream.URL
	t.Cleanup(func() { vizEngineURL = previous })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	layouts := router.Group("/v1/layouts")
	layouts.GET("", handleLayoutServiceUnavailable())
	layouts.GET("/:id", handleLayoutServiceUnavailable())
	layouts.PUT("/:id", handleLayoutServiceUnavailable())

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/layouts", ""},
		{http.MethodGet, "/v1/layouts/11111111-1111-1111-1111-111111111111", ""},
		{http.MethodPut, "/v1/layouts/11111111-1111-1111-1111-111111111111", `{"name":"x"}`},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "layout_service_unavailable", body["error"])
			assert.Equal(t, "layout service is not available", body["message"])
		})
	}
	assert.Zero(t, upstreamCalls.Load(), "the layout service must not be called")
}

// TestLayoutRoutes_Wiring: the registered layout routes use the 503 handler,
// except /v1/layouts/active, which reads the caller's tenant.
func TestLayoutRoutes_Wiring(t *testing.T) {
	handlers := map[string]string{}
	for _, r := range registeredRouter(t).Routes() {
		if strings.HasPrefix(r.Path, "/v1/layouts") {
			handlers[r.Method+" "+r.Path] = r.Handler
		}
	}
	for _, key := range []string{"GET /v1/layouts", "GET /v1/layouts/:id", "PUT /v1/layouts/:id"} {
		require.Contains(t, handlers, key)
		assert.Contains(t, handlers[key], "handleLayoutServiceUnavailable", key)
	}
	require.Contains(t, handlers, "GET /v1/layouts/active")
	assert.Contains(t, handlers["GET /v1/layouts/active"], "handleGetActiveLayout")
}
