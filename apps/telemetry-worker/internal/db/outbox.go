package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Outbox event types and namespaces written by the worker. They match the
// event catalogue of pravara-api (internal/pubsub/events.go), so the API's
// webhook dispatcher and event feeds deliver them unchanged.
const (
	EventMachineCommandFailed = "machine.command_failed"
	EventTaskJobCompleted     = "task.job_completed"
	EventTaskJobFailed        = "task.job_failed"
	EventOrderStatusChanged   = "order.status_changed"

	NamespaceMachines = "machines"
	NamespaceTasks    = "tasks"
	NamespaceOrders   = "orders"
)

// outboxEnvelope is the pravara-api Event envelope.
type outboxEnvelope struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	TenantID  uuid.UUID   `json:"tenant_id"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// CommandFailedData is the payload of machine.command_failed.
type CommandFailedData struct {
	CommandID   uuid.UUID  `json:"command_id"`
	MachineID   uuid.UUID  `json:"machine_id"`
	MachineName string     `json:"machine_name,omitempty"`
	CommandType string     `json:"command_type"`
	Status      string     `json:"status"` // failed | timeout
	Reason      string     `json:"reason"`
	TaskID      *uuid.UUID `json:"task_id,omitempty"`
	Attempts    int        `json:"attempts"`
	FailedAt    time.Time  `json:"failed_at"`
}

// TaskJobData is the payload of task.job_* events (pubsub.TaskJobData).
type TaskJobData struct {
	TaskID        uuid.UUID `json:"task_id"`
	TaskTitle     string    `json:"task_title"`
	CommandID     uuid.UUID `json:"command_id"`
	MachineID     uuid.UUID `json:"machine_id"`
	MachineName   string    `json:"machine_name"`
	CommandType   string    `json:"command_type"`
	Status        string    `json:"status"`
	ErrorMessage  string    `json:"error_message,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	ActualMinutes int       `json:"actual_minutes,omitempty"`
}

// OrderStatusData is the payload of order.status_changed (pubsub.OrderStatusData).
type OrderStatusData struct {
	OrderID         uuid.UUID `json:"order_id"`
	OrderExternalID string    `json:"order_external_id,omitempty"`
	OldStatus       string    `json:"old_status,omitempty"`
	NewStatus       string    `json:"new_status"`
	CustomerName    string    `json:"customer_name"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// insertOutboxEvent writes one event to event_outbox inside tx.
func insertOutboxEvent(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, eventType, namespace string, data interface{}) error {
	env := outboxEnvelope{
		ID:        uuid.New().String(),
		Type:      eventType,
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("outbox: marshal %s: %w", eventType, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO event_outbox (id, tenant_id, event_type, channel_namespace, payload)
		 VALUES ($1, $2, $3, $4, $5)`,
		env.ID, tenantID, eventType, namespace, payload,
	); err != nil {
		return fmt.Errorf("outbox: insert %s: %w", eventType, err)
	}
	return nil
}
