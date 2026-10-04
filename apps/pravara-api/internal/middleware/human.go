package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireHumanCaller admits only people signed in through Janua. Machine
// credentials are refused even when they hold the wildcard scope: use it on
// routes where a person must take the decision (edge-node enrollment
// approval and credential revocation).
func RequireHumanCaller() gin.HandlerFunc {
	return func(c *gin.Context) {
		caller, ok := GetCaller(c)
		if !ok || caller.Kind != CallerUser {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"code":    "human_only",
				"message": "This action must be taken by a person signed in through SSO",
			})
			return
		}
		c.Next()
	}
}
