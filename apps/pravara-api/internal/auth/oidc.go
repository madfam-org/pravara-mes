// Package auth provides authentication and authorization for the PravaraMES API.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	// SessionAudiences are further audiences accepted for a person's
	// FIRST-PARTY session token only. Janua signs the access token of a
	// sign-in in the app (email and password) with its own audience,
	// `janua.dev`, not with this API's. Other products and any OAuth client
	// without an audience of its own share it, so a token that matches only
	// one of these must carry no `client_id` and no `scope` (see
	// VerifyToken). Machine tokens and tokens issued to an OAuth client are
	// accepted on Audience alone. Empty by default, so nothing but Audience
	// is accepted unless it is configured.
	SessionAudiences []string
	// SessionRoleApp is this application's slug in Janua's application
	// roles ("pravara-mes"). Janua's `roles` claim mixes application roles
	// of every product with organization-account roles ("admin", "owner"),
	// so a person's token, on any audience, is accepted only when it holds
	// at least one "<app>:<role>" role, and those, with the prefix removed,
	// are its roles here. Everything else in the claim is dropped. With it
	// empty no person's token is accepted.
	SessionRoleApp string
}

// ErrNoApplicationRole means the token is a valid session token of the
// ecosystem whose holder has been granted no role in this application.
var ErrNoApplicationRole = errors.New("token holds no role for this application")

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

	// Type is Janua's token kind: "access" or "refresh" on a person's token.
	Type string `json:"type,omitempty"`

	// IsServiceAccount marks a Janua service principal: a technical login
	// with a person-shaped token (UUID subject, no machine marker).
	IsServiceAccount bool `json:"is_service_account,omitempty"`

	// Machine-token claims (Janua client_credentials grant). Human tokens
	// carry none of them; see machine_claims.go.
	Scope     ScopeClaim `json:"scope,omitempty"`
	TokenUse  string     `json:"token_use,omitempty"`
	ActorType string     `json:"actor_type,omitempty"`
	ClientID  string     `json:"client_id,omitempty"`
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
//
// Every token: RS256, signed by a key in the issuer's JWKS, carrying an
// expiry that has not passed, the configured issuer and an accepted audience.
//
// A MACHINE token (client_credentials) must name Audience and is returned as
// it is; its authority is its scope.
//
// Any other token is a PERSON's and is held to the same rules whichever
// accepted audience it names and whether or not it names a key ID:
//
//   - `type` is "access" (a refresh token shares signature and audience);
//   - it is not a Janua service principal (`is_service_account`);
//   - it holds at least one "<SessionRoleApp>:<role>" application role, and
//     those, with the prefix removed, are its only roles here.
//
// On a session audience, which other products and OAuth clients share, it
// must also be a first-party session token: no `client_id` and no `scope`.
// Every token Janua issues to an OAuth client carries both, so a token
// obtained by some other application cannot be replayed here.
func (v *OIDCVerifier) VerifyToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// A key ID selects one published key. Janua's session tokens carry
		// none; those are checked against every published RSA signing key.
		// Either way the signature must verify against a key from the
		// issuer's JWKS.
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return v.signingKeySet(ctx)
		}

		// Get the public key
		return v.getPublicKey(ctx, kid)
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithExpirationRequired())

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
	primary := v.verifyAudience(claims.Audience)
	if !primary && !v.verifySessionAudience(claims.Audience) {
		return nil, fmt.Errorf("invalid audience")
	}

	if claims.IsMachineToken() {
		if !primary {
			return nil, fmt.Errorf("invalid audience: machine tokens must be issued for %s", v.config.Audience)
		}
		return claims, nil
	}

	// A person's token from here on.
	if claims.Type != TokenTypeAccess {
		return nil, fmt.Errorf("invalid token type: %q", claims.Type)
	}
	if claims.IsServiceAccount {
		return nil, fmt.Errorf("service principals are not people: use a client_credentials token")
	}
	if !primary && (claims.ClientID != "" || len(claims.Scope) > 0) {
		// Issued to an OAuth client that shares the session audience.
		return nil, fmt.Errorf("invalid audience: tokens issued to an OAuth client must name %s", v.config.Audience)
	}

	// The roles this application granted, and nothing else.
	claims.Roles = applicationRoles(claims.Roles, v.config.SessionRoleApp)
	if len(claims.Roles) == 0 {
		return nil, ErrNoApplicationRole
	}

	return claims, nil
}

// applicationRoles returns the roles named "<app>:<role>" with the prefix
// removed, in order and without repeats. An empty app matches nothing.
func applicationRoles(roles []string, app string) []string {
	app = strings.TrimSpace(app)
	if app == "" {
		return nil
	}
	prefix := app + ":"
	var out []string
	seen := map[string]bool{}
	for _, role := range roles {
		name, ok := strings.CutPrefix(role, prefix)
		if !ok || name == "" || strings.Contains(name, ":") || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// TokenTypeAccess is the `type` claim of a Janua access token issued to a
// person, by a sign-in or by an OAuth client.
const TokenTypeAccess = "access"

// verifyAudience checks if the token audience matches the expected audience.
func (v *OIDCVerifier) verifyAudience(audiences []string) bool {
	if v.config.Audience == "" {
		return false
	}
	for _, aud := range audiences {
		if aud == v.config.Audience {
			return true
		}
	}
	return false
}

// verifySessionAudience checks the token audience against the configured
// session audiences. Blank entries never match.
func (v *OIDCVerifier) verifySessionAudience(audiences []string) bool {
	for _, aud := range audiences {
		if aud == "" {
			continue
		}
		for _, accepted := range v.config.SessionAudiences {
			if aud == strings.TrimSpace(accepted) {
				return true
			}
		}
	}
	return false
}

// currentJWKS returns the cached key set, refreshing it when it is missing or
// older than an hour. A failed refresh falls back to the cached keys.
func (v *OIDCVerifier) currentJWKS(ctx context.Context) (*JWKS, error) {
	v.jwksMutex.RLock()
	jwks := v.jwks
	v.jwksMutex.RUnlock()

	if jwks == nil || time.Since(jwks.FetchedAt) > time.Hour {
		if err := v.refreshJWKS(ctx); err != nil {
			if jwks == nil {
				return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
			}
			v.log.WithError(err).Warn("Failed to refresh JWKS, using cached keys")
		}
	}

	v.jwksMutex.RLock()
	defer v.jwksMutex.RUnlock()
	return v.jwks, nil
}

// signingKeySet returns every published RSA signing key, for a token whose
// header names no key ID. Keys published for another use are left out.
func (v *OIDCVerifier) signingKeySet(ctx context.Context) (interface{}, error) {
	jwks, err := v.currentJWKS(ctx)
	if err != nil {
		return nil, err
	}

	set := jwt.VerificationKeySet{}
	for i := range jwks.Keys {
		key := jwks.Keys[i]
		if !isSigningKey(key) {
			continue
		}
		publicKey, err := key.ToPublicKey()
		if err != nil {
			continue
		}
		set.Keys = append(set.Keys, publicKey)
	}
	if len(set.Keys) == 0 {
		return nil, fmt.Errorf("no RSA signing key published")
	}
	return set, nil
}

// isSigningKey reports whether a published key may verify a signature: an
// RSA key not published for another use or another algorithm.
func isSigningKey(key JWK) bool {
	if key.Kty != "RSA" {
		return false
	}
	if key.Use != "" && key.Use != "sig" {
		return false
	}
	return key.Alg == "" || key.Alg == jwt.SigningMethodRS256.Alg()
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
		if key.Kid == kid && isSigningKey(key) {
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
