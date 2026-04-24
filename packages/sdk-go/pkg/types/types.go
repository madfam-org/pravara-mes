// Package types provides shared type definitions for PravaraMES services.
package types

import (
	"time"

	"github.com/google/uuid"
)

// OrderStatus represents the lifecycle state of an order.
type OrderStatus string

const (
	OrderStatusReceived     OrderStatus = "received"
	OrderStatusConfirmed    OrderStatus = "confirmed"
	OrderStatusInProduction OrderStatus = "in_production"
	OrderStatusQualityCheck OrderStatus = "quality_check"
	OrderStatusReady        OrderStatus = "ready"
	OrderStatusShipped      OrderStatus = "shipped"
	OrderStatusDelivered    OrderStatus = "delivered"
	OrderStatusCancelled    OrderStatus = "cancelled"
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
	MachineStatusOffline     MachineStatus = "offline"
	MachineStatusOnline      MachineStatus = "online"
	MachineStatusIdle        MachineStatus = "idle"
	MachineStatusRunning     MachineStatus = "running"
	MachineStatusSetup       MachineStatus = "setup"
	MachineStatusMaintenance MachineStatus = "maintenance"
	MachineStatusError       MachineStatus = "error"
)

// Tenant represents a customer organization in the multi-tenant system.
type Tenant struct {
	ID        uuid.UUID      `json:"id"`
	Name      string         `json:"name"`
	Slug      string         `json:"slug"`
	Plan      string         `json:"plan"`
	Settings  map[string]any `json:"settings,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
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
	ID            uuid.UUID      `json:"id"`
	TenantID      uuid.UUID      `json:"tenant_id"`
	ExternalID    string         `json:"external_id,omitempty"`
	CustomerName  string         `json:"customer_name"`
	CustomerEmail string         `json:"customer_email,omitempty"`
	Status        OrderStatus    `json:"status"`
	Priority      int            `json:"priority"`
	DueDate       *time.Time     `json:"due_date,omitempty"`
	TotalAmount   float64        `json:"total_amount,omitempty"`
	Currency      string         `json:"currency"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
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
	Description    string         `json:"description,omitempty"`
	Location       string         `json:"location,omitempty"`
	Status         MachineStatus  `json:"status"`
	Capabilities   []string       `json:"capabilities,omitempty"`
	Specifications map[string]any `json:"specifications,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
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

// QualityCertType represents the type of quality certificate.
type QualityCertType string

const (
	QualityCertTypeCOC         QualityCertType = "coc"
	QualityCertTypeCOA         QualityCertType = "coa"
	QualityCertTypeInspection  QualityCertType = "inspection"
	QualityCertTypeTestReport  QualityCertType = "test_report"
	QualityCertTypeCalibration QualityCertType = "calibration"
)

// QualityCertStatus represents the status of a quality certificate.
type QualityCertStatus string

const (
	QualityCertStatusDraft         QualityCertStatus = "draft"
	QualityCertStatusPendingReview QualityCertStatus = "pending_review"
	QualityCertStatusApproved      QualityCertStatus = "approved"
	QualityCertStatusRejected      QualityCertStatus = "rejected"
	QualityCertStatusExpired       QualityCertStatus = "expired"
)

// InspectionResult represents the result of an inspection.
type InspectionResult string

const (
	InspectionResultPass        InspectionResult = "pass"
	InspectionResultFail        InspectionResult = "fail"
	InspectionResultConditional InspectionResult = "conditional"
	InspectionResultPending     InspectionResult = "pending"
)

// QualityCertificate represents a quality certificate (COC, COA, etc.).
type QualityCertificate struct {
	ID                uuid.UUID         `json:"id"`
	TenantID          uuid.UUID         `json:"tenant_id"`
	CertificateNumber string            `json:"certificate_number"`
	Type              QualityCertType   `json:"type"`
	Status            QualityCertStatus `json:"status"`
	OrderID           *uuid.UUID        `json:"order_id,omitempty"`
	TaskID            *uuid.UUID        `json:"task_id,omitempty"`
	MachineID         *uuid.UUID        `json:"machine_id,omitempty"`
	BatchLotID        *uuid.UUID        `json:"batch_lot_id,omitempty"`
	Title             string            `json:"title"`
	Description       string            `json:"description,omitempty"`
	IssuedDate        *time.Time        `json:"issued_date,omitempty"`
	ExpiryDate        *time.Time        `json:"expiry_date,omitempty"`
	IssuedBy          *uuid.UUID        `json:"issued_by,omitempty"`
	ApprovedBy        *uuid.UUID        `json:"approved_by,omitempty"`
	ApprovedAt        *time.Time        `json:"approved_at,omitempty"`
	DocumentURL       string            `json:"document_url,omitempty"`
	Metadata          map[string]any    `json:"metadata,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// Inspection represents a quality inspection.
type Inspection struct {
	ID               uuid.UUID        `json:"id"`
	TenantID         uuid.UUID        `json:"tenant_id"`
	InspectionNumber string           `json:"inspection_number"`
	OrderID          *uuid.UUID       `json:"order_id,omitempty"`
	TaskID           *uuid.UUID       `json:"task_id,omitempty"`
	MachineID        *uuid.UUID       `json:"machine_id,omitempty"`
	Type             string           `json:"type"`
	ScheduledAt      *time.Time       `json:"scheduled_at,omitempty"`
	CompletedAt      *time.Time       `json:"completed_at,omitempty"`
	InspectorID      *uuid.UUID       `json:"inspector_id,omitempty"`
	Result           InspectionResult `json:"result"`
	Notes            string           `json:"notes,omitempty"`
	Checklist        []any            `json:"checklist,omitempty"`
	CertificateID    *uuid.UUID       `json:"certificate_id,omitempty"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// BatchLot represents a batch lot for traceability.
type BatchLot struct {
	ID                uuid.UUID      `json:"id"`
	TenantID          uuid.UUID      `json:"tenant_id"`
	LotNumber         string         `json:"lot_number"`
	ProductName       string         `json:"product_name"`
	ProductCode       string         `json:"product_code,omitempty"`
	Quantity          float64        `json:"quantity"`
	Unit              string         `json:"unit"`
	ManufacturedDate  *time.Time     `json:"manufactured_date,omitempty"`
	ExpiryDate        *time.Time     `json:"expiry_date,omitempty"`
	ReceivedDate      *time.Time     `json:"received_date,omitempty"`
	SupplierName      string         `json:"supplier_name,omitempty"`
	SupplierLotNumber string         `json:"supplier_lot_number,omitempty"`
	PurchaseOrder     string         `json:"purchase_order,omitempty"`
	Status            string         `json:"status"`
	OrderID           *uuid.UUID     `json:"order_id,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}
