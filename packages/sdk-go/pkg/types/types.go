// Package types provides shared type definitions for PravaraMES services.
package types

import (
	"time"

	"github.com/google/uuid"
)

// OrderStatus represents the lifecycle state of an order.
type OrderStatus string

const (
	OrderStatusReceived   OrderStatus = "received"
	OrderStatusValidated  OrderStatus = "validated"
	OrderStatusScheduled  OrderStatus = "scheduled"
	OrderStatusInProgress OrderStatus = "in_progress"
	OrderStatusCompleted  OrderStatus = "completed"
	OrderStatusShipped    OrderStatus = "shipped"
	OrderStatusCancelled  OrderStatus = "cancelled"
)

// TaskStatus represents the Kanban board state of a task.
type TaskStatus string

const (
	TaskStatusBacklog      TaskStatus = "backlog"
	TaskStatusQueued       TaskStatus = "queued"
	TaskStatusInProgress   TaskStatus = "in_progress"
	TaskStatusQualityCheck TaskStatus = "quality_check"
	TaskStatusCompleted    TaskStatus = "completed"
	TaskStatusBlocked      TaskStatus = "blocked"
)

// MachineStatus represents the operational state of a machine.
type MachineStatus string

const (
	MachineStatusIdle        MachineStatus = "idle"
	MachineStatusRunning     MachineStatus = "running"
	MachineStatusSetup       MachineStatus = "setup"
	MachineStatusMaintenance MachineStatus = "maintenance"
	MachineStatusOffline     MachineStatus = "offline"
	MachineStatusError       MachineStatus = "error"
)

// Tenant represents a customer organization in the multi-tenant system.
type Tenant struct {
	ID        uuid.UUID         `json:"id"`
	Name      string            `json:"name"`
	Slug      string            `json:"slug"`
	Plan      string            `json:"plan"`
	Settings  map[string]any    `json:"settings,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// User represents a user within a tenant.
type User struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	Role        string    `json:"role"`
	OIDCSubject string    `json:"oidc_subject,omitempty"`
	OIDCIssuer  string    `json:"oidc_issuer,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Order represents a manufacturing order (typically from Cotiza).
type Order struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	ExternalID   string         `json:"external_id,omitempty"`
	CustomerName string         `json:"customer_name"`
	CustomerEmail string        `json:"customer_email,omitempty"`
	Status       OrderStatus    `json:"status"`
	Priority     int            `json:"priority"`
	DueDate      *time.Time     `json:"due_date,omitempty"`
	TotalAmount  float64        `json:"total_amount,omitempty"`
	Currency     string         `json:"currency"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// OrderItem represents a line item within an order.
type OrderItem struct {
	ID             uuid.UUID      `json:"id"`
	OrderID        uuid.UUID      `json:"order_id"`
	ProductName    string         `json:"product_name"`
	ProductSKU     string         `json:"product_sku,omitempty"`
	Quantity       int            `json:"quantity"`
	UnitPrice      float64        `json:"unit_price,omitempty"`
	Specifications map[string]any `json:"specifications,omitempty"`
	CADFileURL     string         `json:"cad_file_url,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// Machine represents a manufacturing machine or work center.
type Machine struct {
	ID             uuid.UUID      `json:"id"`
	TenantID       uuid.UUID      `json:"tenant_id"`
	Name           string         `json:"name"`
	Code           string         `json:"code"`
	Type           string         `json:"type,omitempty"`
	Location       string         `json:"location,omitempty"`
	Status         MachineStatus  `json:"status"`
	Capabilities   []string       `json:"capabilities,omitempty"`
	Specifications map[string]any `json:"specifications,omitempty"`
	MQTTTopic      string         `json:"mqtt_topic,omitempty"`
	LastHeartbeat  *time.Time     `json:"last_heartbeat,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// Task represents a Kanban work item.
type Task struct {
	ID               uuid.UUID      `json:"id"`
	TenantID         uuid.UUID      `json:"tenant_id"`
	OrderID          *uuid.UUID     `json:"order_id,omitempty"`
	OrderItemID      *uuid.UUID     `json:"order_item_id,omitempty"`
	MachineID        *uuid.UUID     `json:"machine_id,omitempty"`
	AssignedUserID   *uuid.UUID     `json:"assigned_user_id,omitempty"`
	Title            string         `json:"title"`
	Description      string         `json:"description,omitempty"`
	Status           TaskStatus     `json:"status"`
	Priority         int            `json:"priority"`
	EstimatedMinutes int            `json:"estimated_minutes,omitempty"`
	ActualMinutes    int            `json:"actual_minutes,omitempty"`
	KanbanPosition   int            `json:"kanban_position"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	CompletedAt      *time.Time     `json:"completed_at,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// Telemetry represents a machine telemetry data point.
type Telemetry struct {
	ID         uuid.UUID      `json:"id"`
	TenantID   uuid.UUID      `json:"tenant_id"`
	MachineID  uuid.UUID      `json:"machine_id"`
	Timestamp  time.Time      `json:"timestamp"`
	MetricType string         `json:"metric_type"`
	Value      float64        `json:"value"`
	Unit       string         `json:"unit,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

// AuditLog represents an audit trail entry.
type AuditLog struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	UserID       *uuid.UUID     `json:"user_id,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   *uuid.UUID     `json:"resource_id,omitempty"`
	OldValues    map[string]any `json:"old_values,omitempty"`
	NewValues    map[string]any `json:"new_values,omitempty"`
	IPAddress    string         `json:"ip_address,omitempty"`
	UserAgent    string         `json:"user_agent,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}
