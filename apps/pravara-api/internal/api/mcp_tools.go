package api

// The native MCP's tools (RFC 0051, wave A2): the cost ledger's reads of
// Pravara, quantities only. Each tool calls the same repository method, in
// the same tenant transaction, as the HTTP route it covers
// (mcp_coverage.go); its output is a projection of that route's rows (never
// a field the route does not return). There are no write tools.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/mcp"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// MCP tool names (RFC 0051 catalog, Pravara MES).
const (
	MCPToolJobConsumption  = "mes.job_consumption"
	MCPToolInventoryLevels = "mes.inventory_levels"
)

const (
	inventoryLevelsDefaultLimit = 100
	inventoryLevelsMaxLimit     = 1000
	inventoryLevelsMaxOffset    = 1 << 30
)

// inventoryLevelsParams are the arguments of mes.inventory_levels.
var inventoryLevelsParams = []string{"material_code", "limit", "offset"}

// mcpConsumptionRow is the MCP projection of repositories.JobConsumption:
// every field GET /v1/jobs/consumption returns except recorded_by (the id
// of the person who reported an actual; RFC 0051 §6 keeps personal data out
// of MCP outputs). engagement_slug is only filled for callers whose scopes
// read it over HTTP.
type mcpConsumptionRow struct {
	ID             uuid.UUID  `json:"id"`
	JobKind        string     `json:"job_kind"`
	JobID          uuid.UUID  `json:"job_id"`
	DispatchID     *uuid.UUID `json:"dispatch_id,omitempty"`
	PrintJobID     *uuid.UUID `json:"print_job_id,omitempty"`
	TaskID         *uuid.UUID `json:"task_id,omitempty"`
	OrderID        *uuid.UUID `json:"order_id,omitempty"`
	EngagementSlug string     `json:"engagement_slug,omitempty"`
	MachineID      *uuid.UUID `json:"machine_id,omitempty"`
	Kind           string     `json:"kind"`
	MaterialCode   string     `json:"material_code,omitempty"`
	MaterialLot    string     `json:"material_lot,omitempty"`
	Quantity       float64    `json:"quantity"`
	Unit           string     `json:"unit"`
	Basis          string     `json:"basis"`
	Source         string     `json:"source"`
	Note           string     `json:"note,omitempty"`
	OccurredAt     time.Time  `json:"occurred_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// mcpInventoryLevel is the MCP projection of repositories.InventoryItem:
// quantities only. unit_cost, currency, forgesight_id, description and
// metadata stay on the HTTP routes (people and pravara-mes:read).
type mcpInventoryLevel struct {
	ID                uuid.UUID `json:"id"`
	SKU               string    `json:"sku"`
	Name              string    `json:"name"`
	Category          string    `json:"category"`
	Unit              string    `json:"unit"`
	QuantityOnHand    float64   `json:"quantity_on_hand"`
	QuantityReserved  float64   `json:"quantity_reserved"`
	QuantityAvailable float64   `json:"quantity_available"`
	ReorderPoint      float64   `json:"reorder_point"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// mcpPage is a tool's paginated result (the HTTP ListResponse shape).
type mcpPage[T any] struct {
	Data   []T `json:"data"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// mcpTools holds what the tools call.
type mcpTools struct {
	consumption *repositories.JobConsumptionRepository
	inventory   *repositories.InventoryRepository
	// slugReaders is GET /v1/jobs/consumption's machine scope requirement:
	// a caller sees engagement slugs over MCP only when its scopes read
	// them over HTTP (RFC 0051 §6).
	slugReaders middleware.ScopeRequirement
}

// consumptionSlugReaders returns who reads engagement slugs over HTTP.
func consumptionSlugReaders() middleware.ScopeRequirement {
	return MachineRouteScopes()[middleware.RouteKey(http.MethodGet, "/v1/jobs/consumption")]
}

func (t *mcpTools) tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:  MCPToolJobConsumption,
			Title: "Job consumption (quantities)",
			Description: "Per-job consumption for a period, quantities only (no cost): material in g, mm, ml or sheet " +
				"and machine time in min, each with basis (actual or estimate) and source (printer, operator or slicer), " +
				"with the order's engagement slug. Same rows and filters as GET /v1/jobs/consumption, ordered by " +
				"occurred_at; from is inclusive and to exclusive. The cost ledger values these quantities.",
			InputSchema:  jobConsumptionInputSchema(),
			OutputSchema: pageSchema(jobConsumptionRowSchema()),
			Call:         t.jobConsumption,
		},
		{
			Name:  MCPToolInventoryLevels,
			Title: "Inventory levels (quantities)",
			Description: "Stock levels per inventory item: on hand, reserved, available and reorder point, in the item's " +
				"unit. Quantities only: no unit cost or currency. material_code is the item SKU (the material_code " +
				"of job consumption rows when the material is stocked). Same rows as GET /v1/inventory.",
			InputSchema:  inventoryLevelsInputSchema(),
			OutputSchema: pageSchema(inventoryLevelSchema()),
			Call:         t.inventoryLevels,
		},
	}
}

func (t *mcpTools) jobConsumption(ctx context.Context, req mcp.Request) (any, error) {
	if err := req.OnlyArgs(jobConsumptionParams...); err != nil {
		return nil, err
	}
	values := map[string]string{}
	present := map[string]bool{}
	for _, name := range jobConsumptionParams {
		read := req.String
		if name == "limit" || name == "offset" {
			read = req.Integer
		}
		v, ok, err := read(name)
		if err != nil {
			return nil, err
		}
		values[name], present[name] = v, ok
	}
	if values["from"] == "" || values["to"] == "" {
		return nil, mcp.Invalid("from and to are required (RFC 3339 timestamps; from inclusive, to exclusive)")
	}
	f, ferr := parseJobConsumptionFilter(func(name string) (string, bool) { return values[name], present[name] })
	if ferr != nil {
		return nil, mcp.Invalid("%s", ferr.Message)
	}
	seesSlugs := t.slugReaders.Allows(req.Caller)
	if f.EngagementSlug != nil && !seesSlugs {
		return nil, mcp.Invalid("engagement_slug needs a scope that reads engagement slugs over HTTP")
	}
	rows, total, err := t.consumption.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]mcpConsumptionRow, 0, len(rows))
	for _, r := range rows {
		row := mcpConsumptionRow{ID: r.ID, JobKind: r.JobKind, JobID: r.JobID, DispatchID: r.DispatchID,
			PrintJobID: r.PrintJobID, TaskID: r.TaskID, OrderID: r.OrderID, MachineID: r.MachineID, Kind: r.Kind,
			MaterialCode: r.MaterialCode, MaterialLot: r.MaterialLot, Quantity: r.Quantity, Unit: r.Unit,
			Basis: r.Basis, Source: r.Source, Note: r.Note, OccurredAt: r.OccurredAt, CreatedAt: r.CreatedAt}
		if seesSlugs {
			row.EngagementSlug = r.EngagementSlug
		}
		out = append(out, row)
	}
	return mcpPage[mcpConsumptionRow]{Data: out, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

func (t *mcpTools) inventoryLevels(ctx context.Context, req mcp.Request) (any, error) {
	if err := req.OnlyArgs(inventoryLevelsParams...); err != nil {
		return nil, err
	}
	code, byCode, err := req.String("material_code")
	if err != nil {
		return nil, err
	}
	if byCode && (code == "" || len(code) > materialFieldMaxLen) {
		return nil, mcp.Invalid("material_code must be 1-%d characters", materialFieldMaxLen)
	}
	limit, offset := inventoryLevelsDefaultLimit, 0
	for _, p := range []struct {
		name   string
		dst    *int
		lo, hi int
	}{{"limit", &limit, 1, inventoryLevelsMaxLimit}, {"offset", &offset, 0, inventoryLevelsMaxOffset}} {
		raw, ok, err := req.Integer(p.name)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		n, convErr := atoiBounded(raw, p.lo, p.hi)
		if convErr != nil {
			return nil, mcp.Invalid("%s must be an integer from %d to %d", p.name, p.lo, p.hi)
		}
		*p.dst = n
	}

	filter := repositories.InventoryFilter{Limit: limit, Offset: offset}
	if byCode {
		// GET /v1/inventory?search= narrows by SKU or name; the exact SKU
		// is then picked here ((tenant_id, sku) is unique).
		filter = repositories.InventoryFilter{Search: &code}
	}
	items, total, err := t.inventory.ListItems(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := []mcpInventoryLevel{}
	for _, it := range items {
		if byCode && it.SKU != code {
			continue
		}
		out = append(out, mcpInventoryLevel{ID: it.ID, SKU: it.SKU, Name: it.Name, Category: it.Category, Unit: it.Unit,
			QuantityOnHand: it.QuantityOnHand, QuantityReserved: it.QuantityReserved,
			QuantityAvailable: it.QuantityAvailable, ReorderPoint: it.ReorderPoint, UpdatedAt: it.UpdatedAt})
	}
	if byCode {
		total, limit, offset = len(out), len(out), 0
	}
	return mcpPage[mcpInventoryLevel]{Data: out, Total: total, Limit: limit, Offset: offset}, nil
}

// atoiBounded parses raw as an integer in [minimum, maximum].
func atoiBounded(raw string, minimum, maximum int) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < minimum || n > maximum {
		return 0, errors.New("out of range")
	}
	return n, nil
}

// ---- JSON Schemas ----------------------------------------------------------

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string, extra ...string) map[string]any {
	s := map[string]any{"type": "string", "description": desc}
	for i := 0; i+1 < len(extra); i += 2 {
		s[extra[i]] = extra[i+1]
	}
	return s
}

func integer(desc string, minimum, maximum, def int) map[string]any {
	s := map[string]any{"type": "integer", "description": desc, "minimum": minimum, "default": def}
	if maximum > 0 {
		s["maximum"] = maximum
	}
	return s
}

func uuidRef(desc string) map[string]any { return str(desc, "format", "uuid") }

func jobConsumptionInputSchema() map[string]any {
	return obj(map[string]any{
		"from":            str("occurred_at >= from (RFC 3339)", "format", "date-time"),
		"to":              str("occurred_at < to (RFC 3339)", "format", "date-time"),
		"task_id":         uuidRef("Only this task's rows"),
		"order_id":        uuidRef("Only this order's rows"),
		"engagement_slug": str("Only rows whose order belongs to this engagement", "pattern", types.EngagementSlugPattern),
		"limit":           integer("Page size", 1, consumptionMaxLimit, consumptionDefaultLimit),
		"offset":          integer("Rows to skip", 0, 0, 0),
	}, "from", "to")
}

func inventoryLevelsInputSchema() map[string]any {
	return obj(map[string]any{
		"material_code": map[string]any{"type": "string", "description": "Only the item with this SKU (exact match)",
			"minLength": 1, "maxLength": materialFieldMaxLen},
		"limit":  integer("Page size (ignored with material_code)", 1, inventoryLevelsMaxLimit, inventoryLevelsDefaultLimit),
		"offset": integer("Items to skip (ignored with material_code)", 0, 0, 0),
	})
}

func jobConsumptionRowSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"id", "job_kind", "job_id", "kind", "quantity", "unit", "basis", "source", "occurred_at", "created_at"},
		"properties": map[string]any{
			"id":              uuidRef("Consumption fact id"),
			"job_kind":        map[string]any{"type": "string", "enum": []string{"dispatch", "print_job", "task"}},
			"job_id":          uuidRef("The dispatch, print job or task"),
			"dispatch_id":     uuidRef("Dispatch, when the job is one"),
			"print_job_id":    uuidRef("Print job, when the job is one"),
			"task_id":         uuidRef("Task"),
			"order_id":        uuidRef("Order"),
			"engagement_slug": str("The order's engagement", "pattern", types.EngagementSlugPattern),
			"machine_id":      uuidRef("Machine"),
			"kind":            map[string]any{"type": "string", "enum": []string{"material", "machine_time"}},
			"material_code":   str("Inventory SKU when stocked, else the material class"),
			"material_lot":    str("Material lot"),
			"quantity":        map[string]any{"type": "number", "minimum": 0},
			"unit":            map[string]any{"type": "string", "enum": []string{"g", "mm", "ml", "sheet", "min"}},
			"basis":           map[string]any{"type": "string", "enum": []string{"actual", "estimate"}},
			"source":          map[string]any{"type": "string", "enum": []string{"printer", "operator", "slicer"}},
			"note":            str("How the quantity was derived"),
			"occurred_at":     str("When the job completed", "format", "date-time"),
			"created_at":      str("When the fact was recorded", "format", "date-time"),
		}}
}

func inventoryLevelSchema() map[string]any {
	num := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
	return map[string]any{"type": "object", "required": []string{"id", "sku", "name", "unit", "quantity_on_hand", "quantity_reserved", "quantity_available"},
		"properties": map[string]any{
			"id":                 uuidRef("Inventory item id"),
			"sku":                str("SKU (the material_code of stocked consumption)"),
			"name":               str("Item name"),
			"category":           str("Category"),
			"unit":               str("Unit of the quantities"),
			"quantity_on_hand":   num("On hand"),
			"quantity_reserved":  num("Reserved"),
			"quantity_available": num("On hand minus reserved"),
			"reorder_point":      num("Reorder point"),
			"updated_at":         str("Last change", "format", "date-time"),
		}}
}

func pageSchema(row map[string]any) map[string]any {
	return map[string]any{"type": "object", "required": []string{"data", "total", "limit", "offset"},
		"properties": map[string]any{
			"data":   map[string]any{"type": "array", "items": row},
			"total":  map[string]any{"type": "integer", "description": "Rows matching the filter"},
			"limit":  map[string]any{"type": "integer"},
			"offset": map[string]any{"type": "integer"},
		}}
}
