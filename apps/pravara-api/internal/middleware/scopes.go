package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireScope creates middleware that requires a specific scope.
// JWT users get all scopes implicitly. API key users are checked against their scopes field.
func RequireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authMethod, _ := c.Get(string(ContextKeyAuthMethod))

		// JWT users get all scopes implicitly
		if authMethod == "jwt" {
			c.Next()
			return
		}

		// API key users must have the required scope
		scopesVal, exists := c.Get(string(ContextKeyScopes))
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "Insufficient permissions",
			})
			return
		}

		scopes, ok := scopesVal.([]string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "Insufficient permissions",
			})
			return
		}

		// Check for wildcard or specific scope
		for _, s := range scopes {
			if s == "*" || s == scope {
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error":   "forbidden",
			"message": "Missing required scope: " + scope,
		})
	}
}
