package middleware

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

const (
	// ContextKeyScopes is the context key for API key scopes.
	ContextKeyScopes ContextKey = "scopes"
	// ContextKeyAuthMethod is the context key for the authentication method used.
	ContextKeyAuthMethod ContextKey = "auth_method"
	// APIKeyPrefix is the prefix for Pravara API keys.
	APIKeyPrefix = "prv_"
)

// APIKeyOrJWTMiddleware creates middleware that authenticates via API key or JWT.
// API keys are checked first (X-API-Key header or Bearer prv_... prefix detection).
// Falls back to JWT verification if no API key is found.
func APIKeyOrJWTMiddleware(apikeyRepo *repositories.APIKeyRepository, verifier *auth.OIDCVerifier, database *db.DB, log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for API key in X-API-Key header
		apiKey := c.GetHeader("X-API-Key")

		// Also check Authorization header for Bearer prv_... pattern
		if apiKey == "" {
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(authHeader, "Bearer "+APIKeyPrefix) {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if apiKey != "" && strings.HasPrefix(apiKey, APIKeyPrefix) {
			// API key authentication path
			handleAPIKeyAuth(c, apiKey, apikeyRepo, database, log)
			return
		}

		// Fall back to JWT authentication (existing behavior)
		handleJWTAuth(c, verifier, database, log)
	}
}

func handleAPIKeyAuth(c *gin.Context, rawKey string, apikeyRepo *repositories.APIKeyRepository, database *db.DB, log *logrus.Logger) {
	// Hash the key for lookup
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := fmt.Sprintf("%x", hash)

	// Look up the key
	key, err := apikeyRepo.GetByHash(c.Request.Context(), keyHash)
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

	// Check if key is active
	if !key.IsActive {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "API key has been revoked",
		})
		return
	}

	// Check expiration
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "API key has expired",
		})
		return
	}

	// Set tenant context for RLS
	if err := database.SetTenantID(key.TenantID.String()); err != nil {
		log.WithError(err).Error("Failed to set tenant ID in database")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to establish tenant context",
		})
		return
	}

	// Store auth info in context
	c.Set(string(ContextKeyTenantID), key.TenantID.String())
	c.Set(string(ContextKeyUserID), "apikey:"+key.ID.String())
	c.Set(string(ContextKeyScopes), key.Scopes)
	c.Set(string(ContextKeyAuthMethod), "apikey")

	// Update last used (fire and forget)
	go func() {
		_ = apikeyRepo.UpdateLastUsed(c.Request.Context(), key.ID)
	}()

	c.Next()

	// Clean up tenant context after request
	if err := database.ClearTenantID(); err != nil {
		log.WithError(err).Warn("Failed to clear tenant ID from database")
	}
}

func handleJWTAuth(c *gin.Context, verifier *auth.OIDCVerifier, database *db.DB, log *logrus.Logger) {
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
	c.Set(string(ContextKeyAuthMethod), "jwt")

	c.Next()

	// Clean up tenant context after request
	if err := database.ClearTenantID(); err != nil {
		log.WithError(err).Warn("Failed to clear tenant ID from database")
	}
}
