package middleware

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
)

func TestCallerFromClaims(t *testing.T) {
	userID := uuid.New()
	human := &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()}, TenantID: "t", Roles: []string{"admin"}}
	c := callerFromClaims(human)
	assert.Equal(t, CallerUser, c.Kind)
	assert.Equal(t, userID, c.ActorUUID())
	assert.Equal(t, userID, c.UserUUID())
	assert.Equal(t, "user:"+userID.String(), c.Actor())
	assert.False(t, c.IsMachine())

	machine := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "service-account:jnc_example"},
		TenantID:         "t",
		Scope:            auth.ScopeClaim{ScopeJobs, ScopeRead},
		TokenUse:         "client_credentials",
		ActorType:        "service_account",
		ClientID:         "jnc_example",
	}
	m := callerFromClaims(machine)
	assert.Equal(t, CallerServiceAccount, m.Kind)
	assert.Equal(t, "jnc_example", m.ID)
	assert.Equal(t, []string{ScopeJobs, ScopeRead}, m.Scopes)
	assert.True(t, m.IsMachine())
	assert.NotEqual(t, uuid.Nil, m.ActorUUID())
	assert.Equal(t, m.ActorUUID(), callerFromClaims(machine).ActorUUID(), "actor uuid is stable")
	assert.Equal(t, uuid.Nil, m.UserUUID(), "a service account is not a user")
}

func TestCallerFromClaims_PartialMachineMarkersAreMachine(t *testing.T) {
	for _, claims := range []*auth.Claims{
		{TokenUse: "client_credentials"},
		{ActorType: "service_account"},
		{RegisteredClaims: jwt.RegisteredClaims{Subject: "service-account:jnc_x"}},
	} {
		assert.Equal(t, CallerServiceAccount, callerFromClaims(claims).Kind)
	}
}

func TestAPIKeyCallerActor(t *testing.T) {
	keyID := uuid.New()
	c := Caller{Kind: CallerAPIKey, ID: keyID.String(), Scopes: []string{ScopeJobs}}
	assert.Equal(t, keyID, c.ActorUUID(), "API key actor is the key's row id, not uuid.Nil")
	assert.Equal(t, uuid.Nil, c.UserUUID())
	assert.Equal(t, "api_key:"+keyID.String(), c.Actor())
}
