package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
)

// tokenVerifier interface for testing
type tokenVerifier interface {
	VerifyToken(ctx context.Context, token string) (*auth.Claims, error)
}

// tenantDB interface for testing
type tenantDB interface {
	SetTenantID(tenantID string) error
	ClearTenantID() error
}

// mockOIDCVerifier is a mock implementation of tokenVerifier
type mockOIDCVerifier struct {
	mock.Mock
}

func (m *mockOIDCVerifier) VerifyToken(ctx context.Context, token string) (*auth.Claims, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.Claims), args.Error(1)
}

// mockDB is a mock implementation of tenantDB
type mockDB struct {
	mock.Mock
}

func (m *mockDB) SetTenantID(tenantID string) error {
	args := m.Called(tenantID)
	return args.Error(0)
}

func (m *mockDB) ClearTenantID() error {
	args := m.Called()
	return args.Error(0)
}

// authMiddlewareForTest creates middleware with injectable interfaces for testing
func authMiddlewareForTest(verifier tokenVerifier, database tenantDB, log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract token from header
		token, err := auth.ExtractBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			log.WithError(err).Debug("Failed to extract bearer token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Missing or invalid authorization header",
			})
			return
		}

		// Verify token
		claims, err := verifier.VerifyToken(c.Request.Context(), token)
		if err != nil {
			log.WithError(err).Debug("Failed to verify token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Invalid or expired token",
			})
			return
		}

		// Validate tenant ID exists
		if claims.TenantID == "" {
			log.Warn("Token missing tenant_id claim")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "Token does not contain tenant information",
			})
			return
		}

		// Set tenant ID in database session for RLS
		if err := database.SetTenantID(claims.TenantID); err != nil {
			log.WithError(err).Error("Failed to set tenant ID in database")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to establish tenant context",
			})
			return
		}

		// Store claims in context
		c.Set(string(ContextKeyClaims), claims)
		c.Set(string(ContextKeyTenantID), claims.TenantID)
		c.Set(string(ContextKeyUserID), claims.Subject)

		c.Next()

		// Clean up tenant context after request
		if err := database.ClearTenantID(); err != nil {
			log.WithError(err).Warn("Failed to clear tenant ID from database")
		}
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	expectedClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "tenant-abc",
		Email:    "user@example.com",
		Name:     "Test User",
		Roles:    []string{"admin", "operator"},
	}

	mockVerifier.On("VerifyToken", mock.Anything, "valid-token").Return(expectedClaims, nil)
	mockDatabase.On("SetTenantID", "tenant-abc").Return(nil)
	mockDatabase.On("ClearTenantID").Return(nil)

	router := gin.New()
	router.Use(authMiddlewareForTest(mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		claims, exists := GetClaims(c)
		assert.True(t, exists)
		assert.Equal(t, "tenant-abc", claims.TenantID)
		assert.Equal(t, "user-123", claims.Subject)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	mockVerifier.AssertExpectations(t)
	mockDatabase.AssertExpectations(t)
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	router := gin.New()
	router.Use(authMiddlewareForTest(mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test: No Authorization header
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "Missing or invalid authorization header")
}

func TestAuthMiddleware_InvalidFormat(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	router := gin.New()
	router.Use(authMiddlewareForTest(mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	tests := []struct {
		name   string
		header string
	}{
		{
			name:   "No Bearer prefix",
			header: "token-without-bearer",
		},
		{
			name:   "Wrong prefix",
			header: "Basic dXNlcjpwYXNz",
		},
		{
			name:   "Only Bearer",
			header: "Bearer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", tt.header)
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "Missing or invalid authorization header")
		})
	}
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	mockVerifier.On("VerifyToken", mock.Anything, "expired-token").
		Return(nil, fmt.Errorf("token is expired"))

	router := gin.New()
	router.Use(authMiddlewareForTest(mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid or expired token")
	mockVerifier.AssertExpectations(t)
}

func TestAuthMiddleware_MissingTenantID(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	claimsWithoutTenant := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "", // Missing tenant ID
		Email:    "user@example.com",
		Name:     "Test User",
		Roles:    []string{"admin"},
	}

	mockVerifier.On("VerifyToken", mock.Anything, "no-tenant-token").Return(claimsWithoutTenant, nil)

	router := gin.New()
	router.Use(authMiddlewareForTest(mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer no-tenant-token")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "Token does not contain tenant information")
	mockVerifier.AssertExpectations(t)
}

func TestAuthMiddleware_TenantContextSet(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	expectedClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-456",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "tenant-xyz",
		Email:    "test@example.com",
		Name:     "Test User",
		Roles:    []string{"operator"},
	}

	mockVerifier.On("VerifyToken", mock.Anything, "valid-token").Return(expectedClaims, nil)
	mockDatabase.On("SetTenantID", "tenant-xyz").Return(nil)
	mockDatabase.On("ClearTenantID").Return(nil)

	router := gin.New()
	router.Use(authMiddlewareForTest(mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		// Verify context values are set
		tenantID, exists := GetTenantID(c)
		assert.True(t, exists)
		assert.Equal(t, "tenant-xyz", tenantID)

		userID, exists := GetUserID(c)
		assert.True(t, exists)
		assert.Equal(t, "user-456", userID)

		c.JSON(http.StatusOK, gin.H{"tenant": tenantID, "user": userID})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "tenant-xyz")
	assert.Contains(t, w.Body.String(), "user-456")
	mockVerifier.AssertExpectations(t)
	mockDatabase.AssertExpectations(t)
}

func TestGetClaims_Success(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	expectedClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-789",
		},
		TenantID: "tenant-123",
		Email:    "claims@example.com",
		Roles:    []string{"viewer"},
	}

	// Set claims in context
	c.Set(string(ContextKeyClaims), expectedClaims)

	// Test
	claims, exists := GetClaims(c)

	// Assert
	assert.True(t, exists)
	assert.NotNil(t, claims)
	assert.Equal(t, "user-789", claims.Subject)
	assert.Equal(t, "tenant-123", claims.TenantID)
	assert.Equal(t, "claims@example.com", claims.Email)
	assert.Equal(t, []string{"viewer"}, claims.Roles)
}

func TestGetTenantID_Success(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Set tenant ID in context
	c.Set(string(ContextKeyTenantID), "tenant-abc")

	// Test
	tenantID, exists := GetTenantID(c)

	// Assert
	assert.True(t, exists)
	assert.Equal(t, "tenant-abc", tenantID)

	// Test when not set
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	tenantID2, exists2 := GetTenantID(c2)

	assert.False(t, exists2)
	assert.Equal(t, "", tenantID2)
}

func TestRequireRole_HasRole(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()

	// Set up middleware chain
	router.Use(func(c *gin.Context) {
		// Simulate auth middleware setting claims
		claims := &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "user-admin",
			},
			TenantID: "tenant-123",
			Roles:    []string{"admin", "operator"},
		}
		c.Set(string(ContextKeyClaims), claims)
		c.Next()
	})

	router.Use(RequireRole("admin"))
	router.GET("/admin", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "admin access granted"})
	})

	// Test: User has admin role
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin", nil)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "admin access granted")
}

