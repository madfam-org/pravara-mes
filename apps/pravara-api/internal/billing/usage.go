// Package billing provides usage tracking and billing integration for PravaraMES.
package billing

import (
	"context"
	"time"
)

// UsageEventType defines billable events.
type UsageEventType string

const (
	// UsageEventAPICall tracks API requests.
	UsageEventAPICall UsageEventType = "api_call"
	// UsageEventTelemetry tracks telemetry data points ingested.
	UsageEventTelemetry UsageEventType = "telemetry_point"
	// UsageEventStorage tracks storage usage in megabytes.
	UsageEventStorage UsageEventType = "storage_mb"
	// UsageEventWebSocket tracks WebSocket connection time.
	UsageEventWebSocket UsageEventType = "websocket_connection"
	// UsageEventMachine tracks active machine registrations.
	UsageEventMachine UsageEventType = "machine_active"
	// UsageEventOrder tracks order creation.
	UsageEventOrder UsageEventType = "order_created"
	// UsageEventCertificate tracks quality certificate issuance.
	UsageEventCertificate UsageEventType = "certificate_issued"
)

// UsageEvent represents a billable event.
type UsageEvent struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	EventType UsageEventType    `json:"event_type"`
	Quantity  int64             `json:"quantity"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// TenantUsageSummary provides aggregated usage for a tenant over a time period.
type TenantUsageSummary struct {
	TenantID         string    `json:"tenant_id"`
	Period           string    `json:"period"`
	FromDate         time.Time `json:"from_date"`
	ToDate           time.Time `json:"to_date"`
	APICallCount     int64     `json:"api_call_count"`
	TelemetryPoints  int64     `json:"telemetry_points"`
	StorageMB        int64     `json:"storage_mb"`
	WebSocketMinutes int64     `json:"websocket_minutes"`
	ActiveMachines   int64     `json:"active_machines"`
	OrdersCreated    int64     `json:"orders_created"`
	Certificates     int64     `json:"certificates"`
}

// DailyUsageSummary provides daily breakdown of usage.
type DailyUsageSummary struct {
	Date             string `json:"date"`
	APICallCount     int64  `json:"api_call_count"`
	TelemetryPoints  int64  `json:"telemetry_points"`
	StorageMB        int64  `json:"storage_mb"`
	WebSocketMinutes int64  `json:"websocket_minutes"`
	ActiveMachines   int64  `json:"active_machines"`
	OrdersCreated    int64  `json:"orders_created"`
	Certificates     int64  `json:"certificates"`
}

// UsageRecorder interface for recording usage events.
type UsageRecorder interface {
	// RecordEvent records a single usage event.
	RecordEvent(ctx context.Context, event UsageEvent) error

	// RecordBatch records multiple usage events atomically.
	RecordBatch(ctx context.Context, events []UsageEvent) error

	// GetTenantUsage retrieves aggregated usage for a tenant within a time range.
	GetTenantUsage(ctx context.Context, tenantID string, from, to time.Time) (*TenantUsageSummary, error)

	// GetDailyUsage retrieves daily breakdown of usage for a tenant.
	GetDailyUsage(ctx context.Context, tenantID string, from, to time.Time) ([]DailyUsageSummary, error)

	// Close closes the recorder and releases resources.
	Close() error
}
