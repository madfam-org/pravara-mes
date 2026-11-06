package api

// The native MCP's coverage contract and drift guard (RFC 0051 §3): every
// COGS-relevant route is COVERED by a tool or EXEMPTED with a reason; tool
// arguments match the published OpenAPI; tool outputs are projections of
// the HTTP rows (no field the HTTP route does not return), and the fields
// RFC 0051 keeps out of MCP outputs stay out.

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/mcp"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

func mcpToolsByName() map[string]mcp.Tool {
	out := map[string]mcp.Tool{}
	for _, tool := range (&mcpTools{slugReaders: consumptionSlugReaders()}).tools() {
		out[tool.Name] = tool
	}
	return out
}

// A new route in a COGS family (jobs, inventory, dispatch, machine control,
// the ForgeSight webhook) fails here until it is COVERED or EXEMPTED.
func TestMCPCoverageClassifiesEveryCOGSRoute(t *testing.T) {
	coverage := MCPCoverage()
	seen := 0
	for _, r := range registeredRouter(t).Routes() {
		if !isMCPCOGSRoute(r.Path) {
			continue
		}
		seen++
		key := middleware.RouteKey(r.Method, r.Path)
		entry, ok := coverage[key]
		if !assert.True(t, ok, "%s is a COGS-relevant route: add it to MCPCoverage as COVERED by a tool or EXEMPTED with a reason", key) {
			continue
		}
		assert.True(t, (entry.Tool == "") != (entry.Exempt == ""), "%s must be exactly one of COVERED or EXEMPTED", key)
	}
	assert.Equal(t, len(coverage), seen, "every MCPCoverage entry matches a registered route")
}

func TestMCPCoverageEntriesAreRegisteredCOGSRoutes(t *testing.T) {
	routes := registeredV1Routes(t)
	for key := range MCPCoverage() {
		assert.True(t, routes[key], "MCPCoverage entry %q matches no registered route", key)
		assert.True(t, isMCPCOGSRoute(strings.SplitN(key, " ", 2)[1]), "%q is outside the COGS route families", key)
	}
}

// Every tool covers an HTTP read that machine callers may already make, and
// every COVERED route names a tool that exists: no tool without an HTTP
// equivalent (no back door).
func TestMCPToolsAndCoverageAgree(t *testing.T) {
	tools := mcpToolsByName()
	machine := MachineRouteScopes()
	used := map[string]bool{}
	for key, entry := range MCPCoverage() {
		if entry.Tool == "" {
			continue
		}
		used[entry.Tool] = true
		_, ok := tools[entry.Tool]
		assert.True(t, ok, "%s is COVERED by unknown tool %q", key, entry.Tool)
		assert.True(t, strings.HasPrefix(key, "GET "), "%s: only reads are covered in Phase A", key)
		_, listed := machine[key]
		assert.True(t, listed, "%s: a covered route must already accept machine callers over HTTP", key)
	}
	for name := range tools {
		assert.True(t, used[name], "tool %s covers no HTTP route", name)
	}
}

func TestMCPCoverageExemptsWritesDispatchAndMachineControl(t *testing.T) {
	coverage := MCPCoverage()
	for _, key := range []string{
		"POST /v1/inventory/:id/adjust", "POST /v1/inventory", "PATCH /v1/inventory/:id",
		"POST /v1/tasks/:id/consumption", "POST /v1/dispatches", "POST /v1/machines/:id/command",
	} {
		assert.NotEmpty(t, coverage[key].Exempt, "%s must be EXEMPTED", key)
		assert.Empty(t, coverage[key].Tool, key)
	}
}