func TestRequireRole_MissingRole(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		userRoles     []string
		requiredRoles []string
		shouldPass    bool
	}{
		{
			name:          "User has required role",
			userRoles:     []string{"admin", "operator"},
			requiredRoles: []string{"operator"},
			shouldPass:    true,
		},
		{
			name:          "User missing required role",
			userRoles:     []string{"viewer"},
			requiredRoles: []string{"admin"},
			shouldPass:    false,
		},
		{
			name:          "User has one of multiple required roles",
			userRoles:     []string{"operator"},
			requiredRoles: []string{"admin", "operator"},
			shouldPass:    true,
		},
		{
			name:          "User has no required roles",
			userRoles:     []string{"viewer"},
			requiredRoles: []string{"admin", "operator"},
			shouldPass:    false,
		},
		{
			name:          "User has no roles",
			userRoles:     []string{},
			requiredRoles: []string{"admin"},
			shouldPass:    false,
		},
		{
			name:          "No claims in context",
			userRoles:     nil,
			requiredRoles: []string{"admin"},
			shouldPass:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()

			// Set up middleware chain
			router.Use(func(c *gin.Context) {
				if tt.userRoles != nil {
					claims := &auth.Claims{
						RegisteredClaims: jwt.RegisteredClaims{
							Subject: "user-test",
						},
						TenantID: "tenant-123",
						Roles:    tt.userRoles,
					}
					c.Set(string(ContextKeyClaims), claims)
				}
				c.Next()
			})

			router.Use(RequireRole(tt.requiredRoles...))
			router.GET("/protected", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "access granted"})
			})

			// Test
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/protected", nil)
			router.ServeHTTP(w, req)

			// Assert
			if tt.shouldPass {
				assert.Equal(t, http.StatusOK, w.Code)
				assert.Contains(t, w.Body.String(), "access granted")
			} else {
				if tt.userRoles == nil {
					assert.Equal(t, http.StatusUnauthorized, w.Code)
					assert.Contains(t, w.Body.String(), "Authentication required")
				} else {
					assert.Equal(t, http.StatusForbidden, w.Code)
					assert.Contains(t, w.Body.String(), "Insufficient permissions")
				}
			}
		})
	}
}
