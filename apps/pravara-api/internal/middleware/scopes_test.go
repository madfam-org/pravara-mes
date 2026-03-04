package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRequireScope_JWTAuthMethod(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAuthMethod), "jwt")
		c.Next()
	})
	router.Use(RequireScope("feeds:read"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "access granted"})
	})

	// Test: JWT users always pass scope checks
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "access granted")
}

func TestRequireScope_APIKeyWithMatchingScope(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAuthMethod), "apikey")
		c.Set(string(ContextKeyScopes), []string{"feeds:read", "events:write"})
		c.Next()
	})
	router.Use(RequireScope("feeds:read"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "access granted"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "access granted")
}

func TestRequireScope_APIKeyWithWildcardScope(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAuthMethod), "apikey")
		c.Set(string(ContextKeyScopes), []string{"*"})
		c.Next()
	})
	router.Use(RequireScope("orders:delete"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "access granted"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "access granted")
}

func TestRequireScope_APIKeyWithoutMatchingScope(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAuthMethod), "apikey")
		c.Set(string(ContextKeyScopes), []string{"feeds:read", "events:read"})
		c.Next()
	})
	router.Use(RequireScope("orders:write"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "access granted"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "Missing required scope: orders:write")
}

func TestRequireScope_NoAuthMethodSet(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()
	// No auth middleware sets ContextKeyAuthMethod or ContextKeyScopes
	router.Use(RequireScope("feeds:read"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "access granted"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "Insufficient permissions")
}

func TestRequireScope_TableDriven(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		authMethod     string
		scopes         []string
		setAuthMethod  bool
		setScopes      bool
		requiredScope  string
		expectedStatus int
	}{
		{
			name:           "JWT bypasses scope check",
			authMethod:     "jwt",
			setAuthMethod:  true,
			setScopes:      false,
			requiredScope:  "admin:everything",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "API key exact scope match",
			authMethod:     "apikey",
			scopes:         []string{"feeds:read"},
			setAuthMethod:  true,
			setScopes:      true,
			requiredScope:  "feeds:read",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "API key wildcard scope",
			authMethod:     "apikey",
			scopes:         []string{"*"},
			setAuthMethod:  true,
			setScopes:      true,
			requiredScope:  "anything:here",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "API key missing scope",
			authMethod:     "apikey",
			scopes:         []string{"feeds:read"},
			setAuthMethod:  true,
			setScopes:      true,
			requiredScope:  "orders:write",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "No auth method set",
			setAuthMethod:  false,
			setScopes:      false,
			requiredScope:  "feeds:read",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "API key empty scopes list",
			authMethod:     "apikey",
			scopes:         []string{},
			setAuthMethod:  true,
			setScopes:      true,
			requiredScope:  "feeds:read",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tt.setAuthMethod {
					c.Set(string(ContextKeyAuthMethod), tt.authMethod)
				}
				if tt.setScopes {
					c.Set(string(ContextKeyScopes), tt.scopes)
				}
				c.Next()
			})
			router.Use(RequireScope(tt.requiredScope))
			router.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "access granted"})
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}
