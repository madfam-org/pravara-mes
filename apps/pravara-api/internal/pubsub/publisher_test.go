package pubsub

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEvent_Structure(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()
	machineID := uuid.New()

	event := &Event{
		ID:        uuid.New().String(),
		Type:      EventMachineStatusChanged,
		TenantID:  tenantID,
		Timestamp: now,
		Data: MachineStatusData{
			MachineID:   machineID,
			MachineName: "CNC-01",
			OldStatus:   "offline",
			NewStatus:   "online",
			UpdatedAt:   now,
		},
	}

	if event.Type != EventMachineStatusChanged {
		t.Errorf("Type: got %q, want %q", event.Type, EventMachineStatusChanged)
	}
	if event.TenantID != tenantID {
		t.Errorf("TenantID: got %v, want %v", event.TenantID, tenantID)
	}
}

func TestNewEvent(t *testing.T) {
	tenantID := uuid.New()
	data := MachineStatusData{
		MachineID:   uuid.New(),
		MachineName: "CNC-01",
		NewStatus:   "online",
		UpdatedAt:   time.Now().UTC(),
	}

	event := NewEvent(EventMachineStatusChanged, tenantID, data)

	if event.Type != EventMachineStatusChanged {
		t.Errorf("Type: got %q, want %q", event.Type, EventMachineStatusChanged)
	}
	if event.TenantID != tenantID {
		t.Errorf("TenantID: got %v, want %v", event.TenantID, tenantID)
	}
	if event.ID == "" {
		t.Error("ID should not be empty")
	}
	if event.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestEventTypes(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventMachineStatusChanged, "machine.status_changed"},
		{EventMachineHeartbeat, "machine.heartbeat"},
		{EventMachineTelemetryBatch, "machine.telemetry_batch"},
		{EventMachineCreated, "machine.created"},
		{EventMachineUpdated, "machine.updated"},
		{EventMachineDeleted, "machine.deleted"},
		{EventTaskCreated, "task.created"},
		{EventTaskUpdated, "task.updated"},
		{EventTaskMoved, "task.moved"},
		{EventTaskAssigned, "task.assigned"},
		{EventTaskDeleted, "task.deleted"},
		{EventTaskCompleted, "task.completed"},
		{EventOrderCreated, "order.created"},
		{EventOrderUpdated, "order.updated"},
		{EventOrderDeleted, "order.deleted"},
		{EventOrderStatus, "order.status_changed"},
		{EventNotificationAlert, "notification.alert"},
		{EventNotificationWarning, "notification.warning"},
		{EventNotificationInfo, "notification.info"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("EventType: got %q, want %q", tt.eventType, tt.expected)
			}
		})
	}
}

func TestChannelNamespaces(t *testing.T) {
	tests := []struct {
		namespace ChannelNamespace
		expected  string
	}{
		{NamespaceMachines, "machines"},
		{NamespaceTasks, "tasks"},
		{NamespaceOrders, "orders"},
		{NamespaceTelemetry, "telemetry"},
		{NamespaceNotifications, "notifications"},
	}

	for _, tt := range tests {
		t.Run(string(tt.namespace), func(t *testing.T) {
			if string(tt.namespace) != tt.expected {
				t.Errorf("ChannelNamespace: got %q, want %q", tt.namespace, tt.expected)
			}
		})
	}
}

func TestMachineStatusData(t *testing.T) {
	now := time.Now().UTC()
	machineID := uuid.New()

	data := MachineStatusData{
		MachineID:   machineID,
		MachineName: "CNC-01",
		OldStatus:   "idle",
		NewStatus:   "running",
		UpdatedAt:   now,
	}

	if data.MachineName != "CNC-01" {
		t.Errorf("MachineName: got %q, want %q", data.MachineName, "CNC-01")
	}
	if data.OldStatus != "idle" {
		t.Errorf("OldStatus: got %q, want %q", data.OldStatus, "idle")
	}
	if data.NewStatus != "running" {
		t.Errorf("NewStatus: got %q, want %q", data.NewStatus, "running")
	}
	if data.MachineID != machineID {
		t.Errorf("MachineID: got %v, want %v", data.MachineID, machineID)
	}
}

