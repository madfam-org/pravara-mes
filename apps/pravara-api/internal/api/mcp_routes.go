package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/mcp"
	mw "github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// MCPPath is the native MCP endpoint on the API host (RFC 0051 M1).
const MCPPath = "/mcp"

// mcpInstructions is the initialize instructions text.
const mcpInstructions = "Pravara MES reads for the cost ledger: per-job consumption quantities (material and machine " +
	"time) and inventory levels. Quantities only, never money: the cost ledger values consumption. " +
	"Every result belongs to the tenant of your token."

// newMCPServer builds the MCP server over the Pravara tools.
func newMCPServer(cfg config.MCPConfig, log *logrus.Logger, consumption *repositories.JobConsumptionRepository,
	inventory *repositories.InventoryRepository) (*mcp.Server, error) {
	tools := &mcpTools{consumption: consumption, inventory: inventory, slugReaders: consumptionSlugReaders()}
	return mcp.NewServer(mcp.Config{
		Name:          "pravara-mes",
		Title:         "Pravara MES",
		Version:       "1.0.0",
		Instructions:  mcpInstructions,
		Scope:         mw.ScopeCogsRead,
		RatePerMinute: cfg.RatePerMinute,
		Burst:         cfg.Burst,
		Log:           log,
	}, tools.tools()...)
}

// registerMCPRoutes mounts POST /mcp when PRAVARA_MCP_ENABLED is set; off
// (the default) the path is not registered and answers 404.
//
// Gates, in order: a browser Origin → 403; the Janua token, verified by
// the same middleware as the HTTP API (issuer, JWKS, audience
// cfg.OIDC.Audience, tenant_id claim, the token's tenant transaction) → 401
// or 403; not a machine token → 403; no pravara-cogs:read → 403
// insufficient_scope; the client's rate → 429; then the JSON-RPC message.
// API keys are not accepted (they are not Janua tokens).
func registerMCPRoutes(router *gin.Engine, database *db.DB, verifier *auth.OIDCVerifier, cfg *config.Config,
	log *logrus.Logger, consumption *repositories.JobConsumptionRepository, inventory *repositories.InventoryRepository) {
	if !cfg.MCP.Enabled {
		return
	}
	server, err := newMCPServer(cfg.MCP, log, consumption, inventory)
	if err != nil {
		log.WithError(err).Error("Native MCP not mounted: invalid tool set")
		return
	}
	router.POST(MCPPath, server.Prelude, mw.AuthMiddleware(verifier, database, log), server.Handle)
	router.GET(MCPPath, mcp.MethodNotAllowed)
	router.DELETE(MCPPath, mcp.MethodNotAllowed)
	log.WithField("tools", len(server.Tools())).Info("Native MCP mounted at POST /mcp")
}

// MCPRouteCoverage classifies one COGS-relevant HTTP route for the native
// MCP's coverage contract (RFC 0051 §3): COVERED by Tool, or EXEMPTED with
// a reason. Exactly one of the two is set.
type MCPRouteCoverage struct {
	Tool   string
	Exempt string
}

// mcpCOGSPathPrefixes are the route families the coverage contract spans:
// job consumption, inventory, dispatch and machine control, and the
// ForgeSight webhook. TestMCPCoverageClassifiesEveryCOGSRoute fails when a
// route under one of them is registered without an entry below.
var mcpCOGSPathPrefixes = []string{
	"/v1/jobs",
	"/v1/inventory",
	"/v1/tasks/:id/consumption",
	"/v1/match",
	"/v1/dispatches",
	"/v1/machines/:id/command",
	"/v1/webhooks/forgesight",
}

