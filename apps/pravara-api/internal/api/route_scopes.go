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
	// The terminal's reads (RFC 0064, ruling PV-10): TerminalReadRoutes.
	terminal := mw.Require(mw.ScopeTerminal)
	ordersRead := mw.Require(mw.ScopeRead, mw.ScopeJobs, mw.ScopeTerminal)
	eventHistory := mw.Require(mw.ScopeRead, mw.ScopeTerminal).WithLegacy(mw.LegacyScopeReadEvents)

	get, post, patch, put := http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut
	k := mw.RouteKey

	return mw.RouteScopes{
		// Order and job intake.
		k(get, "/v1/orders"):            ordersRead,
		k(post, "/v1/orders"):           jobs,
		k(get, "/v1/orders/:id"):        ordersRead,
		k(get, "/v1/orders/:id/items"):  ordersRead,
		k(post, "/v1/orders/:id/items"): jobs,

		// A shop-floor terminal's reads (RFC 0064, ruling PV-10), under
		// pravara-mes:terminal only. Every task write, the consumption
		// report and the step acknowledgement stay unlisted: an operator's
		// act carries a person's authority, never a machine scope (PV-1).
		k(get, "/v1/tasks"):                       terminal,
		k(get, "/v1/tasks/board"):                 terminal,
		k(get, "/v1/tasks/:id"):                   terminal,
		k(get, "/v1/tasks/:id/work-instructions"): terminal,
		k(get, "/v1/quality/batches/lookup"):      terminal,

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

		// --- fabrication dispatch (MES-1 §5-§7) ---
		// POST /v1/match is a dry run (reserves nothing), so it is a read.
		// POST /v1/dispatches is deliberately absent: it ends in start_job on
		// a printer, so it is people-only like POST /v1/machines/:id/command.
		k(post, "/v1/match"):         jobsRead,
		k(get, "/v1/dispatches"):     jobsRead,
		k(get, "/v1/dispatches/:id"): jobsRead,
		// The kept bytes of a dispatch's render (RFC 0059 S12): the
		// geometry itself, so machine callers need pravara-mes:jobs, not a
		// bare read scope. People are governed by roles, as on the
		// dispatch read.
		k(get, "/v1/dispatches/:id/artifacts/:kind"): jobs,
		// --- end fabrication dispatch ---

		// Job consumption reads for downstream cost ledgers (quantities only).
		// pravara-cogs:read is the cost ledger's scope (RFC 0051 M2): the
		// same read the MCP tool mes_job_consumption serves, so a COGS client
		// sees engagement slugs over HTTP exactly as over MCP.
		// POST /v1/tasks/:id/consumption, POST /v1/jobs/consumption/:id/corrections
		// and POST /v1/print-jobs/:id/complete are deliberately absent: they
		// move stock, so they are people-only like the inventory writes.
		k(get, "/v1/jobs/consumption"):          mw.Require(mw.ScopeRead, mw.ScopeJobs, mw.ScopeCogsRead),
		k(get, "/v1/jobs/consumption/coverage"): mw.Require(mw.ScopeRead, mw.ScopeJobs, mw.ScopeCogsRead),

		// Event history and feeds.
		k(get, "/v1/events"):                        eventHistory,
		k(get, "/v1/events/types"):                  eventHistory,
		k(get, "/v1/events/:id"):                    eventHistory,
		k(get, "/v1/events/stream"):                 events,
		k(get, "/v1/feeds/crm/orders"):              feeds,
		k(get, "/v1/feeds/crm/orders/:id/timeline"): feeds,
		k(get, "/v1/feeds/crm/orders/:id/status"):   feeds,
		k(get, "/v1/feeds/social/milestones"):       feeds,
		k(get, "/v1/feeds/social/stats"):            feeds,
		k(get, "/v1/feeds/social/highlights"):       feeds,
		k(get, "/v1/feeds/status/detailed"):         status,
		k(get, "/v1/feeds/status/incidents"):        status,

		// Inventory reads (stock checks by SKU or name). Writes (create,
		// update, adjust) stay unlisted: people and wildcard API keys only.
		k(get, "/v1/inventory"):     read,
		k(get, "/v1/inventory/:id"): read,

		// RFC 0058 wave 1 reads (quantities only). The registry's codes are
		// what the cost ledger references (ruling I3), so it also accepts
		// pravara-cogs:read. Every write (materials, aliases, conversions,
		// receipts, counts) moves stock or changes what is stock: people and
		// wildcard API keys only.
		k(get, "/v1/inventory/materials"):          mw.Require(mw.ScopeRead, mw.ScopeCogsRead),
		k(get, "/v1/inventory/materials/:id"):      mw.Require(mw.ScopeRead, mw.ScopeCogsRead),
		k(get, "/v1/inventory/materials/unlinked"): read,
		k(get, "/v1/inventory/conversion-gaps"):    read,
		k(get, "/v1/inventory/receipts"):           read,
		k(get, "/v1/inventory/receipts/:id"):       read,
		k(get, "/v1/inventory/counts"):             read,
		k(get, "/v1/inventory/counts/:id"):         read,
	}
}

