// Package middleware provides HTTP middleware for the PravaraMES API.
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

// ContextKey is a type for context keys.
type ContextKey string

const (
	// ContextKeyClaims is the context key for JWT claims.
	ContextKeyClaims ContextKey = "claims"
	// ContextKeyTenantID is the context key for tenant ID.
	ContextKeyTenantID ContextKey = "tenant_id"
	// ContextKeyUserID is the context key for user ID.
	ContextKeyUserID ContextKey = "user_id"
)

// AuthMiddleware creates middleware that validates JWT tokens.
func AuthMiddleware(verifier *auth.OIDCVerifier, database *db.DB, log *logrus.Logger) gin.HandlerFunc {
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

// GetClaims retrieves JWT claims from the Gin context.
func GetClaims(c *gin.Context) (*auth.Claims, bool) {
	claims, exists := c.Get(string(ContextKeyClaims))
	if !exists {
		return nil, false
	}
	return claims.(*auth.Claims), true
}

// GetTenantID retrieves the tenant ID from the Gin context.
func GetTenantID(c *gin.Context) (string, bool) {
	tenantID, exists := c.Get(string(ContextKeyTenantID))
	if !exists {
		return "", false
	}
	return tenantID.(string), true
}

// GetUserID retrieves the user ID from the Gin context.
func GetUserID(c *gin.Context) (string, bool) {
	userID, exists := c.Get(string(ContextKeyUserID))
	if !exists {
		return "", false
	}
	return userID.(string), true
}

// RequireRole creates middleware that requires specific roles.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := GetClaims(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Authentication required",
			})
			return
		}

		// Check if user has any of the required roles
		hasRole := false
		for _, required := range roles {
			for _, userRole := range claims.Roles {
				if userRole == required {
					hasRole = true
					break
				}
			}
			if hasRole {
				break
			}
		}

		if !hasRole {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "Insufficient permissions",
			})
			return
		}

		c.Next()
	}
}
