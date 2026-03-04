package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
)

// CORSMiddleware applies CORS headers based on route patterns.
// - /status* routes: allow all origins (public health data)
// - /v1/feeds/* and /v1/events/*: configurable allowed origins
// - All other /v1/*: no CORS by default (or configurable)
func CORSMiddleware(cfg config.CORSConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		origin := c.GetHeader("Origin")

		if origin == "" {
			c.Next()
			return
		}

		var allowOrigin string

		if strings.HasPrefix(path, "/status") {
			// Public status endpoints — allow all origins
			if cfg.StatusPublic {
				allowOrigin = "*"
			}
		} else if strings.HasPrefix(path, "/v1/feeds/") ||
			strings.HasPrefix(path, "/v1/events/") {
			// Consumer endpoints — configurable origins
			if isOriginAllowed(origin, cfg.AllowedOrigins) {
				allowOrigin = origin
			}
		} else if strings.HasPrefix(path, "/v1/") {
			// Other API endpoints — configurable origins
			if isOriginAllowed(origin, cfg.AllowedOrigins) {
				allowOrigin = origin
			}
		}

		if allowOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowOrigin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
			c.Header("Access-Control-Expose-Headers", "X-Total-Count")
			c.Header("Access-Control-Max-Age", "86400")

			if allowOrigin != "*" {
				c.Header("Vary", "Origin")
			}
		}

		// Handle preflight
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
		// Support wildcard subdomain matching: *.madfam.io
		if strings.HasPrefix(allowed, "*.") {
			domain := allowed[1:] // ".madfam.io"
			if strings.HasSuffix(origin, domain) ||
				origin == "https://"+allowed[2:] ||
				origin == "http://"+allowed[2:] {
				return true
			}
		}
	}
	return false
}
