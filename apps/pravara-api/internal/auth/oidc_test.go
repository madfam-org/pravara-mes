package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

// Test helper to create RSA key pair
func generateTestRSAKey(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return privateKey, &privateKey.PublicKey
}

// Test helper to create a valid JWT token
func createTestJWT(t *testing.T, privateKey *rsa.PrivateKey, kid, issuer, audience string, claims *Claims) string {
	t.Helper()

	if claims == nil {
		claims = &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    issuer,
				Audience:  []string{audience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				Subject:   "test-user-123",
			},
			TenantID: "test-tenant",
			Email:    "test@example.com",
			Name:     "Test User",
			Roles:    []string{"admin", "user"},
		}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	return tokenString
}

// Test helper to create mock JWKS server
func createMockJWKSServer(t *testing.T, publicKey *rsa.PublicKey, kid string) *httptest.Server {
	t.Helper()

	// Encode public key components to base64url
	nBytes := publicKey.N.Bytes()
	eBytes := big.NewInt(int64(publicKey.E)).Bytes()

	nEncoded := base64.RawURLEncoding.EncodeToString(nBytes)
	eEncoded := base64.RawURLEncoding.EncodeToString(eBytes)

	jwks := JWKS{
		Keys: []JWK{
			{
				Kty: "RSA",
				Kid: kid,
				Use: "sig",
				Alg: "RS256",
				N:   nEncoded,
				E:   eEncoded,
			},
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
}

func TestVerifyToken_ValidToken(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)
	kid := "test-key-1"
	issuer := "https://test-issuer.com"
	audience := "test-audience"

	server := createMockJWKSServer(t, publicKey, kid)
	defer server.Close()

	config := OIDCConfig{
		Issuer:   issuer,
		JWKSURL:  server.URL,
		Audience: audience,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(config, logger)

	tokenString := createTestJWT(t, privateKey, kid, issuer, audience, nil)

	claims, err := verifier.VerifyToken(context.Background(), tokenString)
	if err != nil {
		t.Fatalf("expected valid token to verify, got error: %v", err)
	}

	if claims.TenantID != "test-tenant" {
		t.Errorf("TenantID: got %q, want %q", claims.TenantID, "test-tenant")
	}
	if claims.Email != "test@example.com" {
		t.Errorf("Email: got %q, want %q", claims.Email, "test@example.com")
	}
	if claims.Name != "Test User" {
		t.Errorf("Name: got %q, want %q", claims.Name, "Test User")
	}
	if len(claims.Roles) != 2 {
		t.Errorf("Roles count: got %d, want 2", len(claims.Roles))
	}
}

func TestVerifyToken_InvalidSigningMethod(t *testing.T) {
	kid := "test-key-1"
	issuer := "https://test-issuer.com"
	audience := "test-audience"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(JWKS{Keys: []JWK{}})
	}))
	defer server.Close()

	config := OIDCConfig{
		Issuer:   issuer,
		JWKSURL:  server.URL,
		Audience: audience,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(config, logger)

	// Create token with HS256 (symmetric) instead of RS256 (asymmetric)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  []string{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = kid
	tokenString, _ := token.SignedString([]byte("secret"))

	_, err := verifier.VerifyToken(context.Background(), tokenString)
	if err == nil {
		t.Fatal("expected error for invalid signing method, got nil")
	}
}

// newSessionVerifier builds a verifier against a mock JWKS that publishes
// publicKey as "published-key".
func newSessionVerifier(t *testing.T, publicKey *rsa.PublicKey, sessionAudiences ...string) (*OIDCVerifier, func()) {
	t.Helper()
	server := createMockJWKSServer(t, publicKey, "published-key")
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(OIDCConfig{
		Issuer:           "https://test-issuer.com",
		JWKSURL:          server.URL,
		Audience:         "test-audience",
		SessionAudiences: sessionAudiences,
		SessionRoleApp:   "pravara-mes",
	}, logger)
	return verifier, server.Close
}

