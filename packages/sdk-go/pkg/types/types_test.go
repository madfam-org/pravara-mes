package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOrderStatus_Values(t *testing.T) {
	statuses := []struct {
		status   OrderStatus
		expected string
	}{
		{OrderStatusReceived, "received"},
		{OrderStatusConfirmed, "confirmed"},
		{OrderStatusInProduction, "in_production"},
		{OrderStatusQualityCheck, "quality_check"},
		{OrderStatusReady, "ready"},
		{OrderStatusShipped, "shipped"},
		{OrderStatusDelivered, "delivered"},
		{OrderStatusCancelled, "cancelled"},
	}

	for _, tc := range statuses {
		if string(tc.status) != tc.expected {
			t.Errorf("OrderStatus: got %q, want %q", string(tc.status), tc.expected)
		}
	}
}

func TestTaskStatus_Values(t *testing.T) {
	statuses := []struct {
		status   TaskStatus
		expected string
	}{
		{TaskStatusBacklog, "backlog"},
		{TaskStatusQueued, "queued"},
		{TaskStatusInProgress, "in_progress"},
		{TaskStatusQualityCheck, "quality_check"},
		{TaskStatusCompleted, "completed"},
		{TaskStatusBlocked, "blocked"},
	}

	for _, tc := range statuses {
		if string(tc.status) != tc.expected {
			t.Errorf("TaskStatus: got %q, want %q", string(tc.status), tc.expected)
		}
	}
}

func TestMachineStatus_Values(t *testing.T) {
	statuses := []struct {
		status   MachineStatus
		expected string
	}{
		{MachineStatusOffline, "offline"},
		{MachineStatusOnline, "online"},
		{MachineStatusIdle, "idle"},
		{MachineStatusRunning, "running"},
		{MachineStatusMaintenance, "maintenance"},
		{MachineStatusError, "error"},
	}

	for _, tc := range statuses {
		if string(tc.status) != tc.expected {
			t.Errorf("MachineStatus: got %q, want %q", string(tc.status), tc.expected)
		}
	}
}

func TestOrder_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	dueDate := now.Add(24 * time.Hour)

	order := Order{
		ID:            uuid.New(),
		TenantID:      uuid.New(),
		ExternalID:    "EXT-001",
		CustomerName:  "Test Customer",
		CustomerEmail: "test@example.com",
		Status:        OrderStatusReceived,
		Priority:      1,
		DueDate:       &dueDate,
		TotalAmount:   100.50,
		Currency:      "USD",
		Metadata:      map[string]interface{}{"key": "value"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	data, err := json.Marshal(order)
	if err != nil {
		t.Fatalf("failed to marshal order: %v", err)
	}

	var decoded Order
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal order: %v", err)
	}

	if decoded.CustomerName != order.CustomerName {
		t.Errorf("CustomerName: got %q, want %q", decoded.CustomerName, order.CustomerName)
	}
	if decoded.Status != order.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, order.Status)
	}
	if decoded.TotalAmount != order.TotalAmount {
		t.Errorf("TotalAmount: got %f, want %f", decoded.TotalAmount, order.TotalAmount)
	}
}

func TestTask_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	orderID := uuid.New()
	machineID := uuid.New()
	userID := uuid.New()

	task := Task{
		ID:               uuid.New(),
		TenantID:         uuid.New(),
		OrderID:          &orderID,
		MachineID:        &machineID,
		AssignedUserID:   &userID,
		Title:            "Test Task",
		Description:      "Test Description",
		Status:           TaskStatusInProgress,
		Priority:         1,
		EstimatedMinutes: 30,
		ActualMinutes:    25,
		KanbanPosition:   1,
		StartedAt:        &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("failed to marshal task: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal task: %v", err)
	}

	if decoded.Title != task.Title {
		t.Errorf("Title: got %q, want %q", decoded.Title, task.Title)
	}
	if decoded.Status != task.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, task.Status)
	}
	if decoded.KanbanPosition != task.KanbanPosition {
		t.Errorf("KanbanPosition: got %d, want %d", decoded.KanbanPosition, task.KanbanPosition)
	}
}

func TestMachine_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	heartbeat := now.Add(-5 * time.Minute)

	machine := Machine{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Name:        "CNC Machine 1",
		Code:        "CNC-01",
		Type:        "cnc",
		Description: "5-axis CNC milling machine",
		Status:      MachineStatusRunning,
		MQTTTopic:   "madfam/hel/production/line-1/cnc-01",
		Location:    "Building A",
		Specifications: map[string]interface{}{
			"axes":       5,
			"max_rpm":    15000,
			"work_area":  "500x400x300mm",
		},
		LastHeartbeat: &heartbeat,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	data, err := json.Marshal(machine)
	if err != nil {
		t.Fatalf("failed to marshal machine: %v", err)
	}

	var decoded Machine
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal machine: %v", err)
	}

	if decoded.Name != machine.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, machine.Name)
	}
	if decoded.Code != machine.Code {
		t.Errorf("Code: got %q, want %q", decoded.Code, machine.Code)
	}
	if decoded.Status != machine.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, machine.Status)
	}
}

func TestTelemetry_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	telemetry := Telemetry{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		MachineID:  uuid.New(),
		Timestamp:  now,
		MetricType: "temperature",
		Value:      45.2,
		Unit:       "celsius",
		Metadata:   map[string]interface{}{"sensor_id": "S001"},
		CreatedAt:  now,
	}

	data, err := json.Marshal(telemetry)
	if err != nil {
		t.Fatalf("failed to marshal telemetry: %v", err)
	}

	var decoded Telemetry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal telemetry: %v", err)
	}

	if decoded.MetricType != telemetry.MetricType {
		t.Errorf("MetricType: got %q, want %q", decoded.MetricType, telemetry.MetricType)
	}
	if decoded.Value != telemetry.Value {
		t.Errorf("Value: got %f, want %f", decoded.Value, telemetry.Value)
	}
	if decoded.Unit != telemetry.Unit {
		t.Errorf("Unit: got %q, want %q", decoded.Unit, telemetry.Unit)
	}
}

func TestTenant_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	tenant := Tenant{
		ID:        uuid.New(),
		Name:      "Madfam Manufacturing",
		Slug:      "madfam",
		Plan:      "enterprise",
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(tenant)
	if err != nil {
		t.Fatalf("failed to marshal tenant: %v", err)
	}

	var decoded Tenant
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal tenant: %v", err)
	}

	if decoded.Name != tenant.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, tenant.Name)
	}
	if decoded.Slug != tenant.Slug {
		t.Errorf("Slug: got %q, want %q", decoded.Slug, tenant.Slug)
	}
	if decoded.Plan != tenant.Plan {
		t.Errorf("Plan: got %q, want %q", decoded.Plan, tenant.Plan)
	}
}

func TestUser_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	user := User{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Email:       "user@example.com",
		Name:        "Test User",
		Role:        "operator",
		OIDCSubject: "auth0|123456",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	data, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("failed to marshal user: %v", err)
	}

	var decoded User
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal user: %v", err)
	}

	if decoded.Email != user.Email {
		t.Errorf("Email: got %q, want %q", decoded.Email, user.Email)
	}
	if decoded.Role != user.Role {
		t.Errorf("Role: got %q, want %q", decoded.Role, user.Role)
	}
}
