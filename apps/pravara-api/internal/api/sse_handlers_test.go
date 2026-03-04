package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

func TestNewSSEHandler(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	cfg := config.SSEConfig{
		MaxConnections:   100,
		KeepaliveSeconds: 30,
	}

	handler := NewSSEHandler(nil, nil, cfg, log)
	if handler == nil {
		t.Fatal("expected non-nil SSEHandler")
	}
}

func TestSSEHandler_Stream_MissingAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	cfg := config.SSEConfig{
		KeepaliveSeconds: 30,
	}
	handler := NewSSEHandler(nil, nil, cfg, log)

	router := gin.New()
	router.GET("/v1/events/stream", handler.Stream)

	// Request without tenant context set -> should return 401
	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 without auth context, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	if body["error"] != "unauthorized" {
		t.Errorf("expected error 'unauthorized', got %v", body["error"])
	}
}

func TestSSEHandler_Stream_SetsSSEHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	cfg := config.SSEConfig{
		KeepaliveSeconds: 30,
	}
	handler := NewSSEHandler(nil, nil, cfg, log)

	// Create a handler that sets tenant context then calls Stream.
	// Since Stream will try to subscribe to Redis (which is nil), it will panic
	// or fail after setting headers. We wrap it to capture headers before the
	// Redis call by just verifying the auth-gated path sets headers.
	//
	// Instead, we test header logic indirectly: when auth IS present but Redis
	// is nil, the handler will set headers then panic on Redis subscribe.
	// We use recover to capture headers after they are set.

	router := gin.New()
	router.GET("/v1/events/stream", func(c *gin.Context) {
		// Set tenant context to pass auth check
		c.Set(string(middleware.ContextKeyTenantID), "test-tenant-123")

		// Wrap in a deferred recover since Redis client is nil
		defer func() {
			recover()
		}()
		handler.Stream(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify SSE headers were set
	expectedHeaders := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}

	for header, expected := range expectedHeaders {
		got := w.Header().Get(header)
		if got != expected {
			t.Errorf("expected header %s=%q, got %q", header, expected, got)
		}
	}
}

func TestSSEHandler_MatchesFilter(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewSSEHandler(nil, nil, config.SSEConfig{}, log)

	tests := []struct {
		name        string
		payload     string
		typeFilters []string
		expected    bool
	}{
		{
			name:        "no filters matches everything",
			payload:     `{"event_type":"order.created"}`,
			typeFilters: nil,
			expected:    true,
		},
		{
			name:        "exact match in payload",
			payload:     `{"event_type":"order.created","data":{}}`,
			typeFilters: []string{"order.created"},
			expected:    true,
		},
		{
			name:        "glob match with wildcard",
			payload:     `{"event_type":"order.created","data":{}}`,
			typeFilters: []string{"order.*"},
			expected:    true,
		},
		{
			name:        "no match",
			payload:     `{"event_type":"task.completed","data":{}}`,
			typeFilters: []string{"order.created"},
			expected:    false,
		},
		{
			name:        "multiple filters one matches",
			payload:     `{"event_type":"task.completed","data":{}}`,
			typeFilters: []string{"order.created", "task.completed"},
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.matchesFilter(tt.payload, tt.typeFilters)
			if got != tt.expected {
				t.Errorf("matchesFilter(%q, %v) = %v, want %v", tt.payload, tt.typeFilters, got, tt.expected)
			}
		})
	}
}

func TestSSEHandler_MatchesFilterByType(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewSSEHandler(nil, nil, config.SSEConfig{}, log)

	tests := []struct {
		name        string
		eventType   string
		typeFilters []string
		expected    bool
	}{
		{
			name:        "no filters matches everything",
			eventType:   "order.created",
			typeFilters: nil,
			expected:    true,
		},
		{
			name:        "exact match",
			eventType:   "order.created",
			typeFilters: []string{"order.created"},
			expected:    true,
		},
		{
			name:        "glob match",
			eventType:   "order.created",
			typeFilters: []string{"order.*"},
			expected:    true,
		},
		{
			name:        "glob does not match different prefix",
			eventType:   "task.completed",
			typeFilters: []string{"order.*"},
			expected:    false,
		},
		{
			name:        "no match",
			eventType:   "task.completed",
			typeFilters: []string{"order.created"},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.matchesFilterByType(tt.eventType, tt.typeFilters)
			if got != tt.expected {
				t.Errorf("matchesFilterByType(%q, %v) = %v, want %v", tt.eventType, tt.typeFilters, got, tt.expected)
			}
		})
	}
}
