// Package auth provides authentication and authorization for the PravaraMES API.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

// OIDCConfig holds OIDC configuration.
type OIDCConfig struct {
	Issuer   string
	JWKSURL  string
	Audience string
}

// OIDCVerifier verifies JWT tokens from Janua SSO.
type OIDCVerifier struct {
	config     OIDCConfig
	jwks       *JWKS
	jwksMutex  sync.RWMutex
	httpClient *http.Client
	log        *logrus.Logger
}

// JWKS represents a JSON Web Key Set.
type JWKS struct {
	Keys      []JWK     `json:"keys"`
	FetchedAt time.Time `json:"-"`
}

// JWK represents a JSON Web Key.
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// Claims represents the JWT claims from Janua SSO.
type Claims struct {
	jwt.RegisteredClaims
	TenantID string   `json:"tenant_id"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Roles    []string `json:"roles"`
}

// NewOIDCVerifier creates a new OIDC token verifier.
func NewOIDCVerifier(config OIDCConfig, log *logrus.Logger) *OIDCVerifier {
	return &OIDCVerifier{
		config: config,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

// VerifyToken verifies a JWT token and returns the claims.
func (v *OIDCVerifier) VerifyToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Get the key ID
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing key ID in token header")
		}

		// Get the public key
		return v.getPublicKey(ctx, kid)
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Verify issuer
	if claims.Issuer != v.config.Issuer {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", v.config.Issuer, claims.Issuer)
	}

	// Verify audience
	if !v.verifyAudience(claims.Audience) {
		return nil, fmt.Errorf("invalid audience")
	}

	return claims, nil
}

// verifyAudience checks if the token audience matches the expected audience.
func (v *OIDCVerifier) verifyAudience(audiences []string) bool {
	for _, aud := range audiences {
		if aud == v.config.Audience {
			return true
		}
	}
	return false
}

// getPublicKey retrieves the public key for the given key ID.
func (v *OIDCVerifier) getPublicKey(ctx context.Context, kid string) (interface{}, error) {
	// Check if we need to refresh the JWKS
	v.jwksMutex.RLock()
	jwks := v.jwks
	v.jwksMutex.RUnlock()

	// Refresh if JWKS is nil or older than 1 hour
	if jwks == nil || time.Since(jwks.FetchedAt) > time.Hour {
		if err := v.refreshJWKS(ctx); err != nil {
			// If refresh fails but we have cached keys, log and continue
			if jwks != nil {
				v.log.WithError(err).Warn("Failed to refresh JWKS, using cached keys")
			} else {
				return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
			}
		}
	}

	v.jwksMutex.RLock()
	defer v.jwksMutex.RUnlock()

	// Find the key with matching kid
	for _, key := range v.jwks.Keys {
		if key.Kid == kid {
			return key.ToPublicKey()
		}
	}

	return nil, fmt.Errorf("key not found: %s", kid)
}

// refreshJWKS fetches the JWKS from the OIDC provider.
func (v *OIDCVerifier) refreshJWKS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.config.JWKSURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS fetch returned status %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("failed to decode JWKS: %w", err)
	}

	jwks.FetchedAt = time.Now()

	v.jwksMutex.Lock()
	v.jwks = &jwks
	v.jwksMutex.Unlock()

	v.log.WithField("keys_count", len(jwks.Keys)).Debug("JWKS refreshed")

	return nil
}

// ToPublicKey converts a JWK to an RSA public key.
func (j *JWK) ToPublicKey() (*rsa.PublicKey, error) {
	if j.Kty != "RSA" {
		return nil, fmt.Errorf("unsupported key type: %s", j.Kty)
	}

	// Decode the modulus and exponent
	nBytes, err := base64URLDecode(j.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	eBytes, err := base64URLDecode(j.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	// Convert exponent bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	// Create the RSA public key
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: e,
	}, nil
}

// base64URLDecode decodes a base64url-encoded string.
func base64URLDecode(s string) ([]byte, error) {
	// Use RawURLEncoding which handles base64url without padding
	return base64.RawURLEncoding.DecodeString(s)
}

// ExtractBearerToken extracts the token from the Authorization header.
func ExtractBearerToken(authHeader string) (string, error) {
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", fmt.Errorf("invalid authorization header format")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}

	return token, nil
}