func schemaProps(t *testing.T, schema map[string]any) map[string]map[string]any {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	out := map[string]map[string]any{}
	for k, v := range props {
		out[k] = v.(map[string]any)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The tool's arguments are the published query parameters of
// GET /v1/jobs/consumption (docs/swagger.json), with the same types.
func TestMCPJobConsumptionSchemaMatchesOpenAPI(t *testing.T) {
	raw, err := os.ReadFile("../../../../docs/swagger.json")
	require.NoError(t, err)
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name, In, Type string
			} `json:"parameters"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	op, ok := doc.Paths["/jobs/consumption"]["get"]
	require.True(t, ok, "GET /jobs/consumption is in the OpenAPI document")

	published := map[string]string{}
	for _, p := range op.Parameters {
		if p.In == "query" {
			published[p.Name] = p.Type
		}
	}
	props := schemaProps(t, mcpToolsByName()[MCPToolJobConsumption].InputSchema)
	assert.Equal(t, sortedKeys(published), sortedKeys(props))
	assert.ElementsMatch(t, jobConsumptionParams, sortedKeys(props))
	for name, typ := range published {
		assert.Equal(t, typ, props[name]["type"], name)
	}
}

func TestMCPInventoryLevelsSchema(t *testing.T) {
	props := schemaProps(t, mcpToolsByName()[MCPToolInventoryLevels].InputSchema)
	assert.ElementsMatch(t, inventoryLevelsParams, sortedKeys(props))
}

func jsonNames(t *testing.T, v any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	typ := reflect.TypeOf(v)
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}

// Outputs are projections of the HTTP rows, the output schemas describe
// them, and the excluded fields stay excluded.
func TestMCPOutputsAreProjectionsOfTheHTTPRows(t *testing.T) {
	for _, tc := range []struct {
		tool     string
		mcpRow   any
		httpRow  any
		excluded []string
	}{
		{MCPToolJobConsumption, mcpConsumptionRow{}, repositories.JobConsumption{}, []string{"recorded_by", "tenant_id"}},
		{MCPToolInventoryLevels, mcpInventoryLevel{}, repositories.InventoryItem{},
			[]string{"unit_cost", "currency", "forgesight_id", "metadata", "description", "tenant_id"}},
	} {
		mcpNames, httpNames := jsonNames(t, tc.mcpRow), jsonNames(t, tc.httpRow)
		for name := range mcpNames {
			assert.True(t, httpNames[name], "%s returns %q, which the HTTP route does not", tc.tool, name)
		}
		for _, name := range tc.excluded {
			assert.False(t, mcpNames[name], "%s must not return %q", tc.tool, name)
		}
		out := mcpToolsByName()[tc.tool].OutputSchema
		rows := out["properties"].(map[string]any)["data"].(map[string]any)["items"].(map[string]any)
		assert.Equal(t, sortedKeys(mcpNames), sortedKeys(schemaProps(t, rows)), "%s output schema", tc.tool)
	}
}

// Engagement slugs follow the HTTP route's scopes: with a requirement the
// caller does not meet, the slug filter is refused and slugs are dropped.
func TestMCPSlugsFollowTheHTTPScopes(t *testing.T) {
	cogs := middleware.Caller{Kind: middleware.CallerServiceAccount, ID: "jnc_cogs", Scopes: []string{middleware.ScopeCogsRead}}
	assert.True(t, consumptionSlugReaders().Allows(cogs), "GET /v1/jobs/consumption accepts pravara-cogs:read")

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	tools := &mcpTools{consumption: repositories.NewJobConsumptionRepository(sqlDB),
		slugReaders: middleware.Require(middleware.ScopeRead)}
	args := map[string]any{"from": "2026-10-01T00:00:00Z", "to": "2026-11-01T00:00:00Z"}

	_, err = tools.jobConsumption(context.Background(), mcp.Request{Caller: cogs,
		Args: map[string]any{"from": args["from"], "to": args["to"], "engagement_slug": "eng-alpha"}})
	var argErr *mcp.ArgumentError
	require.ErrorAs(t, err, &argErr)

	now := time.Now()
	mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`ORDER BY c.occurred_at`).WillReturnRows(sqlmock.NewRows(consumptionCols).AddRow(uuid.New(), uuid.New(),
		"task", uuid.New(), nil, nil, nil, uuid.New(), nil, "machine_time", "", "", 3.0, "min", "actual", "printer", "", nil,
		now, now, "eng-alpha"))
	out, err := tools.jobConsumption(context.Background(), mcp.Request{Caller: cogs, Args: args})
	require.NoError(t, err)
	page := out.(mcpPage[mcpConsumptionRow])
	require.Len(t, page.Data, 1)
	assert.Empty(t, page.Data[0].EngagementSlug)
	require.NoError(t, mock.ExpectationsWereMet())
}