// isMCPCOGSRoute reports whether path belongs to a covered family.
func isMCPCOGSRoute(path string) bool {
	for _, p := range mcpCOGSPathPrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// MCPCoverage is the coverage contract over the COGS-relevant routes.
func MCPCoverage() map[string]MCPRouteCoverage {
	k := mw.RouteKey
	get, post, patch := http.MethodGet, http.MethodPost, http.MethodPatch
	covered := func(tool string) MCPRouteCoverage { return MCPRouteCoverage{Tool: tool} }
	exempt := func(reason string) MCPRouteCoverage { return MCPRouteCoverage{Exempt: reason} }
	return map[string]MCPRouteCoverage{
		// Job consumption.
		k(get, "/v1/jobs/consumption"):       covered(MCPToolJobConsumption),
		k(post, "/v1/tasks/:id/consumption"): exempt("write: an operator actual moves stock; people only"),
		// Inventory (quantities only over MCP; cost fields stay on HTTP).
		k(get, "/v1/inventory"):             covered(MCPToolInventoryLevels),
		k(get, "/v1/inventory/:id"):         covered(MCPToolInventoryLevels),
		k(get, "/v1/inventory/low-stock"):   exempt("reorder alerting, not a cost input; the same quantities come from mes_inventory_levels"),
		k(post, "/v1/inventory"):            exempt("write: creates stock items; people only"),
		k(patch, "/v1/inventory/:id"):       exempt("write: edits stock items and their cost fields; people only"),
		k(post, "/v1/inventory/:id/adjust"): exempt("manual adjust: moves stock; people only"),
		k(get, "/v1/jobs/consumption/coverage"): exempt("a data-quality gate over the same rows; " +
			"each mes_job_consumption row carries its registry_code"),
		// RFC 0058 wave 1 (docs/operations/inventory.md). Reads stay on HTTP
		// until the cost ledger's wave 2 needs them over MCP (ROADMAP item 22).
		k(get, "/v1/inventory/materials"): exempt("registry reference data; its codes reach the ledger as " +
			"item.material_code and as material_code on mes_inventory_levels"),
		k(get, "/v1/inventory/materials/:id"): exempt("registry reference data (aliases, conversions); " +
			"see GET /v1/inventory/materials"),
		k(get, "/v1/inventory/materials/unlinked"): exempt("registry housekeeping for people, not a cost input"),
		k(get, "/v1/inventory/conversion-gaps"):    exempt("housekeeping for people: quantities waiting for a unit conversion"),
		k(get, "/v1/inventory/receipts"): exempt("receipt quantities; the cost ledger takes purchase lines " +
			"from Karafiel (RFC 0058 wave 2 decides a receipt read)"),
		k(get, "/v1/inventory/receipts/:id"):               exempt("see GET /v1/inventory/receipts"),
		k(get, "/v1/inventory/counts"):                     exempt("count records for people; the stock they adjust is mes_inventory_levels"),
		k(get, "/v1/inventory/counts/:id"):                 exempt("see GET /v1/inventory/counts"),
		k(post, "/v1/inventory/materials"):                 exempt("write: registers a material code; people only"),
		k(patch, "/v1/inventory/materials/:id"):            exempt("write: edits a registry entry; people only"),
		k(post, "/v1/inventory/materials/:id/aliases"):     exempt("write: changes how codes resolve; people only"),
		k(post, "/v1/inventory/materials/:id/conversions"): exempt("write: adds a conversion and posts waiting stock; people only"),
		k(post, "/v1/inventory/receipts"):                  exempt("write: a receipt raises stock; people only"),
		k(post, "/v1/inventory/counts"):                    exempt("write: opens a physical count; people only"),
		k(post, "/v1/inventory/counts/:id/lines"):          exempt("write: records a counted quantity; people only"),
		k(post, "/v1/inventory/counts/:id/close"):          exempt("write: posts count adjustments; people only"),
		// Fabrication dispatch and machine control.
		k(post, "/v1/match"):                exempt("dispatch planning (a dry run), not a cost input"),
		k(post, "/v1/dispatches"):           exempt("dispatch: ends in start_job on a printer (machine control, irreversible)"),
		k(get, "/v1/dispatches"):            exempt("dispatch control plane; the consumption it produces is mes_job_consumption"),
		k(get, "/v1/dispatches/:id"):        exempt("dispatch control plane; the consumption it produces is mes_job_consumption"),
		k(post, "/v1/machines/:id/command"): exempt("machine control: moves equipment; people only"),
		// Inbound webhook.
		k(post, "/v1/webhooks/forgesight"): exempt("inbound webhook from ForgeSight, not a read"),
	}
}
