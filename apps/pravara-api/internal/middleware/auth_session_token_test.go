package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
)

// sessionTokenFixture is a real verifier against a mock JWKS, plus a signer
// for Janua-shaped session tokens (RS256, no key ID, audience janua.dev).
func sessionTokenFixture(t *testing.T) (*auth.OIDCVerifier, func(roles ...string) string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	jwks := auth.JWKS{Keys: []auth.JWK{{
		Kty: "RSA",
		Kid: "published-key",
		Use: "sig",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
	}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(server.Close)

	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	verifier := auth.NewOIDCVerifier(auth.OIDCConfig{
		Issuer:           "https://test-issuer.com",
		JWKSURL:          server.URL,
		Audience:         "pravara-api",
		SessionAudiences: []string{"janua.dev"},
		SessionRoleApp:   "pravara-mes",
	}, logger)

	sign := func(roles ...string) string {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "https://test-issuer.com",
				Audience:  []string{"janua.dev"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				Subject:   "7d2f4f3e-0000-4000-8000-000000000001",
			},
			TenantID: "11111111-1111-4111-8111-111111111111",
			Type:     "access",
			Roles:    roles,
		})
		signed, err := token.SignedString(privateKey)
		if err != nil {
			t.Fatalf("failed to sign token: %v", err)
		}
		return signed
	}
	return verifier, sign
}

// A session token of the ecosystem whose holder has no role in this
// application is refused with 403 and a code the UI can explain, before any
// database work. (The database is nil here: reaching it would panic.)
func TestAuthMiddleware_SessionTokenWithoutApplicationRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	verifier, sign := sessionTokenFixture(t)
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	router := gin.New()
	router.Use(AuthMiddleware(verifier, nil, logger))
	router.GET("/test", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	for name, roles := range map[string][]string{
		"no roles":                 nil,
		"another product's admin":  {"tulana:admin", "nauta:platform-admin"},
		"a bare organization role": {"admin"},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+sign(roles...))
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
			var body map[string]string
			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "forbidden", body["error"])
			assert.Equal(t, "no_application_role", body["code"])
		})
	}
}

func TestAuthMiddleware_TamperedSessionTokenIsUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	verifier, sign := sessionTokenFixture(t)
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	router := gin.New()
	router.Use(AuthMiddleware(verifier, nil, logger))
	router.GET("/test", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	token := sign("pravara-mes:admin")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token[:len(token)-4]+"AAAA")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
