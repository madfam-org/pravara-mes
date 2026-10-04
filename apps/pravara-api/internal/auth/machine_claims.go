package auth

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Janua mints two shapes of access token for the pravara-api audience:
//
//   - HUMAN (authorization_code / refresh): a UUID `sub`, `tenant_id`, `roles`;
//     no `token_use`, no `actor_type`.
//   - MACHINE (client_credentials): `token_use: "client_credentials"`,
//     `actor_type: "service_account"`, `sub: "service-account:<client_id>"`,
//     `client_id`, and a space-delimited `scope`. Organization-bound clients
//     also carry `tenant_id` (the organization id).
//
// See janua docs/service-tokens.md ("Token shape").

const (
	// TokenUseClientCredentials is the `token_use` value on machine tokens.
	TokenUseClientCredentials = "client_credentials"
	// ActorTypeServiceAccount is the `actor_type` value on machine tokens.
	ActorTypeServiceAccount = "service_account"
	// ServiceAccountSubjectPrefix prefixes the `sub` of machine tokens.
	ServiceAccountSubjectPrefix = "service-account:"
)

// ScopeClaim is the OAuth `scope` claim. Janua emits a space-delimited string;
// the JSON-array form some issuers use is accepted too.
type ScopeClaim []string

// UnmarshalJSON accepts a space-delimited string, an array of strings or null.
func (s *ScopeClaim) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" || trimmed == "" {
		*s = nil
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("scope claim: %w", err)
		}
		out := make([]string, 0, len(list))
		for _, v := range list {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		*s = out
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("scope claim: %w", err)
	}
	*s = strings.Fields(str)
	return nil
}

// MarshalJSON writes the space-delimited string form.
func (s ScopeClaim) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.Join(s, " "))
}

// IsMachineToken reports whether the claims came from a client_credentials
// grant. Any one machine marker is sufficient, so a partial claim set is
// classified as a machine (and must then pass scope checks) rather than
// slipping through the human path.
func (c *Claims) IsMachineToken() bool {
	if c == nil {
		return false
	}
	return c.TokenUse == TokenUseClientCredentials ||
		c.ActorType == ActorTypeServiceAccount ||
		strings.HasPrefix(c.Subject, ServiceAccountSubjectPrefix)
}

// ServiceClientID returns the machine client's identifier: the `client_id`
// claim, else the `sub` without its service-account prefix.
func (c *Claims) ServiceClientID() string {
	if c == nil {
		return ""
	}
	if c.ClientID != "" {
		return c.ClientID
	}
	return strings.TrimPrefix(c.Subject, ServiceAccountSubjectPrefix)
}
