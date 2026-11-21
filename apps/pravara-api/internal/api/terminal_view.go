package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
// caller is unchanged.

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
