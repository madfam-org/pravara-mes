package middleware

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// apiKeyRepo interface for testing
type apiKeyRepo interface {
	GetByHash(ctx context.Context, keyHash string) (*repositories.APIKey, error)
	UpdateLastUsed(ctx context.Context, id uuid.UUID) error
}

// mockAPIKeyRepo is a mock implementation of apiKeyRepo
type mockAPIKeyRepo struct {
	mock.Mock
}

func (m *mockAPIKeyRepo) GetByHash(ctx context.Context, keyHash string) (*repositories.APIKey, error) {
	args := m.Called(ctx, keyHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repositories.APIKey), args.Error(1)
}

func (m *mockAPIKeyRepo) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// apiKeyOrJWTMiddlewareForTest creates the dual auth middleware with injectable interfaces.
func apiKeyOrJWTMiddlewareForTest(repo apiKeyRepo, verifier tokenVerifier, database tenantDB, log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for API key in X-API-Key header
		apiKey := c.GetHeader("X-API-Key")

		// Also check Authorization header for Bearer prv_... pattern
		if apiKey == "" {
			authHeader := c.GetHeader("Authorization")
			if len(authHeader) > 7 && authHeader[:7] == "Bearer " && len(authHeader) > 11 && authHeader[7:11] == APIKeyPrefix {
				apiKey = authHeader[7:]
			}
		}

		if apiKey != "" && len(apiKey) >= 4 && apiKey[:4] == APIKeyPrefix {
			// API key authentication path
			hash := sha256.Sum256([]byte(apiKey))
			keyHash := fmt.Sprintf("%x", hash)

			key, err := repo.GetByHash(c.Request.Context(), keyHash)
			if err != nil {
				log.WithError(err).Error("Failed to look up API key")
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":   "internal_error",
					"message": "Failed to validate API key",
				})
				return
			}

			if key == nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error":   "unauthorized",
					"message": "Invalid API key",
				})
				return
			}

			if !key.IsActive {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error":   "unauthorized",
					"message": "API key has been revoked",
				})
				return
			}

			if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error":   "unauthorized",
					"message": "API key has expired",
				})
				return
			}

			if err := database.SetTenantID(key.TenantID.String()); err != nil {
				log.WithError(err).Error("Failed to set tenant ID in database")
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":   "internal_error",
					"message": "Failed to establish tenant context",
				})
				return
			}

			c.Set(string(ContextKeyTenantID), key.TenantID.String())
			c.Set(string(ContextKeyUserID), "apikey:"+key.ID.String())
			c.Set(string(ContextKeyScopes), key.Scopes)
			c.Set(string(ContextKeyAuthMethod), "apikey")

			c.Next()

			if err := database.ClearTenantID(); err != nil {
				log.WithError(err).Warn("Failed to clear tenant ID from database")
			}
			return
		}

		// Fall back to JWT authentication
		token, err := auth.ExtractBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			log.WithError(err).Debug("Failed to extract bearer token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Missing or invalid authorization header",
			})
			return
		}

		claims, err := verifier.VerifyToken(c.Request.Context(), token)
		if err != nil {
			log.WithError(err).Debug("Failed to verify token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Invalid or expired token",
			})
			return
		}

		if claims.TenantID == "" {
			log.Warn("Token missing tenant_id claim")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "Token does not contain tenant information",
			})
			return
		}

		if err := database.SetTenantID(claims.TenantID); err != nil {
			log.WithError(err).Error("Failed to set tenant ID in database")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to establish tenant context",
			})
			return
		}

		c.Set(string(ContextKeyClaims), claims)
		c.Set(string(ContextKeyTenantID), claims.TenantID)
		c.Set(string(ContextKeyUserID), claims.Subject)
		c.Set(string(ContextKeyAuthMethod), "jwt")

		c.Next()

		if err := database.ClearTenantID(); err != nil {
			log.WithError(err).Warn("Failed to clear tenant ID from database")
		}
	}
}

