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
	EventMachineCommandSent    EventType = "machine.command_sent"
	EventMachineCommandAck     EventType = "machine.command_ack"
	EventMachineCommandFailed  EventType = "machine.command_failed"

	// Task events
	EventTaskCreated      EventType = "task.created"
	EventTaskUpdated      EventType = "task.updated"
	EventTaskMoved        EventType = "task.moved"
	EventTaskAssigned     EventType = "task.assigned"
	EventTaskDeleted      EventType = "task.deleted"
	EventTaskCompleted    EventType = "task.completed"
	EventTaskJobStarted   EventType = "task.job_started"   // Job dispatched to machine
	EventTaskJobCompleted EventType = "task.job_completed" // Machine completed job
	EventTaskJobFailed    EventType = "task.job_failed"    // Machine job failed
	EventTaskBlocked      EventType = "task.blocked"       // Task blocked due to machine error

	// Order events
	EventOrderCreated EventType = "order.created"
	EventOrderUpdated EventType = "order.updated"
	EventOrderDeleted EventType = "order.deleted"
	EventOrderStatus  EventType = "order.status_changed"
	EventOrderItemAdd EventType = "order.item_added"

	// Notification events
	EventNotificationAlert   EventType = "notification.alert"
	EventNotificationWarning EventType = "notification.warning"
	EventNotificationInfo    EventType = "notification.info"

	// Analytics events
	EventOEEUpdated EventType = "analytics.oee_updated"

	// Maintenance events
	EventMaintenanceDue       EventType = "maintenance.due"
	EventMaintenanceOverdue   EventType = "maintenance.overdue"
	EventMaintenanceStarted   EventType = "maintenance.started"
	EventMaintenanceCompleted EventType = "maintenance.completed"

	// Genealogy events
	EventGenealogyCreated EventType = "genealogy.created"
	EventGenealogySealed  EventType = "genealogy.sealed"

	// Work Instruction events
	EventWorkInstructionAck EventType = "task.work_instruction_ack"

	// SPC events
	EventSPCViolation EventType = "analytics.spc_violation"

	// Inventory events
	EventInventoryLowStock EventType = "inventory.low_stock"
	EventInventoryUpdated  EventType = "inventory.updated"

	// Product import events
	EventProductImported EventType = "product.imported_from_yantra4d"
)

// ChannelNamespace defines the Centrifugo channel namespaces.
type ChannelNamespace string

