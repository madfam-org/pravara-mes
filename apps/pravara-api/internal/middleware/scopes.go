package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Machine scopes. Janua reserves the pravara-mes:* names, so only a platform
// admin can put them on a client; the `pravara-mes:admin` precedent sets the
// namespace.
const (
	// ScopeJobs: order and job intake (orders, order items, design import,
	// quoting webhooks).
	ScopeJobs = "pravara-mes:jobs"
	// ScopeNodes: producer-node registry, heartbeats and telemetry.
	ScopeNodes = "pravara-mes:nodes"
	// ScopePassports: passport and genealogy writes.
	ScopePassports = "pravara-mes:passports"
	// ScopeRead: read-only access to the machine-facing resources.
	ScopeRead = "pravara-mes:read"

	// ScopeWildcard grants every scope. API keys only: it is never honoured
	// on a Janua token.
	ScopeWildcard = "*"

	// Legacy API-key scope names, still honoured for API keys on the routes
	// they always covered. Never honoured on a Janua token.
	LegacyScopeReadEvents = "read:events"
	LegacyScopeReadFeeds  = "read:feeds"
	LegacyScopeReadStatus = "read:status"
)

// MachineScopes are the scopes a Janua machine token may carry for this API.
var MachineScopes = []string{ScopeJobs, ScopeNodes, ScopePassports, ScopeRead}

// APIKeyScopes are the scopes an API key may be issued with.
var APIKeyScopes = []string{
	ScopeJobs, ScopeNodes, ScopePassports, ScopeRead,
	LegacyScopeReadEvents, LegacyScopeReadFeeds, LegacyScopeReadStatus,
	ScopeWildcard,
}

// IsValidAPIKeyScope reports whether scope may be issued on an API key.
func IsValidAPIKeyScope(scope string) bool {
	for _, s := range APIKeyScopes {
		if s == scope {
			return true
		}
	}
	return false
}

// ScopeRequirement is what a route demands of machine callers. A caller passes
// when it holds ANY of AnyOf; an API key also passes with any of
// LegacyAPIKeyScopes or the wildcard.
type ScopeRequirement struct {
	AnyOf              []string
	LegacyAPIKeyScopes []string
}

// Require builds a requirement satisfied by any of the given scopes.
func Require(scopes ...string) ScopeRequirement {
	return ScopeRequirement{AnyOf: scopes}
}

// WithLegacy adds legacy API-key scope names to a requirement.
func (r ScopeRequirement) WithLegacy(scopes ...string) ScopeRequirement {
	r.LegacyAPIKeyScopes = append(append([]string(nil), r.LegacyAPIKeyScopes...), scopes...)
	return r
}

// String lists the accepted scopes for error messages and logs.
func (r ScopeRequirement) String() string {
	return strings.Join(r.AnyOf, " or ")
}

// allows reports whether the caller satisfies the requirement. Users always
// pass: their access is governed by roles (RequireRole), not scopes.
func (r ScopeRequirement) allows(caller Caller) bool {
	switch caller.Kind {
	case CallerUser:
		return true
	case CallerServiceAccount:
		return containsAny(caller.Scopes, r.AnyOf)
	case CallerAPIKey:
		return containsAny(caller.Scopes, []string{ScopeWildcard}) ||
			containsAny(caller.Scopes, r.AnyOf) ||
			containsAny(caller.Scopes, r.LegacyAPIKeyScopes)
	default:
		return false
	}
}

func containsAny(held, wanted []string) bool {
	for _, w := range wanted {
		for _, h := range held {
			if h == w {
				return true
			}
		}
	}
	return false
}

// RouteScopes maps "METHOD /full/route/pattern" (gin's FullPath) to the scope
// a machine caller needs. Routes not listed accept people only (plus API keys
// holding the wildcard scope).
type RouteScopes map[string]ScopeRequirement

// RouteKey builds the RouteScopes key for a method and gin route pattern.
func RouteKey(method, fullPath string) string {
	return method + " " + fullPath
}

// EnforceRouteScopes applies the route→scope matrix to every request:
//
//	user (Janua human token)       -> unchanged; roles decide (RequireRole).
//	service account (Janua machine)-> must hold a listed scope; unlisted -> 403.
//	API key                        -> must hold a listed (or legacy) scope or
//	                                  the wildcard; unlisted -> wildcard only.
//
// It fails closed: a request with no recorded caller gets 401.
func EnforceRouteScopes(policy RouteScopes, log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		caller, ok := GetCaller(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Authentication required",
			})
			return
		}
		if caller.Kind == CallerUser {
			c.Next()
			return
		}

		req, listed := policy[RouteKey(c.Request.Method, c.FullPath())]
		if !listed {
			if caller.Kind == CallerAPIKey && containsAny(caller.Scopes, []string{ScopeWildcard}) {
				c.Next()
				return
			}
			denyScope(c, log, caller, "", "This route does not accept machine credentials")
			return
		}
		if req.allows(caller) {
			c.Next()
			return
		}
		denyScope(c, log, caller, req.String(), "Missing required scope: "+req.String())
	}
}

// RequireScope requires one of the given scopes from machine callers on the
// routes it is attached to. Users pass (roles govern them).
func RequireScope(scopes ...string) gin.HandlerFunc {
	req := Require(scopes...)
	return func(c *gin.Context) {
		caller, ok := GetCaller(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Authentication required",
			})
			return
		}
		if req.allows(caller) {
			c.Next()
			return
		}
		denyScope(c, nil, caller, req.String(), "Missing required scope: "+req.String())
	}
}

func denyScope(c *gin.Context, log *logrus.Logger, caller Caller, required, message string) {
	if log != nil {
		log.WithFields(logrus.Fields{
			"caller_kind":    caller.Kind,
			"caller_id":      caller.ID,
			"required_scope": required,
			"present_scopes": strings.Join(caller.Scopes, " "),
			"method":         c.Request.Method,
			"route":          c.FullPath(),
		}).Warn("scope.denied")
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error":   "forbidden",
		"code":    "missing_scope",
		"message": message,
	})
}