// signWithoutKID signs claims the way Janua signs a session token: RS256
// with no key ID in the header.
func signWithoutKID(t *testing.T, privateKey *rsa.PrivateKey, claims *Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return tokenString
}

func sessionClaims(audience, tokenType string) *Claims {
	return &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://test-issuer.com",
			Audience:  []string{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Subject:   "7d2f4f3e-0000-4000-8000-000000000001",
		},
		TenantID: "test-tenant",
		Type:     tokenType,
		Roles:    []string{"pravara-mes:operator"},
	}
}

func TestVerifyToken_MissingKID_VerifiesAgainstPublishedKeys(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)
	verifier, closeServer := newSessionVerifier(t, publicKey)
	defer closeServer()

	claims, err := verifier.VerifyToken(context.Background(),
		signWithoutKID(t, privateKey, sessionClaims("test-audience", "")))
	if err != nil {
		t.Fatalf("expected a token without kid, signed by the published key, to verify: %v", err)
	}
	if claims.TenantID != "test-tenant" {
		t.Errorf("unexpected tenant: %q", claims.TenantID)
	}
}

func TestVerifyToken_MissingKID_RejectsAnotherSigner(t *testing.T) {
	_, publicKey := generateTestRSAKey(t)
	otherPrivateKey, _ := generateTestRSAKey(t)
	verifier, closeServer := newSessionVerifier(t, publicKey)
	defer closeServer()

	_, err := verifier.VerifyToken(context.Background(),
		signWithoutKID(t, otherPrivateKey, sessionClaims("test-audience", "")))
	if err == nil {
		t.Fatal("expected a token signed by an unpublished key to be rejected")
	}
}

