package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// A shop-floor terminal's view of an order (owner ruling 2026-11-20:
// "Narrow the terminal's order view, no customer fields"). A device on the
// shop floor gets what the floor needs to find and make the work, and
// nothing about the customer or the price: no customer name or email, no
// shipping address, no engagement slug, no amount or currency, no metadata;
// on items no unit price and no CAD file URL. The order reads answer these
// types to a caller that holds pravara-mes:terminal and neither
// pravara-mes:read nor pravara-mes:jobs (isTerminalOnly); every other
// caller is unchanged. The same caller's task reads carry only the task
// metadata in terminalTaskMetadataKeys (no CAD file URL), and its event
// history has the customer's name removed from order events (owner ruling
// 2026-11-22: "Yes, remove them").

// TerminalOrder is an order as a terminal reads it.
type TerminalOrder struct {
	ID         uuid.UUID         `json:"id"`
	TenantID   uuid.UUID         `json:"tenant_id"`
	ExternalID string            `json:"external_id,omitempty"`
	Status     types.OrderStatus `json:"status"`
	Priority   int               `json:"priority"`
	DueDate    *time.Time        `json:"due_date,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// TerminalOrderItem is an order item as a terminal reads it.
type TerminalOrderItem struct {
	ID             uuid.UUID      `json:"id"`
	OrderID        uuid.UUID      `json:"order_id"`
	ProductName    string         `json:"product_name"`
	ProductSKU     string         `json:"product_sku,omitempty"`
	Quantity       int            `json:"quantity"`
	Specifications map[string]any `json:"specifications,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// TerminalOrderList is GET /v1/orders for a terminal.
type TerminalOrderList struct {
	Data   []TerminalOrder `json:"data"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// TerminalOrderItemsResponse is GET /v1/orders/:id/items for a terminal.
type TerminalOrderItemsResponse struct {
	OrderID uuid.UUID           `json:"order_id"`
	Items   []TerminalOrderItem `json:"items"`
	Count   int                 `json:"count"`
}

// isTerminalOnly reports whether the request comes from a terminal's
// device token: a Janua machine token holding pravara-mes:terminal and
// neither of the scopes that read full orders.
func isTerminalOnly(c *gin.Context) bool {
	caller, ok := middleware.GetCaller(c)
	if !ok || caller.Kind != middleware.CallerServiceAccount {
		return false
	}
	has := map[string]bool{}
	for _, s := range caller.Scopes {
		has[s] = true
	}
	return has[middleware.ScopeTerminal] && !has[middleware.ScopeRead] && !has[middleware.ScopeJobs]
}

func terminalOrder(o types.Order) TerminalOrder {
	return TerminalOrder{ID: o.ID, TenantID: o.TenantID, ExternalID: o.ExternalID, Status: o.Status,
		Priority: o.Priority, DueDate: o.DueDate, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

func terminalOrders(orders []types.Order) []TerminalOrder {
	out := make([]TerminalOrder, 0, len(orders))
	for _, o := range orders {
		out = append(out, terminalOrder(o))
	}
	return out
}

func terminalOrderItems(items []types.OrderItem) []TerminalOrderItem {
	out := make([]TerminalOrderItem, 0, len(items))
	for _, it := range items {
		out = append(out, TerminalOrderItem{ID: it.ID, OrderID: it.OrderID, ProductName: it.ProductName,
			ProductSKU: it.ProductSKU, Quantity: it.Quantity, Specifications: it.Specifications, CreatedAt: it.CreatedAt})
	}
	return out
}

// terminalTaskMetadataKeys are the task metadata keys a terminal reads: what
// order decomposition writes to describe the work (order_decomposition_service
// .go), without the CAD file URL. Any other key, including what a person
// stored by hand, is left out of a terminal's task reads.
var terminalTaskMetadataKeys = map[string]bool{
	"source":                true,
	"product_name":          true,
	"product_sku":           true,
	"quantity":              true,
	"specifications":        true,
	"assignment_basis":      true,
	"assigned_machine_code": true,
}

// terminalTask is a task as a terminal reads it: the same fields, with its
// metadata narrowed to terminalTaskMetadataKeys.
func terminalTask(t types.Task) types.Task {
	if t.Metadata == nil {
		return t
	}
	kept := make(map[string]any, len(t.Metadata))
	for k, v := range t.Metadata {
		if terminalTaskMetadataKeys[k] {
			kept[k] = v
		}
	}
	if len(kept) == 0 {
		kept = nil
	}
	t.Metadata = kept
	return t
}

func terminalTasks(tasks []types.Task) []types.Task {
	if tasks == nil {
		return nil
	}
	out := make([]types.Task, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, terminalTask(t))
	}
	return out
}

// terminalEventRule is what a terminal keeps of one event type's data.
type terminalEventRule struct {
	// keep lists the data keys kept; nil keeps every key.
	keep map[string]bool
	// narrow lists, for an object-valued data key, the sub-keys kept.
	narrow map[string]map[string]bool
}

func keySet(k ...string) map[string]bool {
	out := make(map[string]bool, len(k))
	for _, v := range k {
		out[v] = true
	}
	return out
}

// orderEventMetadataKeys are the order events' metadata keys a terminal
// keeps (what the order handlers and the quoting webhook write).
var orderEventMetadataKeys = keySet("external_id", "item_count", "source")

// terminalEventRules narrow event payloads for a terminal (owner ruling
// 2026-11-22: the customer's name is removed; everything else stays
// readable). Order events are an allow-list of their data keys: the
// customer's name (EntityCreatedData.Name of an order, OrderStatusData's
// customer_name) and any key not listed here never reach a terminal, and an
// order.* type that is not listed is answered with empty data
// (terminalEvent). machine.command_sent keeps its fields with its
// parameters narrowed to the task metadata a terminal reads
// (terminalTaskMetadataKeys): no file names or ids and no free-form manual
// parameters. Other event types carry no customer data and pass unchanged.
var terminalEventRules = map[string]terminalEventRule{
	"order.created": {
		keep:   keySet("entity_id", "entity_type", "created_by", "created_at", "metadata"),
		narrow: map[string]map[string]bool{"metadata": orderEventMetadataKeys},
	},
	"order.updated": {
		keep:   keySet("entity_id", "entity_type", "changed_fields", "updated_by", "updated_at", "metadata"),
		narrow: map[string]map[string]bool{"metadata": orderEventMetadataKeys},
	},
	"order.deleted": {
		keep: keySet("entity_id", "entity_type", "deleted_by", "deleted_at"),
	},
	"order.status_changed": {
		keep: keySet("order_id", "order_external_id", "old_status", "new_status", "updated_at"),
	},
	"machine.command_sent": {
		narrow: map[string]map[string]bool{"parameters": terminalTaskMetadataKeys},
	},
}

// terminalEventRuleFor returns the rule for an event type: its own, or, for an
// order.* type with none, a rule that keeps no data key.
func terminalEventRuleFor(eventType string) (terminalEventRule, bool) {
	if r, ok := terminalEventRules[eventType]; ok {
		return r, true
	}
	if strings.HasPrefix(eventType, "order.") {
		return terminalEventRule{keep: map[string]bool{}}, true
	}
	return terminalEventRule{}, false
}

// terminalEvent applies terminalEventRules to an event's payload ({id, type,
// tenant_id, timestamp, data}). A payload of a narrowed type that cannot be
// read as that envelope is replaced by an empty object rather than passed on
// unread.
func terminalEvent(e repositories.OutboxEvent) repositories.OutboxEvent {
	rule, ok := terminalEventRuleFor(e.EventType)
	if !ok {
		return e
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &envelope); err != nil {
		e.Payload = json.RawMessage(`{}`)
		return e
	}
	if raw, ok := envelope["data"]; ok && len(raw) > 0 && string(raw) != "null" {
		var data map[string]json.RawMessage
		if err := json.Unmarshal(raw, &data); err != nil {
			e.Payload = json.RawMessage(`{}`)
			return e
		}
		narrowed, ok := applyTerminalEventRule(data, rule)
		if !ok {
			e.Payload = json.RawMessage(`{}`)
			return e
		}
		if envelope["data"], ok = marshalRaw(narrowed); !ok {
			e.Payload = json.RawMessage(`{}`)
			return e
		}
	}
	out, ok := marshalRaw(envelope)
	if !ok {
		out = json.RawMessage(`{}`)
	}
	e.Payload = out
	return e
}

// applyTerminalEventRule keeps rule.keep (all keys when nil) and narrows the
// object-valued keys in rule.narrow; false when such a key is not an object.
func applyTerminalEventRule(data map[string]json.RawMessage, rule terminalEventRule) (map[string]json.RawMessage, bool) {
	out := make(map[string]json.RawMessage, len(data))
	for k, v := range data {
		if rule.keep != nil && !rule.keep[k] {
			continue
		}
		if allowed, ok := rule.narrow[k]; ok && string(v) != "null" {
			var inner map[string]json.RawMessage
			if err := json.Unmarshal(v, &inner); err != nil {
				return nil, false
			}
			for ik := range inner {
				if !allowed[ik] {
					delete(inner, ik)
				}
			}
			b, ok := marshalRaw(inner)
			if !ok {
				return nil, false
			}
			v = b
		}
		out[k] = v
	}
	return out, true
}

func marshalRaw(v any) (json.RawMessage, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return b, true
}

func terminalEvents(events []repositories.OutboxEvent) []repositories.OutboxEvent {
	if events == nil {
		return nil
	}
	out := make([]repositories.OutboxEvent, 0, len(events))
	for _, e := range events {
		out = append(out, terminalEvent(e))
	}
	return out
}