func TestTaskMoveData(t *testing.T) {
	now := time.Now().UTC()
	taskID := uuid.New()
	movedBy := uuid.New()

	data := TaskMoveData{
		TaskID:      taskID,
		TaskTitle:   "Assemble Part A",
		OldStatus:   "queued",
		NewStatus:   "in_progress",
		OldPosition: 0,
		NewPosition: 0,
		MovedBy:     movedBy,
		MovedAt:     now,
	}

	if data.TaskTitle != "Assemble Part A" {
		t.Errorf("TaskTitle: got %q, want %q", data.TaskTitle, "Assemble Part A")
	}
	if data.OldStatus != "queued" {
		t.Errorf("OldStatus: got %q, want %q", data.OldStatus, "queued")
	}
	if data.NewStatus != "in_progress" {
		t.Errorf("NewStatus: got %q, want %q", data.NewStatus, "in_progress")
	}
	if data.TaskID != taskID {
		t.Errorf("TaskID: got %v, want %v", data.TaskID, taskID)
	}
}

func TestTaskAssignData(t *testing.T) {
	now := time.Now().UTC()
	taskID := uuid.New()
	newAssignee := uuid.New()
	assignedBy := uuid.New()
	assigneeName := "John Doe"

	data := TaskAssignData{
		TaskID:       taskID,
		TaskTitle:    "Assemble Part A",
		OldAssignee:  nil,
		NewAssignee:  &newAssignee,
		AssigneeName: &assigneeName,
		AssignedBy:   assignedBy,
		AssignedAt:   now,
	}

	if *data.AssigneeName != "John Doe" {
		t.Errorf("AssigneeName: got %q, want %q", *data.AssigneeName, "John Doe")
	}
	if data.OldAssignee != nil {
		t.Errorf("OldAssignee: expected nil, got %v", data.OldAssignee)
	}
	if *data.NewAssignee != newAssignee {
		t.Errorf("NewAssignee: got %v, want %v", *data.NewAssignee, newAssignee)
	}
}

func TestOrderStatusData(t *testing.T) {
	now := time.Now().UTC()
	orderID := uuid.New()

	data := OrderStatusData{
		OrderID:         orderID,
		OrderExternalID: "ORD-2026-001",
		OldStatus:       "confirmed",
		NewStatus:       "in_production",
		CustomerName:    "Acme Corp",
		UpdatedAt:       now,
	}

	if data.OrderExternalID != "ORD-2026-001" {
		t.Errorf("OrderExternalID: got %q, want %q", data.OrderExternalID, "ORD-2026-001")
	}
	if data.CustomerName != "Acme Corp" {
		t.Errorf("CustomerName: got %q, want %q", data.CustomerName, "Acme Corp")
	}
	if data.OrderID != orderID {
		t.Errorf("OrderID: got %v, want %v", data.OrderID, orderID)
	}
}

func TestNotificationData(t *testing.T) {
	sourceID := uuid.New()

	data := NotificationData{
		Title:    "Machine Alert",
		Message:  "CNC-01 temperature exceeding threshold",
		Severity: "warning",
		Source:   "machine",
		SourceID: &sourceID,
	}

	if data.Severity != "warning" {
		t.Errorf("Severity: got %q, want %q", data.Severity, "warning")
	}
	if data.Source != "machine" {
		t.Errorf("Source: got %q, want %q", data.Source, "machine")
	}
	if *data.SourceID != sourceID {
		t.Errorf("SourceID: got %v, want %v", *data.SourceID, sourceID)
	}
}

func TestEntityCreatedData(t *testing.T) {
	now := time.Now().UTC()
	entityID := uuid.New()
	createdBy := uuid.New()

	data := EntityCreatedData{
		EntityID:   entityID,
		EntityType: "machine",
		Name:       "CNC-01",
		CreatedBy:  createdBy,
		CreatedAt:  now,
	}

	if data.EntityType != "machine" {
		t.Errorf("EntityType: got %q, want %q", data.EntityType, "machine")
	}
	if data.Name != "CNC-01" {
		t.Errorf("Name: got %q, want %q", data.Name, "CNC-01")
	}
	if data.EntityID != entityID {
		t.Errorf("EntityID: got %v, want %v", data.EntityID, entityID)
	}
}

