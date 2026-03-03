package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DhanamWebhookPayload represents the incoming webhook from Dhanam.
type DhanamWebhookPayload struct {
	Event     InvoiceEvent           `json:"event"`
	Timestamp time.Time              `json:"timestamp"`
	Data      DhanamWebhookData      `json:"data"`
	Raw       map[string]interface{} `json:"-"`
}

// DhanamWebhookData holds the invoice data within a webhook payload.
type DhanamWebhookData struct {
	InvoiceID   string            `json:"invoice_id"`
	TenantID    string            `json:"tenant_id"`
	Amount      float64           `json:"amount"`
	Currency    string            `json:"currency"`
	PeriodStart time.Time         `json:"period_start"`
	PeriodEnd   time.Time         `json:"period_end"`
	LineItems   []InvoiceLineItem `json:"line_items"`
	Status      string            `json:"status"`
}

// ValidateSignature verifies the HMAC-SHA256 signature of a webhook payload.
func ValidateSignature(payload []byte, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// ParseWebhookPayload parses and validates a Dhanam webhook payload.
func ParseWebhookPayload(body []byte) (*DhanamWebhookPayload, error) {
	var payload DhanamWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	// Store raw payload for audit
	var raw map[string]interface{}
	json.Unmarshal(body, &raw)
	payload.Raw = raw

	// Validate event type
	switch payload.Event {
	case InvoiceEventCreated, InvoiceEventPaid, InvoiceEventOverdue, InvoiceEventCancelled:
		// Valid
	default:
		return nil, fmt.Errorf("unknown event type: %s", payload.Event)
	}

	return &payload, nil
}

// ToInvoice converts a webhook payload to an Invoice record.
func (p *DhanamWebhookPayload) ToInvoice() (*Invoice, error) {
	tenantID, err := uuid.Parse(p.Data.TenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid tenant_id: %w", err)
	}

	return &Invoice{
		ID:           uuid.New(),
		TenantID:     tenantID,
		DhanamID:     p.Data.InvoiceID,
		Status:       InvoiceStatus(p.Data.Status),
		Amount:       p.Data.Amount,
		Currency:     p.Data.Currency,
		PeriodStart:  p.Data.PeriodStart,
		PeriodEnd:    p.Data.PeriodEnd,
		LineItems:    p.Data.LineItems,
		RawPayload:   p.Raw,
		WebhookEvent: p.Event,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}, nil
}
