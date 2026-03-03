package billing

import (
	"time"

	"github.com/google/uuid"
)

// InvoiceStatus represents the lifecycle state of an invoice.
type InvoiceStatus string

const (
	InvoiceStatusCreated   InvoiceStatus = "created"
	InvoiceStatusPaid      InvoiceStatus = "paid"
	InvoiceStatusOverdue   InvoiceStatus = "overdue"
	InvoiceStatusCancelled InvoiceStatus = "cancelled"
)

// InvoiceEvent represents the type of webhook event from Dhanam.
type InvoiceEvent string

const (
	InvoiceEventCreated   InvoiceEvent = "invoice.created"
	InvoiceEventPaid      InvoiceEvent = "invoice.paid"
	InvoiceEventOverdue   InvoiceEvent = "invoice.overdue"
	InvoiceEventCancelled InvoiceEvent = "invoice.cancelled"
)

// Invoice represents an invoice record from Dhanam.
type Invoice struct {
	ID           uuid.UUID              `json:"id" db:"id"`
	TenantID     uuid.UUID              `json:"tenant_id" db:"tenant_id"`
	DhanamID     string                 `json:"dhanam_id" db:"dhanam_id"`
	Status       InvoiceStatus          `json:"status" db:"status"`
	Amount       float64                `json:"amount" db:"amount"`
	Currency     string                 `json:"currency" db:"currency"`
	PeriodStart  time.Time              `json:"period_start" db:"period_start"`
	PeriodEnd    time.Time              `json:"period_end" db:"period_end"`
	LineItems    []InvoiceLineItem      `json:"line_items" db:"-"`
	RawPayload   map[string]interface{} `json:"raw_payload" db:"raw_payload"`
	WebhookEvent InvoiceEvent           `json:"webhook_event" db:"webhook_event"`
	CreatedAt    time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at" db:"updated_at"`
}

// InvoiceLineItem represents a line item on an invoice.
type InvoiceLineItem struct {
	Description string  `json:"description"`
	Quantity    int64   `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	Total       float64 `json:"total"`
	UsageType   string  `json:"usage_type"`
}
