package middleware

import "github.com/gin-gonic/gin"

// Role names carried in the Janua token's roles claim.
const (
	// RoleAdmin is the tenant administrator.
	RoleAdmin = "admin"
	// RoleInventoryManager may record receipts, run physical counts and keep
	// the material registry (RFC 0058; owner ruling 2026-11-14). The
	// identity provider does not issue it yet, so today only admins pass.
	RoleInventoryManager = "inventory_manager"
)

// InventoryWriteRoles are the roles allowed to change inventory records:
// receipts, counts and the material registry (docs/operations/inventory.md).
var InventoryWriteRoles = []string{RoleAdmin, RoleInventoryManager}

// RequireInventoryWriter is the gate of the inventory record writes: a
// person signed in through SSO (RequireHumanCaller: machine tokens and API
// keys are refused, 403 code human_only) holding one of
// InventoryWriteRoles (RequireRole: 403 error forbidden otherwise).
func RequireInventoryWriter() []gin.HandlerFunc {
	return []gin.HandlerFunc{RequireHumanCaller(), RequireRole(InventoryWriteRoles...)}
}

// IsInventoryWriter reports whether the caller passes RequireInventoryWriter,
// for writes gated only when they touch the registry (linking a stock item
// to a material on the pre-existing item routes).
func IsInventoryWriter(c *gin.Context) bool {
	caller, ok := GetCaller(c)
	if !ok || caller.Kind != CallerUser {
		return false
	}
	claims, ok := GetClaims(c)
	if !ok {
		return false
	}
	for _, have := range claims.Roles {
		for _, want := range InventoryWriteRoles {
			if have == want {
				return true
			}
		}
	}
	return false
}
