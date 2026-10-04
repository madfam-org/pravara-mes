package machineclients

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
)

// Janua audiences and scopes of the three clients pravara holds (janua#685
// and the fabrication-prep registration). Janua binds the audience to the
// client; pravara requests the scope.
const (
	Yantra4DAudience = "yantra4d-api"
	Yantra4DScope    = "yantra4d:render"

	FabricationPrepAudience = "fabrication-prep-api"
	FabricationPrepScope    = "fabrication-prep:slice"

	AssetShellsAudience = "asset-shells-api"
	AssetShellsScope    = "asset-shells:publish-instances asset-shells:read"
)

// Set is the machine clients of one pravara-api process.
type Set struct {
	Yantra4D        *TokenSource
	FabricationPrep *TokenSource
	// assetShells maps a pravara tenant slug to its org-bound publisher.
	assetShells map[string]*TokenSource
}

// SetConfig is the input of NewSet (built from config.MachineClientsConfig).
type SetConfig struct {
	TokenURL                    string
	Yantra4DClientID            string
	Yantra4DClientSecret        string
	FabricationPrepClientID     string
	FabricationPrepClientSecret string
	// AssetShellsTenantPrefixes maps tenant slug → environment prefix.
	AssetShellsTenantPrefixes map[string]string
	// Getenv reads <PREFIX>_CLIENT_ID/_SECRET; nil means os.Getenv.
	Getenv func(string) string
}

// NewSet builds the clients. Missing credentials are allowed (the client
// reports ErrNotConfigured when used); Status says which ones are missing.
func NewSet(cfg SetConfig, httpClient *http.Client) *Set {
	getenv := cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	s := &Set{
		Yantra4D: NewTokenSource(Credentials{
			Name: "yantra4d-step-reader", TokenURL: cfg.TokenURL,
			ClientID: cfg.Yantra4DClientID, ClientSecret: cfg.Yantra4DClientSecret,
			Scope: Yantra4DScope, Audience: Yantra4DAudience,
		}, httpClient),
		FabricationPrep: NewTokenSource(Credentials{
			Name: "fabrication-prep", TokenURL: cfg.TokenURL,
			ClientID: cfg.FabricationPrepClientID, ClientSecret: cfg.FabricationPrepClientSecret,
			Scope: FabricationPrepScope, Audience: FabricationPrepAudience,
		}, httpClient),
		assetShells: map[string]*TokenSource{},
	}
	for slug, prefix := range cfg.AssetShellsTenantPrefixes {
		s.assetShells[slug] = NewTokenSource(Credentials{
			Name:         "asset-shells-publisher/" + slug,
			TokenURL:     cfg.TokenURL,
			ClientID:     strings.TrimSpace(getenv(prefix + "_CLIENT_ID")),
			ClientSecret: getenv(prefix + "_CLIENT_SECRET"),
			Scope:        AssetShellsScope,
			Audience:     AssetShellsAudience,
		}, httpClient)
	}
	return s
}

// ErrTenantNotMapped is returned when a tenant has no asset-shells publisher.
var ErrTenantNotMapped = fmt.Errorf("no asset-shells publisher client is mapped for this tenant")

// AssetShellsPublisher returns the org-bound publisher of a pravara tenant.
func (s *Set) AssetShellsPublisher(tenantSlug string) (*TokenSource, error) {
	ts, ok := s.assetShells[tenantSlug]
	if !ok {
		return nil, fmt.Errorf("%w: %q (set ASSET_SHELLS_PUBLISHER_TENANTS)", ErrTenantNotMapped, tenantSlug)
	}
	if !ts.Configured() {
		return nil, fmt.Errorf("%w: %s", ErrNotConfigured, ts.Name())
	}
	return ts, nil
}

// Status lists each client and whether it has credentials, for startup logs
// and readiness detail. It never includes ids or secrets.
func (s *Set) Status() map[string]bool {
	out := map[string]bool{
		s.Yantra4D.Name():        s.Yantra4D.Configured(),
		s.FabricationPrep.Name(): s.FabricationPrep.Configured(),
	}
	for _, ts := range s.assetShells {
		out[ts.Name()] = ts.Configured()
	}
	return out
}

// Missing lists the clients without credentials, sorted.
func (s *Set) Missing() []string {
	var out []string
	for name, ok := range s.Status() {
		if !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