func TestEntityUpdatedData(t *testing.T) {
	now := time.Now().UTC()
	entityID := uuid.New()
	updatedBy := uuid.New()

	data := EntityUpdatedData{
		EntityID:      entityID,
		EntityType:    "task",
		Name:          "Assemble Part A",
		ChangedFields: []string{"status", "assigned_user_id"},
		UpdatedBy:     updatedBy,
		UpdatedAt:     now,
	}

	if len(data.ChangedFields) != 2 {
		t.Errorf("ChangedFields length: got %d, want %d", len(data.ChangedFields), 2)
	}
	if data.ChangedFields[0] != "status" {
		t.Errorf("ChangedFields[0]: got %q, want %q", data.ChangedFields[0], "status")
	}
}

func TestEntityDeletedData(t *testing.T) {
	now := time.Now().UTC()
	entityID := uuid.New()
	deletedBy := uuid.New()

	data := EntityDeletedData{
		EntityID:   entityID,
		EntityType: "order",
		Name:       "ORD-2026-001",
		DeletedBy:  deletedBy,
		DeletedAt:  now,
	}

	if data.EntityType != "order" {
		t.Errorf("EntityType: got %q, want %q", data.EntityType, "order")
	}
	if data.Name != "ORD-2026-001" {
		t.Errorf("Name: got %q, want %q", data.Name, "ORD-2026-001")
	}
}

func TestPublisherConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		config PublisherConfig
		valid  bool
	}{
		{
			name:   "valid redis URL",
			config: PublisherConfig{RedisURL: "redis://localhost:6379"},
			valid:  true,
		},
		{
			name:   "valid redis URL with password",
			config: PublisherConfig{RedisURL: "redis://:password@localhost:6379/0"},
			valid:  true,
		},
		{
			name:   "empty redis URL",
			config: PublisherConfig{RedisURL: ""},
			valid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.config.RedisURL != ""
			if valid != tt.valid {
				t.Errorf("config validity: got %v, want %v", valid, tt.valid)
			}
		})
	}
}

func TestChannelFormat(t *testing.T) {
	tests := []struct {
		namespace ChannelNamespace
		tenantID  uuid.UUID
		expected  string
	}{
		{
			namespace: NamespaceMachines,
			tenantID:  uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
			expected:  "machines:123e4567-e89b-12d3-a456-426614174000",
		},
		{
			namespace: NamespaceTasks,
			tenantID:  uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
			expected:  "tasks:550e8400-e29b-41d4-a716-446655440000",
		},
		{
			namespace: NamespaceTelemetry,
			tenantID:  uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"),
			expected:  "telemetry:6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.namespace), func(t *testing.T) {
			channel := string(tt.namespace) + ":" + tt.tenantID.String()
			if channel != tt.expected {
				t.Errorf("channel: got %q, want %q", channel, tt.expected)
			}
		})
	}
}

func TestTelemetryBatchData(t *testing.T) {
	machineID := uuid.New()
	now := time.Now().UTC()

	data := TelemetryBatchData{
		MachineID:  machineID,
		ReceivedAt: now,
		Metrics: []TelemetryMetric{
			{Type: "temperature", Value: 45.2, Unit: "celsius", Timestamp: now},
			{Type: "power", Value: 1500.0, Unit: "watts", Timestamp: now},
		},
	}

	if data.MachineID != machineID {
		t.Errorf("MachineID: got %v, want %v", data.MachineID, machineID)
	}
	if len(data.Metrics) != 2 {
		t.Errorf("Metrics length: got %d, want %d", len(data.Metrics), 2)
	}
	if data.Metrics[0].Type != "temperature" {
		t.Errorf("Metrics[0].Type: got %q, want %q", data.Metrics[0].Type, "temperature")
	}
	if data.Metrics[1].Value != 1500.0 {
		t.Errorf("Metrics[1].Value: got %f, want %f", data.Metrics[1].Value, 1500.0)
	}
}