// TerminalReadRoutes are exactly the routes pravara-mes:terminal opens (RFC
// 0064, ruling PV-10): the order reads, the task reads, a task's work
// instructions, the lot lookup by number and the event history. The live
// event stream is not one of them: a terminal catches up from the history.
// TestTerminalScopeOpensExactlyTheTerminalReads holds MachineRouteScopes to
// this list in both directions, and the terminal contract
// (docs/contracts/terminal.v1.json) lists the same routes.
func TerminalReadRoutes() []string {
	k := mw.RouteKey
	get := http.MethodGet
	return []string{
		k(get, "/v1/orders"),
		k(get, "/v1/orders/:id"),
		k(get, "/v1/orders/:id/items"),
		k(get, "/v1/tasks"),
		k(get, "/v1/tasks/board"),
		k(get, "/v1/tasks/:id"),
		k(get, "/v1/tasks/:id/work-instructions"),
		k(get, "/v1/quality/batches/lookup"),
		k(get, "/v1/events"),
		k(get, "/v1/events/types"),
		k(get, "/v1/events/:id"),
	}
}

// IdempotentWriteRoutes honour an Idempotency-Key header
// (middleware.Idempotent; RFC 0064 §5, ruling PV-4): the writes a shop-floor
// terminal delivers at least once, each on a person's authority. The
// terminal contract (docs/contracts/terminal.v1.json) lists the same writes;
// TestTerminalContractWrites holds the two together. Receipts keep their own
// body field.
func IdempotentWriteRoutes() []string {
	k := mw.RouteKey
	post := http.MethodPost
	return []string{
		k(post, "/v1/tasks/:id/move"),
		k(post, "/v1/tasks/:id/consumption"),
		k(post, "/v1/tasks/:id/work-instructions/:wiId/acknowledge"),
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
		// RFC 0058 inventory record writes (and an inventory role:
		// middleware.RequireInventoryWriter).
		mw.RouteKey(http.MethodPost, "/v1/inventory/materials"),
		mw.RouteKey(http.MethodPatch, "/v1/inventory/materials/:id"),
		mw.RouteKey(http.MethodPost, "/v1/inventory/materials/:id/aliases"),
		mw.RouteKey(http.MethodPost, "/v1/inventory/materials/:id/conversions"),
		mw.RouteKey(http.MethodPost, "/v1/inventory/receipts"),
		mw.RouteKey(http.MethodPost, "/v1/inventory/counts"),
		mw.RouteKey(http.MethodPost, "/v1/inventory/counts/:id/lines"),
		mw.RouteKey(http.MethodPost, "/v1/inventory/counts/:id/close"),
	}
}