func TestVerifyToken_MissingKID_JWKSUnavailable(t *testing.T) {
	privateKey, _ := generateTestRSAKey(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(OIDCConfig{
		Issuer:   "https://test-issuer.com",
		JWKSURL:  "http://127.0.0.1:1/jwks",
		Audience: "test-audience",
	}, logger)

	_, err := verifier.VerifyToken(context.Background(),
		signWithoutKID(t, privateKey, sessionClaims("test-audience", "")))
	if err == nil {
		t.Fatal("expected an error when no key can be fetched")
	}
	if !strings.Contains(err.Error(), "failed to fetch JWKS") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestVerifyToken_MissingKID_IgnoresNonSigningKeys(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)
	jwk := JWK{
		Kty: "RSA",
		Kid: "encryption-key",
		Use: "enc",
		Alg: "RSA-OAEP",
		N:   base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(publicKey.E)).Bytes()),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(JWKS{Keys: []JWK{jwk}})
	}))
	defer server.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(OIDCConfig{
		Issuer:   "https://test-issuer.com",
		JWKSURL:  server.URL,
		Audience: "test-audience",
	}, logger)

	_, err := verifier.VerifyToken(context.Background(),
		signWithoutKID(t, privateKey, sessionClaims("test-audience", "")))
	if err == nil {
		t.Fatal("expected a key published for encryption not to verify a signature")
	}
	if !strings.Contains(err.Error(), "no RSA signing key published") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestVerifyToken_SessionAudience(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)

	machine := sessionClaims("janua.dev", "access")
	machine.TokenUse = TokenUseClientCredentials
	serviceAccount := sessionClaims("janua.dev", "access")
	serviceAccount.Subject = ServiceAccountSubjectPrefix + "some-client"

	cases := []struct {
		name             string
		sessionAudiences []string
		claims           *Claims
		wantErr          string
	}{
		{"access token on a configured session audience", []string{"janua.dev"}, sessionClaims("janua.dev", "access"), ""},
		{"session audience with surrounding spaces in config", []string{" janua.dev "}, sessionClaims("janua.dev", "access"), ""},
		{"session audience not configured", nil, sessionClaims("janua.dev", "access"), "invalid audience"},
		{"refresh token", []string{"janua.dev"}, sessionClaims("janua.dev", "refresh"), "invalid token type"},
		{"no type claim", []string{"janua.dev"}, sessionClaims("janua.dev", ""), "invalid token type"},
		{"machine token by token_use", []string{"janua.dev"}, machine, "machine tokens must be issued for test-audience"},
		{"machine token by subject", []string{"janua.dev"}, serviceAccount, "machine tokens must be issued for test-audience"},
		{"unrelated audience", []string{"janua.dev"}, sessionClaims("someone-else", "access"), "invalid audience"},
		{"blank configured entry never matches", []string{""}, sessionClaims("", "access"), "invalid audience"},
		{"primary audience needs no type", []string{"janua.dev"}, sessionClaims("test-audience", ""), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verifier, closeServer := newSessionVerifier(t, publicKey, tc.sessionAudiences...)
			defer closeServer()

			_, err := verifier.VerifyToken(context.Background(), signWithoutKID(t, privateKey, tc.claims))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the token to verify, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected an error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestVerifyToken_InvalidIssuer(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)
	kid := "test-key-1"
	issuer := "https://test-issuer.com"
	audience := "test-audience"
	wrongIssuer := "https://wrong-issuer.com"

	server := createMockJWKSServer(t, publicKey, kid)
	defer server.Close()

	config := OIDCConfig{
		Issuer:   issuer,
		JWKSURL:  server.URL,
		Audience: audience,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(config, logger)

	// Create token with wrong issuer
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    wrongIssuer,
			Audience:  []string{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	tokenString := createTestJWT(t, privateKey, kid, wrongIssuer, audience, claims)

	_, err := verifier.VerifyToken(context.Background(), tokenString)
	if err == nil {
		t.Fatal("expected error for invalid issuer, got nil")
	}
}

func TestVerifyToken_InvalidAudience(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)
	kid := "test-key-1"
	issuer := "https://test-issuer.com"
	audience := "test-audience"
	wrongAudience := "wrong-audience"

	server := createMockJWKSServer(t, publicKey, kid)
	defer server.Close()

	config := OIDCConfig{
		Issuer:   issuer,
		JWKSURL:  server.URL,
		Audience: audience,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(config, logger)

	// Create token with wrong audience
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  []string{wrongAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	tokenString := createTestJWT(t, privateKey, kid, issuer, wrongAudience, claims)

	_, err := verifier.VerifyToken(context.Background(), tokenString)
	if err == nil {
		t.Fatal("expected error for invalid audience, got nil")
	}
}

func TestJWKSRefresh_Success(t *testing.T) {
	_, publicKey := generateTestRSAKey(t)
	kid := "test-key-1"

	server := createMockJWKSServer(t, publicKey, kid)
	defer server.Close()

	config := OIDCConfig{
		Issuer:   "https://test-issuer.com",
		JWKSURL:  server.URL,
		Audience: "test-audience",
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(config, logger)

	err := verifier.refreshJWKS(context.Background())
	if err != nil {
		t.Fatalf("expected successful JWKS refresh, got error: %v", err)
	}

	if verifier.jwks == nil {
		t.Fatal("JWKS should not be nil after refresh")
	}

	if len(verifier.jwks.Keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(verifier.jwks.Keys))
	}

	if verifier.jwks.Keys[0].Kid != kid {
		t.Errorf("key ID: got %q, want %q", verifier.jwks.Keys[0].Kid, kid)
	}
}

func TestJWKSRefresh_UseCachedOnFailure(t *testing.T) {
	_, publicKey := generateTestRSAKey(t)
	kid := "test-key-1"

	// Create server that will be closed to simulate failure
	server := createMockJWKSServer(t, publicKey, kid)

	config := OIDCConfig{
		Issuer:   "https://test-issuer.com",
		JWKSURL:  server.URL,
		Audience: "test-audience",
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(config, logger)

	// First refresh should succeed
	err := verifier.refreshJWKS(context.Background())
	if err != nil {
		t.Fatalf("initial JWKS refresh failed: %v", err)
	}

	// Close server to simulate network failure
	server.Close()

	// Force refresh by setting old timestamp
	verifier.jwksMutex.Lock()
	verifier.jwks.FetchedAt = time.Now().Add(-2 * time.Hour)
	verifier.jwksMutex.Unlock()

	// Attempt to get public key should use cached JWKS despite refresh failure
	_, err = verifier.getPublicKey(context.Background(), kid)
	if err != nil {
		t.Errorf("expected to use cached keys on refresh failure, got error: %v", err)
	}
}

func TestExtractBearerToken_Valid(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantToken  string
		wantError  bool
	}{
		{
			name:       "valid bearer token",
			authHeader: "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantToken:  "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantError:  false,
		},
		{
			name:       "valid bearer token lowercase",
			authHeader: "bearer test-token-123",
			wantToken:  "test-token-123",
			wantError:  false,
		},
		{
			name:       "valid bearer with mixed case",
			authHeader: "BeArEr token-value",
			wantToken:  "token-value",
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := ExtractBearerToken(tt.authHeader)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if token != tt.wantToken {
				t.Errorf("token: got %q, want %q", token, tt.wantToken)
			}
		})
	}
}

func TestExtractBearerToken_Invalid(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
	}{
		{
			name:       "empty header",
			authHeader: "",
		},
		{
			name:       "missing bearer prefix",
			authHeader: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
		{
			name:       "wrong auth type",
			authHeader: "Basic dXNlcjpwYXNz",
		},
		{
			name:       "bearer without token",
			authHeader: "Bearer",
		},
		{
			name:       "bearer with empty token",
			authHeader: "Bearer ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := ExtractBearerToken(tt.authHeader)
			if err == nil {
				t.Fatalf("expected error, got token: %q", token)
			}
		})
	}
}

func TestJWK_ToPublicKey(t *testing.T) {
	_, publicKey := generateTestRSAKey(t)

	// Encode public key components to base64url
	nBytes := publicKey.N.Bytes()
	eBytes := big.NewInt(int64(publicKey.E)).Bytes()

	nEncoded := base64.RawURLEncoding.EncodeToString(nBytes)
	eEncoded := base64.RawURLEncoding.EncodeToString(eBytes)

	jwk := JWK{
		Kty: "RSA",
		Kid: "test-key",
		Use: "sig",
		Alg: "RS256",
		N:   nEncoded,
		E:   eEncoded,
	}

	pubKey, err := jwk.ToPublicKey()
	if err != nil {
		t.Fatalf("ToPublicKey failed: %v", err)
	}

	if pubKey == nil {
		t.Fatal("public key should not be nil")
	}
}

func TestJWK_ToPublicKey_UnsupportedKeyType(t *testing.T) {
	jwk := JWK{
		Kty: "EC", // Elliptic curve, not RSA
		Kid: "test-key",
	}

	_, err := jwk.ToPublicKey()
	if err == nil {
		t.Fatal("expected error for unsupported key type, got nil")
	}
	if err.Error() != "unsupported key type: EC" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestVerifyToken_ExpiredToken(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)
	kid := "test-key-1"
	issuer := "https://test-issuer.com"
	audience := "test-audience"

	server := createMockJWKSServer(t, publicKey, kid)
	defer server.Close()

	config := OIDCConfig{
		Issuer:   issuer,
		JWKSURL:  server.URL,
		Audience: audience,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(config, logger)

	// Create expired token
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  []string{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // Expired 1 hour ago
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}

	tokenString := createTestJWT(t, privateKey, kid, issuer, audience, claims)

	_, err := verifier.VerifyToken(context.Background(), tokenString)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestVerifyToken_MultipleAudiences(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)
	kid := "test-key-1"
	issuer := "https://test-issuer.com"
	audience := "test-audience"

	server := createMockJWKSServer(t, publicKey, kid)
	defer server.Close()

	config := OIDCConfig{
		Issuer:   issuer,
		JWKSURL:  server.URL,
		Audience: audience,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(config, logger)

	// Create token with multiple audiences including the valid one
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  []string{"other-audience", audience, "another-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	tokenString, _ := token.SignedString(privateKey)

	_, err := verifier.VerifyToken(context.Background(), tokenString)
	if err != nil {
		t.Fatalf("expected valid token with multiple audiences to verify, got error: %v", err)
	}
}

func TestVerifyToken_SessionAudience_ApplicationRoles(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)

	withRoles := func(roles ...string) *Claims {
		c := sessionClaims("janua.dev", "access")
		c.Roles = roles
		return c
	}

	cases := []struct {
		name      string
		claims    *Claims
		wantRoles []string
		wantNone  bool
	}{
		{"keeps this application's roles without the prefix",
			withRoles("pravara-mes:admin", "pravara-mes:inventory_manager"), []string{"admin", "inventory_manager"}, false},
		{"drops other products' roles and bare names",
			withRoles("admin", "tulana:admin", "hcm:hr", "pravara-mes:operator"), []string{"operator"}, false},
		{"a bare admin grants nothing", withRoles("admin"), nil, true},
		{"another product's admin grants nothing", withRoles("tulana:admin", "nauta:platform-admin"), nil, true},
		{"a look-alike prefix grants nothing", withRoles("pravara-mes-x:admin", "pravara:admin"), nil, true},
		{"an empty or nested role name grants nothing", withRoles("pravara-mes:", "pravara-mes:a:b"), nil, true},
		{"no roles at all", withRoles(), nil, true},
		{"repeats collapse", withRoles("pravara-mes:admin", "pravara-mes:admin"), []string{"admin"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verifier, closeServer := newSessionVerifier(t, publicKey, "janua.dev")
			defer closeServer()

			claims, err := verifier.VerifyToken(context.Background(), signWithoutKID(t, privateKey, tc.claims))
			if tc.wantNone {
				if !errors.Is(err, ErrNoApplicationRole) {
					t.Fatalf("expected ErrNoApplicationRole, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected the token to verify, got: %v", err)
			}
			if strings.Join(claims.Roles, ",") != strings.Join(tc.wantRoles, ",") {
				t.Errorf("roles = %v, want %v", claims.Roles, tc.wantRoles)
			}
		})
	}
}

func TestVerifyToken_SessionAudience_NeedsAnApplicationSlug(t *testing.T) {
	privateKey, publicKey := generateTestRSAKey(t)
	server := createMockJWKSServer(t, publicKey, "published-key")
	defer server.Close()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	verifier := NewOIDCVerifier(OIDCConfig{
		Issuer:           "https://test-issuer.com",
		JWKSURL:          server.URL,
		Audience:         "test-audience",
		SessionAudiences: []string{"janua.dev"},
	}, logger)

	_, err := verifier.VerifyToken(context.Background(),
		signWithoutKID(t, privateKey, sessionClaims("janua.dev", "access")))
	if !errors.Is(err, ErrNoApplicationRole) {
		t.Fatalf("expected no session token to be accepted without an application slug, got: %v", err)
	}
}

func TestVerifyToken_PrimaryAudienceKeepsItsRoles(t *testing.T) {
	// Tokens minted for this API's own audience are untouched by the
	// application-role filter.
	privateKey, publicKey := generateTestRSAKey(t)
	verifier, closeServer := newSessionVerifier(t, publicKey, "janua.dev")
	defer closeServer()

	c := sessionClaims("test-audience", "")
	c.Roles = []string{"admin", "user"}
	claims, err := verifier.VerifyToken(context.Background(), signWithoutKID(t, privateKey, c))
	if err != nil {
		t.Fatalf("expected the token to verify, got: %v", err)
	}
	if strings.Join(claims.Roles, ",") != "admin,user" {
		t.Errorf("roles = %v, want [admin user]", claims.Roles)
	}
}
