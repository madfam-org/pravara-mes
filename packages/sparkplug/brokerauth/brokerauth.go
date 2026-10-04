// Package brokerauth decides MQTT authentication and authorization for
// Sparkplug edge-node credentials (MES-1 §3), and serves the decisions over
// the EMQX HTTP authentication and authorization protocol.
//
// The credential registry is the source of truth: a username maps to exactly
// one (tenant group, edge node) pair and a password hash. The tenant used for
// the ACL comes from that credential, never from the topic. Usernames the
// registry does not know get "ignore", so EMQX moves on to its next
// authenticator or authorization source (the host application's own
// credential is not an edge-node credential and is not decided here).
package brokerauth

import (
	"context"
	"errors"
	"fmt"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// Decision is an EMQX authentication or authorization result.
type Decision string

// Decisions, as EMQX's HTTP authenticator and authorizer expect them.
const (
	Allow  Decision = "allow"
	Deny   Decision = "deny"
	Ignore Decision = "ignore"
)

// Credential is one edge node's broker credential as the registry stores it.
type Credential struct {
	Username     string
	Group        string // Sparkplug group_id: the tenant slug
	EdgeNodeID   string
	PasswordHash string // bcrypt
	Disabled     bool
}

// CredentialStore looks credentials up by MQTT username.
type CredentialStore interface {
	// LookupCredential returns the credential for username, or nil when the
	// username is not an edge-node credential.
	LookupCredential(ctx context.Context, username string) (*Credential, error)
}

// Password rules for self-generated edge-node credentials. bcrypt reads at
// most 72 bytes, and a site box generates 32 random bytes (43 characters in
// base64url), so anything shorter than 32 characters is not machine
// generated.
const (
	MinPasswordLength = 32
	MaxPasswordLength = 72
)

// DefaultCost is the bcrypt cost for stored credential hashes.
const DefaultCost = 12

// ErrWeakPassword is returned for a password that does not meet the rules.
var ErrWeakPassword = errors.New("brokerauth: password must be 32 to 72 printable ASCII characters")

// ValidatePassword checks the self-generated credential rules.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength || len(password) > MaxPasswordLength {
		return ErrWeakPassword
	}
	for _, r := range password {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || r == ' ' {
			return ErrWeakPassword
		}
	}
	return nil
}

// HashPassword validates and hashes a credential with bcrypt at cost.
func HashPassword(password string, cost int) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", fmt.Errorf("brokerauth: hash password: %w", err)
	}
	return string(h), nil
}

// Authorizer makes the decisions.
type Authorizer struct {
	Store CredentialStore
}

// Authenticate decides a CONNECT. Unknown usernames are ignored; a known
// username with a wrong password, or a disabled credential, is denied.
func (a *Authorizer) Authenticate(ctx context.Context, username, password string) (Decision, error) {
	if username == "" {
		return Ignore, nil
	}
	cred, err := a.Store.LookupCredential(ctx, username)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return Ignore, nil
	}
	if cred.Disabled || cred.PasswordHash == "" {
		return Deny, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(password)) != nil {
		return Deny, nil
	}
	return Allow, nil
}

// Authorize decides a publish or subscribe with the MES-1 edge-node ACL of
// the credential's own group and edge node. Unknown usernames are ignored;
// a disabled credential is denied everything.
func (a *Authorizer) Authorize(ctx context.Context, username string, action sparkplug.ACLAction, topic string) (Decision, error) {
	if username == "" {
		return Ignore, nil
	}
	cred, err := a.Store.LookupCredential(ctx, username)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return Ignore, nil
	}
	if cred.Disabled {
		return Deny, nil
	}
	if action != sparkplug.ACLPublish && action != sparkplug.ACLSubscribe {
		return Deny, nil
	}
	rules, err := sparkplug.EdgeNodeACL(cred.Group, cred.EdgeNodeID)
	if err != nil {
		return Deny, nil // a registry row that cannot form topics grants nothing
	}
	if sparkplug.Permits(rules, action, topic) {
		return Allow, nil
	}
	return Deny, nil
}
