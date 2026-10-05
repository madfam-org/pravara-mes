package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRequireHumanCaller(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name   string
		caller *Caller
		want   int
	}{
		{"person", &Caller{Kind: CallerUser, ID: "u1"}, http.StatusOK},
		{"wildcard API key", &Caller{Kind: CallerAPIKey, ID: "k1", Scopes: []string{ScopeWildcard}}, http.StatusForbidden},
		{"service account with every scope", &Caller{Kind: CallerServiceAccount, ID: "c1", Scopes: MachineScopes}, http.StatusForbidden},
		{"no caller", nil, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/x", func(ctx *gin.Context) {
				if c.caller != nil {
					ctx.Set(string(ContextKeyCaller), *c.caller)
				}
				ctx.Next()
			}, RequireHumanCaller(), func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
			assert.Equal(t, c.want, w.Code)
			if c.want == http.StatusForbidden {
				assert.Contains(t, w.Body.String(), "human_only")
			}
		})
	}
}
