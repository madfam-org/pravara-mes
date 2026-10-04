package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
)

// CallerKind classifies the credential that authenticated a request.
type CallerKind string

const (
	// CallerUser is a person signed in through Janua (authorization_code).
	CallerUser CallerKind = "user"
	// CallerServiceAccount is a Janua client_credentials (machine) token.
	CallerServiceAccount CallerKind = "service_account"
	// CallerAPIKey is a tenant-issued prv_ API key.
	CallerAPIKey CallerKind = "api_key"
)

// ContextKeyCaller is the context key for the authenticated Caller.
const ContextKeyCaller ContextKey = "caller"

// serviceAccountActorNamespace derives stable actor UUIDs for Janua service
// clients, whose identifiers are not UUIDs. Fixed forever: changing it would
// change every recorded service-account actor id.
var serviceAccountActorNamespace = uuid.MustParse("8f6c1d2e-5b7a-4c3e-9d1f-2a6b8c0e4f13")

// Caller is who made the request and what it may do.
type Caller struct {
	Kind CallerKind
	// ID is the user's subject, the API key's row id, or the Janua client id.
	ID string
	// Scopes are the granted scopes: the token's `scope` claim for a service
	// account, the key's scopes for an API key. Users have none; their access
	// is governed by roles.
	Scopes []string
}

// IsMachine reports whether the caller is a non-human credential.
func (c Caller) IsMachine() bool {
	return c.Kind == CallerServiceAccount || c.Kind == CallerAPIKey
}

// Actor returns the audit identity, e.g. "user:<uuid>", "api_key:<uuid>" or
// "service_account:<client_id>".
func (c Caller) Actor() string {
	return string(c.Kind) + ":" + c.ID
}

// ActorUUID returns a stable UUID for the caller: the user's id, the API key's
// row id, or a name-based UUID (v5) derived from the service client id. It is
// uuid.Nil only when the caller is unknown or a user subject is not a UUID.
func (c Caller) ActorUUID() uuid.UUID {
	switch c.Kind {
	case CallerUser, CallerAPIKey:
		if id, err := uuid.Parse(c.ID); err == nil {
			return id
		}
	case CallerServiceAccount:
		if c.ID != "" {
			return uuid.NewSHA1(serviceAccountActorNamespace, []byte(c.Actor()))
		}
	}
	return uuid.Nil
}

// UserUUID returns the user's id for columns that reference users(id), and
// uuid.Nil (stored as NULL) for machine callers, which are not users.
func (c Caller) UserUUID() uuid.UUID {
	if c.Kind != CallerUser {
		return uuid.Nil
	}
	if id, err := uuid.Parse(c.ID); err == nil {
		return id
	}
	return uuid.Nil
}

// callerFromClaims classifies verified Janua claims.
func callerFromClaims(claims *auth.Claims) Caller {
	if claims.IsMachineToken() {
		return Caller{
			Kind:   CallerServiceAccount,
			ID:     claims.ServiceClientID(),
			Scopes: append([]string(nil), claims.Scope...),
		}
	}
	return Caller{Kind: CallerUser, ID: claims.Subject}
}

// setJWTCaller records the caller for a verified Janua token.
func setJWTCaller(c *gin.Context, claims *auth.Claims) {
	c.Set(string(ContextKeyCaller), callerFromClaims(claims))
	c.Set(string(ContextKeyAuthMethod), "jwt")
}

// setAPIKeyCaller records the caller for a validated API key.
func setAPIKeyCaller(c *gin.Context, keyID uuid.UUID, scopes []string) {
	c.Set(string(ContextKeyCaller), Caller{
		Kind:   CallerAPIKey,
		ID:     keyID.String(),
		Scopes: append([]string(nil), scopes...),
	})
}

// GetCaller retrieves the authenticated caller from the Gin context.
func GetCaller(c *gin.Context) (Caller, bool) {
	v, ok := c.Get(string(ContextKeyCaller))
	if !ok {
		return Caller{}, false
	}
	caller, ok := v.(Caller)
	return caller, ok
}

// ActorUUID returns the request's actor UUID (see Caller.ActorUUID) for event
// payloads and other records that do not reference users(id).
func ActorUUID(c *gin.Context) uuid.UUID {
	caller, ok := GetCaller(c)
	if !ok {
		return uuid.Nil
	}
	return caller.ActorUUID()
}

// ActorUserUUID returns the user id for columns that reference users(id), or
// uuid.Nil (stored as NULL) for machine callers.
func ActorUserUUID(c *gin.Context) uuid.UUID {
	caller, ok := GetCaller(c)
	if !ok {
		return uuid.Nil
	}
	return caller.UserUUID()
}
