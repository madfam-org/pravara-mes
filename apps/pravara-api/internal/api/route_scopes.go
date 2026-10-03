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

	get, post, patch := http.MethodGet, http.MethodPost, http.MethodPatch
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
