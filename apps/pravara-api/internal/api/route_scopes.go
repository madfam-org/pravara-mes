package api

import (
	"net/http"

	mw "github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// MachineRouteScopes is the route→scope matrix for machine callers (Janua
// client_credentials tokens and API keys). People signed in through Janua are
// not affected: roles govern them.
//
// Rules:
//   - A route that is not listed accepts no Janua machine token, and only API
//     keys holding the wildcard scope.
//   - A write scope also covers reads of its own resource family, so an intake
//     client can read back what it wrote.
//   - Legacy API-key scope names keep working on the routes they always
//     covered (events and feeds).
//
// TestMachineRouteScopesMatchRegisteredRoutes keeps this table in step with
// RegisterRoutesAll.
func MachineRouteScopes() mw.RouteScopes {
	read := mw.Require(mw.ScopeRead)
	jobs := mw.Require(mw.ScopeJobs)
	jobsRead := mw.Require(mw.ScopeRead, mw.ScopeJobs)
	nodes := mw.Require(mw.ScopeNodes)
	nodesRead := mw.Require(mw.ScopeRead, mw.ScopeNodes)
	passports := mw.Require(mw.ScopePassports)
	passportsRead := mw.Require(mw.ScopeRead, mw.ScopePassports)
	events := read.WithLegacy(mw.LegacyScopeReadEvents)
	feeds := read.WithLegacy(mw.LegacyScopeReadFeeds)
	status := read.WithLegacy(mw.LegacyScopeReadStatus)

	get, post, patch, put := http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut
	k := mw.RouteKey

	return mw.RouteScopes{
		// Order and job intake.
		k(get, "/v1/orders"):            jobsRead,
		k(post, "/v1/orders"):           jobs,
		k(get, "/v1/orders/:id"):        jobsRead,
		k(get, "/v1/orders/:id/items"):  jobsRead,
		k(post, "/v1/orders/:id/items"): jobs,

		// Design import from yantra4d.
		k(post, "/v1/import/yantra4d"):        jobs,
		k(get, "/v1/import/yantra4d/preview"): jobsRead,

		// Quoting webhook (order intake).
		k(post, "/v1/webhooks/cotiza"): jobs,

		// Producer-node registry, heartbeats and telemetry.
		k(get, "/v1/machines"):                 nodesRead,
		k(post, "/v1/machines"):                nodes,
		k(get, "/v1/machines/:id"):             nodesRead,
		k(patch, "/v1/machines/:id"):           nodes,
		k(get, "/v1/machines/:id/telemetry"):   nodesRead,
		k(post, "/v1/machines/:id/heartbeat"):  nodes,
		k(get, "/v1/machines/:id/maintenance"): read,
		k(get, "/v1/telemetry"):                nodesRead,
		k(get, "/v1/telemetry/aggregated"):     nodesRead,
		k(get, "/v1/telemetry/latest"):         nodesRead,
		k(post, "/v1/telemetry/batch"):         nodes,

		// Sparkplug edge-node registry and live machine state (edge_routes.go).
		// Enrollment approval and credential revocation are people only
		// (HumanOnlyRoutes); the box's own enrollment calls are NonJWTRoutes.
		k(get, "/v1/edge/nodes"):              nodesRead,
		k(get, "/v1/edge/live-state"):         nodesRead,
		k(put, "/v1/machines/:id/sparkplug"):  nodes,
		k(get, "/v1/machines/:id/live-state"): nodesRead,

		// Passport and genealogy.
		k(get, "/v1/genealogy"):           passportsRead,
		k(post, "/v1/genealogy"):          passports,
		k(get, "/v1/genealogy/:id"):       passportsRead,
		k(patch, "/v1/genealogy/:id"):     passports,
		k(post, "/v1/genealogy/:id/seal"): passports,
		k(get, "/v1/genealogy/:id/tree"):  passportsRead,

		// Event history and feeds.
		k(get, "/v1/events"):                        events,
		k(get, "/v1/events/types"):                  events,
		k(get, "/v1/events/:id"):                    events,
		k(get, "/v1/events/stream"):                 events,
		k(get, "/v1/feeds/crm/orders"):              feeds,
		k(get, "/v1/feeds/crm/orders/:id/timeline"): feeds,
		k(get, "/v1/feeds/crm/orders/:id/status"):   feeds,
		k(get, "/v1/feeds/social/milestones"):       feeds,
		k(get, "/v1/feeds/social/stats"):            feeds,
		k(get, "/v1/feeds/social/highlights"):       feeds,
		k(get, "/v1/feeds/status/detailed"):         status,
		k(get, "/v1/feeds/status/incidents"):        status,
	}
}

// NonJWTRoute documents a /v1 route that takes neither a Janua token nor an
// API key, and what protects it instead.
type NonJWTRoute struct {
	// Listener is "public" (the main API port) or "internal" (the in-cluster
	// port of NewInternalRouter, never routed by the public ingress).
	Listener string
	// Guard is what authenticates or bounds the caller.
	Guard string
}

// NonJWTRoutes lists every /v1 route outside the authenticated group.
// TestNonJWTRoutesAreExactlyTheUnauthenticatedRoutes fails when a route is
// added outside the authenticated group without being listed here, or when
// an entry goes stale.
func NonJWTRoutes() map[string]NonJWTRoute {
	k := mw.RouteKey
	return map[string]NonJWTRoute{
		// EMQX HTTP authentication and authorization (MES-1 §3).
		k(http.MethodPost, "/v1/mqtt/auth"): {"internal", "shared internal key from the Secret (X-Pravara-Internal-Key)"},
		k(http.MethodPost, "/v1/mqtt/acl"):  {"internal", "shared internal key from the Secret (X-Pravara-Internal-Key)"},
		// Edge enrollment: the site box has no credential yet. It registers
		// a self-generated credential (stored hashed) that only becomes
		// active after a person approves its user code.
		k(http.MethodPost, "/v1/edge/enrollments"):    {"public", "per-address rate limit; inactive until approved by a person"},
		k(http.MethodGet, "/v1/edge/enrollments/:id"): {"public", "returns status only for an unguessable enrollment id"},
		// Centrifugo proxy callbacks (pre-existing).
		k(http.MethodPost, "/v1/realtime/auth"):      {"public", "Centrifugo connect proxy"},
		k(http.MethodPost, "/v1/realtime/subscribe"): {"public", "Centrifugo subscribe proxy"},
	}
}

// HumanOnlyRoutes may only be called by a person signed in through Janua
// (middleware.RequireHumanCaller): machine credentials are refused even with
// the wildcard scope.
func HumanOnlyRoutes() []string {
	return []string{
		mw.RouteKey(http.MethodPost, "/v1/edge/enrollments/approve"),
		mw.RouteKey(http.MethodPost, "/v1/edge/nodes/:id/disable"),
	}
}
