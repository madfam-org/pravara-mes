// Package command provides command dispatch functionality for machine control.
package command

import (
	"time"

	"github.com/google/uuid"
)

// MachineCommand represents a command to be dispatched to a physical machine.
type MachineCommand struct {
	CommandID  uuid.UUID              `json:"command_id"`
	MachineID  uuid.UUID              `json:"machine_id"`
	MQTTTopic  string                 `json:"mqtt_topic"`
	Command    string                 `json:"command"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	TaskID     *uuid.UUID             `json:"task_id,omitempty"`
	OrderID    *uuid.UUID             `json:"order_id,omitempty"`
	IssuedBy   uuid.UUID              `json:"issued_by"`
	IssuedAt   time.Time              `json:"issued_at"`
}

// CommandType represents supported machine command types.
type CommandType string

const (
	// Core machine control commands
	CommandStartJob  CommandType = "start_job"
	CommandPause     CommandType = "pause"
	CommandResume    CommandType = "resume"
	CommandStop      CommandType = "stop"
	CommandHome      CommandType = "home"
	CommandCalibrate CommandType = "calibrate"
	CommandEmergency CommandType = "emergency_stop"

	// 3D printer specific commands
	CommandPreheat    CommandType = "preheat"
	CommandCooldown   CommandType = "cooldown"
	CommandLoadFile   CommandType = "load_file"
	CommandUnloadFile CommandType = "unload_file"

	// CNC specific commands
	CommandSetOrigin CommandType = "set_origin"
	CommandProbe     CommandType = "probe"
)

// MQTTCommandPayload is the payload format sent to machines via MQTT.
type MQTTCommandPayload struct {
	CommandID  string                 `json:"command_id"`
	Command    string                 `json:"command"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	TaskID     *string                `json:"task_id,omitempty"`
	OrderID    *string                `json:"order_id,omitempty"`
	IssuedBy   string                 `json:"issued_by"`
	IssuedAt   string                 `json:"issued_at"`
}

// CommandAck represents an acknowledgment from a machine.
type CommandAck struct {
	CommandID    string    `json:"command_id"`
	Success      bool      `json:"success"`
	Message      string    `json:"message,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	JobCompleted bool      `json:"job_completed,omitempty"` // True when job has finished
}

// CommandAckEvent represents a command ack event to publish via Redis.
type CommandAckEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	TenantID  uuid.UUID      `json:"tenant_id"`
	Timestamp time.Time      `json:"timestamp"`
	Data      CommandAckData `json:"data"`
}

// CommandAckData contains the acknowledgment data for an event.
type CommandAckData struct {
	CommandID uuid.UUID `json:"command_id"`
	MachineID uuid.UUID `json:"machine_id"`
	Success   bool      `json:"success"`
	Message   string    `json:"message,omitempty"`
	AckedAt   time.Time `json:"acked_at"`
}

// CommandChannelPattern returns the Redis channel pattern for command dispatch.
// Commands are published to "pravara.commands.{tenant_id}".
func CommandChannelPattern(tenantID uuid.UUID) string {
	return "pravara.commands." + tenantID.String()
}

// CommandChannelWildcard returns the wildcard pattern for subscribing to all command channels.
func CommandChannelWildcard() string {
	return "pravara.commands.*"
}

// ToMQTTPayload converts a MachineCommand to the MQTT payload format.
func (c *MachineCommand) ToMQTTPayload() MQTTCommandPayload {
	payload := MQTTCommandPayload{
		CommandID:  c.CommandID.String(),
		Command:    c.Command,
		Parameters: c.Parameters,
		IssuedBy:   c.IssuedBy.String(),
		IssuedAt:   c.IssuedAt.Format(time.RFC3339),
	}

	if c.TaskID != nil {
		taskIDStr := c.TaskID.String()
		payload.TaskID = &taskIDStr
	}

	if c.OrderID != nil {
		orderIDStr := c.OrderID.String()
		payload.OrderID = &orderIDStr
	}

	return payload
}

// CommandTopicSuffix is the MQTT topic suffix for commands.
const CommandTopicSuffix = "/cmd"

// AckTopicSuffix is the MQTT topic suffix for acknowledgments.
const AckTopicSuffix = "/ack"

// GetCommandTopic returns the full MQTT topic for sending commands to a machine.
func GetCommandTopic(baseTopic string) string {
	return baseTopic + CommandTopicSuffix
}

// GetAckTopic returns the full MQTT topic for receiving acknowledgments from a machine.
func GetAckTopic(baseTopic string) string {
	return baseTopic + AckTopicSuffix
}
