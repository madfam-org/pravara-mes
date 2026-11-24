package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

const realtimeTestSecret = "realtime-test-secret"

func newRealtimeTestHandler() *RealtimeHandler {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return NewRealtimeHandler(&config.CentrifugoConfig{
		TokenSecret: realtimeTestSecret,
		TokenTTL:    3600,
		PublicURL:   "wss://realtime.example.test/connection/websocket",
	}, log)
}

// getRealtimeToken calls GetToken with the context the auth middleware
// leaves: tenant and user as strings. An empty value is left unset.
func getRealtimeToken(t *testing.T, h *RealtimeHandler, tenantID, userID string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/v1/realtime/token", func(c *gin.Context) {
		if tenantID != "" {
			c.Set(string(middleware.ContextKeyTenantID), tenantID)
		}
		if userID != "" {
			c.Set(string(middleware.ContextKeyUserID), userID)
		}
		c.Next()
	}, h.GetToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/realtime/token", nil))
	return w
}

func postRealtimeJSON(t *testing.T, handler gin.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST(path, handler)
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestRealtimeGetToken_IssuesTokenWithTenantInSubject(t *testing.T) {
	h := newRealtimeTestHandler()
	tenantID, userID := uuid.NewString(), uuid.NewString()

	w := getRealtimeToken(t, h, tenantID, userID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp TokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Token)
	assert.Equal(t, "wss://realtime.example.test/connection/websocket", resp.URL)

	claims := &CentrifugoClaims{}
	_, err := jwt.ParseWithClaims(resp.Token, claims, func(*jwt.Token) (interface{}, error) {
		return []byte(realtimeTestSecret), nil
	})
	require.NoError(t, err)
	// "sub" on the wire (CentrifugoClaims.Sub shadows RegisteredClaims.Subject).
	assert.Equal(t, tenantID+":"+userID, claims.Sub)
	assert.Equal(t, tenantID, claims.Info["tenant_id"])
}

func TestRealtimeGetToken_APIKeyCaller(t *testing.T) {
	// API-key callers carry the user "apikey:<key id>"; the tenant is still
	// the segment before the first ":".
	h := newRealtimeTestHandler()
	tenantID := uuid.NewString()
	w := getRealtimeToken(t, h, tenantID, "apikey:"+uuid.NewString())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestRealtimeGetToken_MissingContext(t *testing.T) {
	h := newRealtimeTestHandler()
	for name, tc := range map[string]struct{ tenant, user string }{
		"no user":   {tenant: uuid.NewString()},
		"no tenant": {user: uuid.NewString()},
		"neither":   {},
	} {
		t.Run(name, func(t *testing.T) {
			w := getRealtimeToken(t, h, tc.tenant, tc.user)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			var resp ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, "unauthorized", resp.Error)
		})
	}
}

// TestRealtimeToken_RoundTrip: the connect proxy returns the token's subject
// as the connection's user, and the subscribe proxy accepts that user on its
// own tenant's channels only.
func TestRealtimeToken_RoundTrip(t *testing.T) {
	h := newRealtimeTestHandler()
	tenantID, userID := uuid.NewString(), uuid.NewString()

	w := getRealtimeToken(t, h, tenantID, userID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var tok TokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tok))

	w = postRealtimeJSON(t, h.AuthConnect, "/v1/realtime/auth", ConnectAuthRequest{Client: "c1", Token: tok.Token})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var conn ConnectAuthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &conn))
	assert.Equal(t, tenantID+":"+userID, conn.Result.User)
	assert.Equal(t, tenantID, conn.Result.Info["tenant_id"])

	w = postRealtimeJSON(t, h.AuthSubscribe, "/v1/realtime/subscribe", SubscribeRequest{
		User: conn.Result.User, Channel: "machines:" + tenantID})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = postRealtimeJSON(t, h.AuthSubscribe, "/v1/realtime/subscribe", SubscribeRequest{
		User: conn.Result.User, Channel: "machines:" + uuid.NewString()})
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestRealtimeAuthSubscribe(t *testing.T) {
	h := newRealtimeTestHandler()
	tenantID, otherTenant, userID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	user := tenantID + ":" + userID

	cases := []struct {
		name    string
		user    string
		channel string
		want    int
	}{
		{"own tenant", user, "machines:" + tenantID, http.StatusOK},
		{"own tenant entity channel", user, "telemetry:" + tenantID + ":" + uuid.NewString(), http.StatusOK},
		{"own tenant, API key user", tenantID + ":apikey:" + uuid.NewString(), "tasks:" + tenantID, http.StatusOK},
		{"other tenant", user, "machines:" + otherTenant, http.StatusForbidden},
		{"user without tenant (old format)", userID, "machines:" + tenantID, http.StatusForbidden},
		{"user with a non-UUID tenant", "acme:" + userID, "machines:" + tenantID, http.StatusForbidden},
		{"empty user", "", "machines:" + tenantID, http.StatusForbidden},
		{"channel without tenant", user, "machines", http.StatusForbidden},
		{"channel with empty tenant", user, "machines:", http.StatusForbidden},
		{"channel with non-UUID tenant", user, "machines:acme", http.StatusForbidden},
		{"channel without namespace", user, ":" + tenantID, http.StatusForbidden},
		{"channel with extra segments", user, "machines:" + tenantID + ":" + uuid.NewString() + ":x", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postRealtimeJSON(t, h.AuthSubscribe, "/v1/realtime/subscribe", SubscribeRequest{
				Client: "c1", User: tc.user, Channel: tc.channel})
			assert.Equal(t, tc.want, w.Code, w.Body.String())
			if tc.want == http.StatusForbidden {
				var body struct {
					Error struct {
						Code int `json:"code"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, 103, body.Error.Code)
			}
		})
	}
}