func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)
	return logger
}

func hashAPIKey(rawKey string) string {
	hash := sha256.Sum256([]byte(rawKey))
	return fmt.Sprintf("%x", hash)
}

func TestAPIKeyOrJWT_ValidAPIKeyInHeader(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := newTestLogger()

	mockRepo := new(mockAPIKeyRepo)
	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	tenantID := uuid.New()
	keyID := uuid.New()
	rawKey := "prv_test1234567890abcdef"
	keyHash := hashAPIKey(rawKey)

	apiKey := &repositories.APIKey{
		ID:       keyID,
		TenantID: tenantID,
		Name:     "test-key",
		KeyHash:  keyHash,
		Scopes:   []string{"feeds:read", "events:write"},
		IsActive: true,
	}

	mockRepo.On("GetByHash", mock.Anything, keyHash).Return(apiKey, nil)
	mockDatabase.On("SetTenantID", tenantID.String()).Return(nil)
	mockDatabase.On("ClearTenantID").Return(nil)

	router := gin.New()
	router.Use(apiKeyOrJWTMiddlewareForTest(mockRepo, mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		tid, exists := GetTenantID(c)
		assert.True(t, exists)
		assert.Equal(t, tenantID.String(), tid)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	mockRepo.AssertExpectations(t)
	mockDatabase.AssertExpectations(t)
}

func TestAPIKeyOrJWT_ValidAPIKeyViaBearerPrefix(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := newTestLogger()

	mockRepo := new(mockAPIKeyRepo)
	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	tenantID := uuid.New()
	keyID := uuid.New()
	rawKey := "prv_bearer_key_abc123"
	keyHash := hashAPIKey(rawKey)

	apiKey := &repositories.APIKey{
		ID:       keyID,
		TenantID: tenantID,
		Name:     "bearer-key",
		KeyHash:  keyHash,
		Scopes:   []string{"*"},
		IsActive: true,
	}

	mockRepo.On("GetByHash", mock.Anything, keyHash).Return(apiKey, nil)
	mockDatabase.On("SetTenantID", tenantID.String()).Return(nil)
	mockDatabase.On("ClearTenantID").Return(nil)

	router := gin.New()
	router.Use(apiKeyOrJWTMiddlewareForTest(mockRepo, mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		tid, exists := GetTenantID(c)
		assert.True(t, exists)
		assert.Equal(t, tenantID.String(), tid)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	mockRepo.AssertExpectations(t)
	mockDatabase.AssertExpectations(t)
}

func TestAPIKeyOrJWT_ExpiredAPIKey(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := newTestLogger()

	mockRepo := new(mockAPIKeyRepo)
	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	rawKey := "prv_expired_key_999"
	keyHash := hashAPIKey(rawKey)
	expired := time.Now().Add(-24 * time.Hour)

	apiKey := &repositories.APIKey{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Name:      "expired-key",
		KeyHash:   keyHash,
		Scopes:    []string{"feeds:read"},
		IsActive:  true,
		ExpiresAt: &expired,
	}

	mockRepo.On("GetByHash", mock.Anything, keyHash).Return(apiKey, nil)

	router := gin.New()
	router.Use(apiKeyOrJWTMiddlewareForTest(mockRepo, mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "API key has expired")
	mockRepo.AssertExpectations(t)
}

func TestAPIKeyOrJWT_InactiveAPIKey(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := newTestLogger()

	mockRepo := new(mockAPIKeyRepo)
	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	rawKey := "prv_revoked_key_abc"
	keyHash := hashAPIKey(rawKey)

	apiKey := &repositories.APIKey{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Name:     "revoked-key",
		KeyHash:  keyHash,
		Scopes:   []string{"feeds:read"},
		IsActive: false,
	}

	mockRepo.On("GetByHash", mock.Anything, keyHash).Return(apiKey, nil)

	router := gin.New()
	router.Use(apiKeyOrJWTMiddlewareForTest(mockRepo, mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "API key has been revoked")
	mockRepo.AssertExpectations(t)
}

func TestAPIKeyOrJWT_InvalidUnknownAPIKey(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := newTestLogger()

	mockRepo := new(mockAPIKeyRepo)
	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	rawKey := "prv_unknown_key_xyz"
	keyHash := hashAPIKey(rawKey)

	mockRepo.On("GetByHash", mock.Anything, keyHash).Return(nil, nil)

	router := gin.New()
	router.Use(apiKeyOrJWTMiddlewareForTest(mockRepo, mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid API key")
	mockRepo.AssertExpectations(t)
}

func TestAPIKeyOrJWT_NoAPIKey_ValidJWT(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := newTestLogger()

	mockRepo := new(mockAPIKeyRepo)
	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	expectedClaims := &auth.Claims{
		TenantID: "tenant-jwt-123",
	}
	expectedClaims.Subject = "user-jwt-456"

	mockVerifier.On("VerifyToken", mock.Anything, "valid-jwt-token").Return(expectedClaims, nil)
	mockDatabase.On("SetTenantID", "tenant-jwt-123").Return(nil)
	mockDatabase.On("ClearTenantID").Return(nil)

	router := gin.New()
	router.Use(apiKeyOrJWTMiddlewareForTest(mockRepo, mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		tid, exists := GetTenantID(c)
		assert.True(t, exists)
		assert.Equal(t, "tenant-jwt-123", tid)

		authMethod, _ := c.Get(string(ContextKeyAuthMethod))
		assert.Equal(t, "jwt", authMethod)

		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-jwt-token")
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	mockVerifier.AssertExpectations(t)
	mockDatabase.AssertExpectations(t)
}

func TestAPIKeyOrJWT_NoAPIKey_NoJWT(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := newTestLogger()

	mockRepo := new(mockAPIKeyRepo)
	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	router := gin.New()
	router.Use(apiKeyOrJWTMiddlewareForTest(mockRepo, mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test: No headers at all
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "Missing or invalid authorization header")
}

func TestAPIKeyOrJWT_SetsCorrectScopes(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := newTestLogger()

	mockRepo := new(mockAPIKeyRepo)
	mockVerifier := new(mockOIDCVerifier)
	mockDatabase := new(mockDB)

	tenantID := uuid.New()
	keyID := uuid.New()
	rawKey := "prv_scoped_key_test"
	keyHash := hashAPIKey(rawKey)
	expectedScopes := []string{"feeds:read", "events:write", "orders:read"}

	apiKey := &repositories.APIKey{
		ID:       keyID,
		TenantID: tenantID,
		Name:     "scoped-key",
		KeyHash:  keyHash,
		Scopes:   expectedScopes,
		IsActive: true,
	}

	mockRepo.On("GetByHash", mock.Anything, keyHash).Return(apiKey, nil)
	mockDatabase.On("SetTenantID", tenantID.String()).Return(nil)
	mockDatabase.On("ClearTenantID").Return(nil)

	router := gin.New()
	router.Use(apiKeyOrJWTMiddlewareForTest(mockRepo, mockVerifier, mockDatabase, logger))
	router.GET("/test", func(c *gin.Context) {
		scopesVal, exists := c.Get(string(ContextKeyScopes))
		assert.True(t, exists)
		scopes, ok := scopesVal.([]string)
		assert.True(t, ok)
		assert.Equal(t, expectedScopes, scopes)

		authMethod, _ := c.Get(string(ContextKeyAuthMethod))
		assert.Equal(t, "apikey", authMethod)

		userID, exists := GetUserID(c)
		assert.True(t, exists)
		assert.Equal(t, "apikey:"+keyID.String(), userID)

		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	mockRepo.AssertExpectations(t)
	mockDatabase.AssertExpectations(t)
}
