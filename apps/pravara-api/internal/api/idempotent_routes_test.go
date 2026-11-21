package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The writes that honour Idempotency-Key are registered, and each is an
// operator's act no machine scope opens (RFC 0064 §5; ruling PV-1).
func TestIdempotentWriteRoutesAreRegisteredPersonWrites(t *testing.T) {
	routes := registeredV1Routes(t)
	policy := MachineRouteScopes()
	for _, key := range IdempotentWriteRoutes() {
		assert.True(t, routes[key], "%s is not a registered route", key)
		_, listed := policy[key]
		assert.False(t, listed, "%s must not accept machine credentials", key)
	}
}
