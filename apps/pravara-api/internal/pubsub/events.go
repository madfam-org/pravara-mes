// Package pubsub provides real-time event publishing for PravaraMES.
package pubsub

import (
	"time"

	"github.com/google/uuid"
)

// EventType represents the type of real-time event.
type EventType string

const (
	// Machine events
	EventMachineStatusChanged  EventType = "machine.status_changed"
	EventMachineHeartbeat      EventType = "machine.heartbeat"
	EventMachineTelemetryBatch EventType = "machine.telemetry_batch"
	EventMachineCreated        EventType = "machine.created"
	EventMachineUpdated        EventType = "machine.updated"
	EventMachineDeleted        EventType = "machine.deleted"

	// Task events
	EventTaskCreated   EventType = "task.created"
	EventTaskUpdated   EventType = "task.updated"
	EventTaskMoved     EventType = "task.moved"
	EventTaskAssigned  EventType = "task.assigned"
	EventTaskDeleted   EventType = "task.deleted"
	EventTaskCompleted EventType = "task.completed"

	// Order events
	EventOrderCreated  EventType = "order.created"
	EventOrderUpdated  EventType = "order.updated"
	EventOrderDeleted  EventType = "order.deleted"
	EventOrderStatus   EventType = "order.status_changed"
	EventOrderItemAdd  EventType = "order.item_added"

	// Notification events
	EventNotificationAlert   EventType = "notification.alert"
	EventNotificationWarning EventType = "notification.warning"
	EventNotificationInfo    EventType = "notification.info"
)

// ChannelNamespace defines the Centrifugo channel namespaces.
type ChannelNamespace string

const (
	NamespaceMachines      ChannelNamespace = "machines"
	NamespaceTasks         ChannelNamespace = "tasks"
	NamespaceOrders        ChannelNamespace = "orders"
	NamespaceTelemetry     ChannelNamespace = "telemetry"
	NamespaceNotifications ChannelNamespace = "notifications"
)

// Event represents a real-time event to be published.
type Event struct {
	ID        string      `json:"id"`
	Type      EventType   `json:"type"`
	TenantID  uuid.UUID   `json:"tenant_id"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// NewEvent creates a new event with auto-generated ID and timestamp.
func NewEvent(eventType EventType, tenantID uuid.UUID, data interface{}) *Event {
	return &Event{
		ID:        uuid.New().String(),
		Type:      eventType,
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}
}

// MachineStatusData contains data for machine status change events.
type MachineStatusData struct {
	MachineID   uuid.UUID `json:"machine_id"`
	MachineName string    `json:"machine_name"`
	OldStatus   string    `json:"old_status,omitempty"`
	NewStatus   string    `json:"new_status"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MachineHeartbeatData contains data for machine heartbeat events.
type MachineHeartbeatData struct {
	MachineID      uuid.UUID `json:"machine_id"`
	LastHeartbeat  time.Time `json:"last_heartbeat"`
	IsOnline       bool      `json:"is_online"`
	CurrentJobID   *string   `json:"current_job_id,omitempty"`
	CurrentJobName *string   `json:"current_job_name,omitempty"`
}

// TelemetryBatchData contains data for telemetry batch events.
type TelemetryBatchData struct {
	MachineID  uuid.UUID          `json:"machine_id"`
	Metrics    []TelemetryMetric  `json:"metrics"`
	ReceivedAt time.Time          `json:"received_at"`
}

// TelemetryMetric represents a single telemetry metric.
type TelemetryMetric struct {
	Type      string    `json:"type"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Timestamp time.Time `json:"timestamp"`
}

// TaskMoveData contains data for task move events.
type TaskMoveData struct {
	TaskID      uuid.UUID `json:"task_id"`
	TaskTitle   string    `json:"task_title"`
	OldStatus   string    `json:"old_status"`
	NewStatus   string    `json:"new_status"`
	OldPosition int       `json:"old_position"`
	NewPosition int       `json:"new_position"`
	MovedBy     uuid.UUID `json:"moved_by"`
	MovedAt     time.Time `json:"moved_at"`
}

// TaskAssignData contains data for task assignment events.
type TaskAssignData struct {
	TaskID       uuid.UUID  `json:"task_id"`
	TaskTitle    string     `json:"task_title"`
	OldAssignee  *uuid.UUID `json:"old_assignee,omitempty"`
	NewAssignee  *uuid.UUID `json:"new_assignee,omitempty"`
	AssigneeName *string    `json:"assignee_name,omitempty"`
	AssignedBy   uuid.UUID  `json:"assigned_by"`
	AssignedAt   time.Time  `json:"assigned_at"`
}

// OrderStatusData contains data for order status change events.
type OrderStatusData struct {
	OrderID         uuid.UUID `json:"order_id"`
	OrderExternalID string    `json:"order_external_id,omitempty"`
	OldStatus       string    `json:"old_status,omitempty"`
	NewStatus       string    `json:"new_status"`
	CustomerName    string    `json:"customer_name"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// NotificationData contains data for notification events.
type NotificationData struct {
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Severity    string                 `json:"severity"` // info, warning, error, critical
	Source      string                 `json:"source"`   // machine, order, task, system
	SourceID    *uuid.UUID             `json:"source_id,omitempty"`
	ActionURL   *string                `json:"action_url,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// EntityCreatedData is a generic data structure for entity creation events.
type EntityCreatedData struct {
	EntityID   uuid.UUID              `json:"entity_id"`
	EntityType string                 `json:"entity_type"`
	Name       string                 `json:"name"`
	CreatedBy  uuid.UUID              `json:"created_by"`
	CreatedAt  time.Time              `json:"created_at"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// EntityUpdatedData is a generic data structure for entity update events.
type EntityUpdatedData struct {
	EntityID      uuid.UUID              `json:"entity_id"`
	EntityType    string                 `json:"entity_type"`
	Name          string                 `json:"name"`
	ChangedFields []string               `json:"changed_fields,omitempty"`
	UpdatedBy     uuid.UUID              `json:"updated_by"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// EntityDeletedData is a generic data structure for entity deletion events.
type EntityDeletedData struct {
	EntityID   uuid.UUID `json:"entity_id"`
	EntityType string    `json:"entity_type"`
	Name       string    `json:"name"`
	DeletedBy  uuid.UUID `json:"deleted_by"`
	DeletedAt  time.Time `json:"deleted_at"`
}
