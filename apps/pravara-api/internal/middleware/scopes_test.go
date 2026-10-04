package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func scopeRouter(caller *Caller, handlers ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if caller != nil {
			c.Set(string(ContextKeyCaller), *caller)
		}
		c.Next()
	})
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"message": "access granted"}) }
	router.GET("/scoped", append(handlers, ok)...)
	router.GET("/unlisted", append(handlers, ok)...)
	router.POST("/scoped", append(handlers, ok)...)
	return router
}

func doGet(router *gin.Engine, path string) int {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(w, req)
	return w.Code
}

func TestRequireScope(t *testing.T) {
	cases := []struct {
		name   string
		caller *Caller
		want   int
	}{
		{"user passes (roles govern)", &Caller{Kind: CallerUser, ID: "u"}, http.StatusOK},
		{"service account with scope", &Caller{Kind: CallerServiceAccount, ID: "c", Scopes: []string{ScopeRead}}, http.StatusOK},
		{"service account without scope", &Caller{Kind: CallerServiceAccount, ID: "c", Scopes: []string{ScopeJobs}}, http.StatusForbidden},
		{"service account wildcard is not honoured", &Caller{Kind: CallerServiceAccount, ID: "c", Scopes: []string{ScopeWildcard}}, http.StatusForbidden},
		{"api key with scope", &Caller{Kind: CallerAPIKey, ID: "k", Scopes: []string{ScopeRead}}, http.StatusOK},
		{"api key wildcard", &Caller{Kind: CallerAPIKey, ID: "k", Scopes: []string{ScopeWildcard}}, http.StatusOK},
		{"api key without scope", &Caller{Kind: CallerAPIKey, ID: "k", Scopes: []string{ScopeJobs}}, http.StatusForbidden},
		{"no caller", nil, http.StatusUnauthorized},
		{"unknown caller kind", &Caller{Kind: "other", Scopes: []string{ScopeRead}}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, doGet(scopeRouter(tc.caller, RequireScope(ScopeRead)), "/scoped"))
		})
	}
}

func TestEnforceRouteScopes(t *testing.T) {
	policy := RouteScopes{
		RouteKey(http.MethodGet, "/scoped"): Require(ScopeRead, ScopeNodes).WithLegacy(LegacyScopeReadEvents),
	}
	cases := []struct {
		name   string
		caller *Caller
		path   string
		want   int
	}{
		{"user on listed route", &Caller{Kind: CallerUser, ID: "u"}, "/scoped", http.StatusOK},
		{"user on unlisted route", &Caller{Kind: CallerUser, ID: "u"}, "/unlisted", http.StatusOK},
		{"machine with any-of scope", &Caller{Kind: CallerServiceAccount, ID: "c", Scopes: []string{ScopeNodes}}, "/scoped", http.StatusOK},
		{"machine missing scope", &Caller{Kind: CallerServiceAccount, ID: "c", Scopes: []string{ScopeJobs}}, "/scoped", http.StatusForbidden},
		{"machine with legacy name is not honoured", &Caller{Kind: CallerServiceAccount, ID: "c", Scopes: []string{LegacyScopeReadEvents}}, "/scoped", http.StatusForbidden},
		{"machine on unlisted route", &Caller{Kind: CallerServiceAccount, ID: "c", Scopes: MachineScopes}, "/unlisted", http.StatusForbidden},
		{"api key legacy scope", &Caller{Kind: CallerAPIKey, ID: "k", Scopes: []string{LegacyScopeReadEvents}}, "/scoped", http.StatusOK},
		{"api key missing scope", &Caller{Kind: CallerAPIKey, ID: "k", Scopes: []string{LegacyScopeReadFeeds}}, "/scoped", http.StatusForbidden},
		{"api key narrow scope on unlisted route", &Caller{Kind: CallerAPIKey, ID: "k", Scopes: []string{ScopeRead}}, "/unlisted", http.StatusForbidden},
		{"api key wildcard on unlisted route", &Caller{Kind: CallerAPIKey, ID: "k", Scopes: []string{ScopeWildcard}}, "/unlisted", http.StatusOK},
		{"no caller fails closed", nil, "/scoped", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := scopeRouter(tc.caller, EnforceRouteScopes(policy, newTestLogger()))
			assert.Equal(t, tc.want, doGet(router, tc.path))
		})
	}
}

func TestEnforceRouteScopes_MethodIsPartOfTheKey(t *testing.T) {
	policy := RouteScopes{RouteKey(http.MethodGet, "/scoped"): Require(ScopeRead)}
	router := scopeRouter(&Caller{Kind: CallerServiceAccount, ID: "c", Scopes: []string{ScopeRead}}, EnforceRouteScopes(policy, nil))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/scoped", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "missing_scope")
}

func TestIsValidAPIKeyScope(t *testing.T) {
	for _, s := range APIKeyScopes {
		assert.True(t, IsValidAPIKeyScope(s), s)
	}
	for _, s := range []string{"", "pravara-mes:admin", "write:orders", "pravara-mes:*"} {
		assert.False(t, IsValidAPIKeyScope(s), s)
	}
}
