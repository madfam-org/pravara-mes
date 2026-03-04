package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
)

func TestCORSMiddleware_StatusRouteWildcard(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://pravara.madfam.io"},
		StatusPublic:   true,
	}

	router := gin.New()
	router.Use(CORSMiddleware(cfg))
	router.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/status", nil)
	req.Header.Set("Origin", "https://random-site.com")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "", w.Header().Get("Vary")) // No Vary for wildcard
}

func TestCORSMiddleware_FeedsAllowedOrigin(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://pravara.madfam.io"},
		StatusPublic:   true,
	}

	router := gin.New()
	router.Use(CORSMiddleware(cfg))
	router.GET("/v1/feeds/abc", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": "feed"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/feeds/abc", nil)
	req.Header.Set("Origin", "https://pravara.madfam.io")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "https://pravara.madfam.io", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PATCH, DELETE, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Authorization, Content-Type, X-API-Key", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "X-Total-Count", w.Header().Get("Access-Control-Expose-Headers"))
	assert.Equal(t, "86400", w.Header().Get("Access-Control-Max-Age"))
	assert.Equal(t, "Origin", w.Header().Get("Vary"))
}

func TestCORSMiddleware_FeedsDisallowedOrigin(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://pravara.madfam.io"},
		StatusPublic:   true,
	}

	router := gin.New()
	router.Use(CORSMiddleware(cfg))
	router.GET("/v1/feeds/abc", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": "feed"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/feeds/abc", nil)
	req.Header.Set("Origin", "https://evil-site.com")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORSMiddleware_EventsAllowedOrigin(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://mes-app.madfam.io"},
		StatusPublic:   true,
	}

	router := gin.New()
	router.Use(CORSMiddleware(cfg))
	router.GET("/v1/events/stream", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": "events"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/events/stream", nil)
	req.Header.Set("Origin", "https://mes-app.madfam.io")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "https://mes-app.madfam.io", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", w.Header().Get("Vary"))
}

func TestCORSMiddleware_OptionsPreflightRequest(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://pravara.madfam.io"},
		StatusPublic:   true,
	}

	router := gin.New()
	router.Use(CORSMiddleware(cfg))
	router.OPTIONS("/v1/feeds/abc", func(c *gin.Context) {
		// This handler should not be reached; the middleware handles OPTIONS
		c.JSON(http.StatusOK, gin.H{"message": "should not reach"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/v1/feeds/abc", nil)
	req.Header.Set("Origin", "https://pravara.madfam.io")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "https://pravara.madfam.io", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PATCH, DELETE, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORSMiddleware_StatusPublicFalse(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://pravara.madfam.io"},
		StatusPublic:   false,
	}

	router := gin.New()
	router.Use(CORSMiddleware(cfg))
	router.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/status", nil)
	req.Header.Set("Origin", "https://random-site.com")
	router.ServeHTTP(w, req)

	// Assert: No wildcard CORS when StatusPublic is false
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSMiddleware_NoOriginHeader(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://pravara.madfam.io"},
		StatusPublic:   true,
	}

	router := gin.New()
	router.Use(CORSMiddleware(cfg))
	router.GET("/v1/feeds/abc", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": "feed"})
	})

	// Test: No Origin header (same-origin or non-browser request)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/feeds/abc", nil)
	router.ServeHTTP(w, req)

	// Assert: No CORS headers set
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSMiddleware_WildcardSubdomain(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	cfg := config.CORSConfig{
		AllowedOrigins: []string{"*.madfam.io"},
		StatusPublic:   true,
	}

	router := gin.New()
	router.Use(CORSMiddleware(cfg))
	router.GET("/v1/feeds/abc", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": "feed"})
	})

	tests := []struct {
		name          string
		origin        string
		expectAllowed bool
	}{
		{
			name:          "Subdomain match",
			origin:        "https://app.madfam.io",
			expectAllowed: true,
		},
		{
			name:          "Another subdomain match",
			origin:        "https://admin.madfam.io",
			expectAllowed: true,
		},
		{
			name:          "Non-matching domain",
			origin:        "https://evil-site.com",
			expectAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/v1/feeds/abc", nil)
			req.Header.Set("Origin", tt.origin)
			router.ServeHTTP(w, req)

			if tt.expectAllowed {
				assert.Equal(t, tt.origin, w.Header().Get("Access-Control-Allow-Origin"))
			} else {
				assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}