const (
	NamespaceMachines      ChannelNamespace = "machines"
	NamespaceTasks         ChannelNamespace = "tasks"
	NamespaceOrders        ChannelNamespace = "orders"
	NamespaceTelemetry     ChannelNamespace = "telemetry"
	NamespaceNotifications ChannelNamespace = "notifications"
	NamespaceAnalytics     ChannelNamespace = "analytics"
	NamespaceMaintenance   ChannelNamespace = "maintenance"
	NamespaceInventory     ChannelNamespace = "inventory"
	NamespaceProducts      ChannelNamespace = "products"
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

// MachineCommandType represents supported machine command types.
type MachineCommandType string

const (
	// Core machine control commands
	CommandStartJob  MachineCommandType = "start_job"
	CommandPause     MachineCommandType = "pause"
	CommandResume    MachineCommandType = "resume"
	CommandStop      MachineCommandType = "stop"
	CommandHome      MachineCommandType = "home"
	CommandCalibrate MachineCommandType = "calibrate"
	CommandEmergency MachineCommandType = "emergency_stop"
	// 3D printer specific commands
	CommandPreheat    MachineCommandType = "preheat"
	CommandCooldown   MachineCommandType = "cooldown"
	CommandLoadFile   MachineCommandType = "load_file"
	CommandUnloadFile MachineCommandType = "unload_file"
	// CNC specific commands
	CommandSetOrigin MachineCommandType = "set_origin"
	CommandProbe     MachineCommandType = "probe"
)

// MachineCommandData contains data for machine command events.
type MachineCommandData struct {
	CommandID   uuid.UUID              `json:"command_id"`
	MachineID   uuid.UUID              `json:"machine_id"`
	MachineName string                 `json:"machine_name"`
	MQTTTopic   string                 `json:"mqtt_topic"`
	Command     MachineCommandType     `json:"command"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	TaskID      *uuid.UUID             `json:"task_id,omitempty"`
	OrderID     *uuid.UUID             `json:"order_id,omitempty"`
	IssuedBy    uuid.UUID              `json:"issued_by"`
	IssuedAt    time.Time              `json:"issued_at"`
}

// MachineCommandAckData contains data for command acknowledgement events.
type MachineCommandAckData struct {
	CommandID uuid.UUID `json:"command_id"`
	MachineID uuid.UUID `json:"machine_id"`
	Success   bool      `json:"success"`
	Message   string    `json:"message,omitempty"`
	AckedAt   time.Time `json:"acked_at"`
}

// TelemetryBatchData contains data for telemetry batch events.
type TelemetryBatchData struct {
	MachineID  uuid.UUID         `json:"machine_id"`
	Metrics    []TelemetryMetric `json:"metrics"`
	ReceivedAt time.Time         `json:"received_at"`
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

// TaskJobData contains data for task job lifecycle events.
type TaskJobData struct {
	TaskID        uuid.UUID `json:"task_id"`
	TaskTitle     string    `json:"task_title"`
	CommandID     uuid.UUID `json:"command_id"`
	MachineID     uuid.UUID `json:"machine_id"`
	MachineName   string    `json:"machine_name"`
	CommandType   string    `json:"command_type"`
	Status        string    `json:"status"` // started, completed, failed
	ErrorMessage  string    `json:"error_message,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	ActualMinutes int       `json:"actual_minutes,omitempty"`
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
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Severity  string                 `json:"severity"` // info, warning, error, critical
	Source    string                 `json:"source"`   // machine, order, task, system
	SourceID  *uuid.UUID             `json:"source_id,omitempty"`
	ActionURL *string                `json:"action_url,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
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

// OEEUpdatedData contains data for OEE computation events.
type OEEUpdatedData struct {
	MachineID    uuid.UUID `json:"machine_id"`
	MachineName  string    `json:"machine_name,omitempty"`
	SnapshotDate string    `json:"snapshot_date"`
	OEE          float64   `json:"oee"`
	Availability float64   `json:"availability"`
	Performance  float64   `json:"performance"`
	Quality      float64   `json:"quality"`
}

// MaintenanceEventData contains data for maintenance lifecycle events.
type MaintenanceEventData struct {
	WorkOrderID     uuid.UUID  `json:"work_order_id"`
	WorkOrderNumber string     `json:"work_order_number"`
	MachineID       uuid.UUID  `json:"machine_id"`
	MachineName     string     `json:"machine_name,omitempty"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	AssignedTo      *uuid.UUID `json:"assigned_to,omitempty"`
	Timestamp       time.Time  `json:"timestamp"`
}

// GenealogyEventData contains data for genealogy events.
type GenealogyEventData struct {
	GenealogyID  uuid.UUID  `json:"genealogy_id"`
	SerialNumber string     `json:"serial_number,omitempty"`
	LotNumber    string     `json:"lot_number,omitempty"`
	ProductSKU   string     `json:"product_sku,omitempty"`
	Status       string     `json:"status"`
	TaskID       *uuid.UUID `json:"task_id,omitempty"`
	Timestamp    time.Time  `json:"timestamp"`
}

// SPCViolationData contains data for SPC violation events.
type SPCViolationData struct {
	ViolationID   uuid.UUID `json:"violation_id"`
	MachineID     uuid.UUID `json:"machine_id"`
	MachineName   string    `json:"machine_name,omitempty"`
	MetricType    string    `json:"metric_type"`
	ViolationType string    `json:"violation_type"`
	Value         float64   `json:"value"`
	LimitValue    float64   `json:"limit_value"`
	DetectedAt    time.Time `json:"detected_at"`
}

// InventoryEventData contains data for inventory events.
type InventoryEventData struct {
	ItemID            uuid.UUID `json:"item_id"`
	SKU               string    `json:"sku"`
	Name              string    `json:"name"`
	ItemName          string    `json:"item_name,omitempty"`
	Action            string    `json:"action,omitempty"`
	Quantity          float64   `json:"quantity,omitempty"`
	NewOnHand         float64   `json:"new_on_hand,omitempty"`
	QuantityAvailable float64   `json:"quantity_available"`
	ReorderPoint      float64   `json:"reorder_point,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
}
