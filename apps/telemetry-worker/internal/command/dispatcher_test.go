package command

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMachineCommand_ToMQTTPayload(t *testing.T) {
	taskID := uuid.New()
	orderID := uuid.New()
	issuedAt := time.Now().UTC()

	tests := []struct {
		name     string
		cmd      MachineCommand
		validate func(t *testing.T, payload MQTTCommandPayload)
	}{
		{
			name: "basic command without optional fields",
			cmd: MachineCommand{
				CommandID: uuid.New(),
				MachineID: uuid.New(),
				MQTTTopic: "madfam/hel/production/line-1/cnc-01",
				Command:   "start_job",
				IssuedBy:  uuid.New(),
				IssuedAt:  issuedAt,
			},
			validate: func(t *testing.T, payload MQTTCommandPayload) {
				if payload.Command != "start_job" {
					t.Errorf("expected command 'start_job', got %q", payload.Command)
				}
				if payload.TaskID != nil {
					t.Error("expected TaskID to be nil")
				}
				if payload.OrderID != nil {
					t.Error("expected OrderID to be nil")
				}
			},
		},
		{
			name: "command with task and order IDs",
			cmd: MachineCommand{
				CommandID: uuid.New(),
				MachineID: uuid.New(),
				MQTTTopic: "madfam/hel/production/line-1/cnc-01",
				Command:   "start_job",
				TaskID:    &taskID,
				OrderID:   &orderID,
				IssuedBy:  uuid.New(),
				IssuedAt:  issuedAt,
			},
			validate: func(t *testing.T, payload MQTTCommandPayload) {
				if payload.TaskID == nil {
					t.Fatal("expected TaskID to be set")
				}
				if *payload.TaskID != taskID.String() {
					t.Errorf("expected TaskID %q, got %q", taskID.String(), *payload.TaskID)
				}
				if payload.OrderID == nil {
					t.Fatal("expected OrderID to be set")
				}
				if *payload.OrderID != orderID.String() {
					t.Errorf("expected OrderID %q, got %q", orderID.String(), *payload.OrderID)
				}
			},
		},
		{
			name: "command with parameters",
			cmd: MachineCommand{
				CommandID: uuid.New(),
				MachineID: uuid.New(),
				MQTTTopic: "madfam/hel/production/line-1/3d-printer-01",
				Command:   "preheat",
				Parameters: map[string]interface{}{
					"temperature": 200,
					"bed_temp":    60,
				},
				IssuedBy: uuid.New(),
				IssuedAt: issuedAt,
			},
			validate: func(t *testing.T, payload MQTTCommandPayload) {
				if payload.Command != "preheat" {
					t.Errorf("expected command 'preheat', got %q", payload.Command)
				}
				if payload.Parameters == nil {
					t.Fatal("expected Parameters to be set")
				}
				if temp, ok := payload.Parameters["temperature"]; !ok {
					t.Error("expected temperature parameter")
				} else if temp != 200 {
					t.Errorf("expected temperature 200, got %v (type: %T)", temp, temp)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := tt.cmd.ToMQTTPayload()

			// Verify command ID is preserved
			if payload.CommandID != tt.cmd.CommandID.String() {
				t.Errorf("expected CommandID %q, got %q", tt.cmd.CommandID.String(), payload.CommandID)
			}

			// Verify IssuedBy is preserved
			if payload.IssuedBy != tt.cmd.IssuedBy.String() {
				t.Errorf("expected IssuedBy %q, got %q", tt.cmd.IssuedBy.String(), payload.IssuedBy)
			}

			// Run custom validation
			tt.validate(t, payload)

			// Verify payload can be serialized to JSON
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("failed to marshal payload: %v", err)
			}

			// Verify it can be deserialized
			var decoded MQTTCommandPayload
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal payload: %v", err)
			}
		})
	}
}

func TestCommandChannelPattern(t *testing.T) {
	tenantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	expected := "pravara.commands.550e8400-e29b-41d4-a716-446655440000"

	result := CommandChannelPattern(tenantID)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestCommandChannelWildcard(t *testing.T) {
	expected := "pravara.commands.*"
	result := CommandChannelWildcard()
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGetCommandTopic(t *testing.T) {
	tests := []struct {
		baseTopic string
		expected  string
	}{
		{
			baseTopic: "madfam/hel/production/line-1/cnc-01",
			expected:  "madfam/hel/production/line-1/cnc-01/cmd",
		},
		{
			baseTopic: "tenant/site/area/line/machine",
			expected:  "tenant/site/area/line/machine/cmd",
		},
	}

	for _, tt := range tests {
		result := GetCommandTopic(tt.baseTopic)
		if result != tt.expected {
			t.Errorf("GetCommandTopic(%q) = %q, want %q", tt.baseTopic, result, tt.expected)
		}
	}
}

func TestGetAckTopic(t *testing.T) {
	tests := []struct {
		baseTopic string
		expected  string
	}{
		{
			baseTopic: "madfam/hel/production/line-1/cnc-01",
			expected:  "madfam/hel/production/line-1/cnc-01/ack",
		},
		{
			baseTopic: "tenant/site/area/line/machine",
			expected:  "tenant/site/area/line/machine/ack",
		},
	}

	for _, tt := range tests {
		result := GetAckTopic(tt.baseTopic)
		if result != tt.expected {
			t.Errorf("GetAckTopic(%q) = %q, want %q", tt.baseTopic, result, tt.expected)
		}
	}
}

func TestCommandTypes(t *testing.T) {
	// Verify all command types have expected values
	expectedCommands := map[CommandType]string{
		CommandStartJob:   "start_job",
		CommandPause:      "pause",
		CommandResume:     "resume",
		CommandStop:       "stop",
		CommandHome:       "home",
		CommandCalibrate:  "calibrate",
		CommandEmergency:  "emergency_stop",
		CommandPreheat:    "preheat",
		CommandCooldown:   "cooldown",
		CommandLoadFile:   "load_file",
		CommandUnloadFile: "unload_file",
		CommandSetOrigin:  "set_origin",
		CommandProbe:      "probe",
	}

	for cmdType, expected := range expectedCommands {
		if string(cmdType) != expected {
			t.Errorf("CommandType %v should be %q", cmdType, expected)
		}
	}

	// Verify we have 13 command types
	if len(expectedCommands) != 13 {
		t.Errorf("expected 13 command types, got %d", len(expectedCommands))
	}
}

func TestCommandAck_JSONSerialization(t *testing.T) {
	ack := CommandAck{
		CommandID: "550e8400-e29b-41d4-a716-446655440000",
		Success:   true,
		Message:   "Command executed successfully",
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(ack)
	if err != nil {
		t.Fatalf("failed to marshal CommandAck: %v", err)
	}

	var decoded CommandAck
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal CommandAck: %v", err)
	}

	if decoded.CommandID != ack.CommandID {
		t.Errorf("CommandID mismatch: got %q, want %q", decoded.CommandID, ack.CommandID)
	}
	if decoded.Success != ack.Success {
		t.Errorf("Success mismatch: got %v, want %v", decoded.Success, ack.Success)
	}
	if decoded.Message != ack.Message {
		t.Errorf("Message mismatch: got %q, want %q", decoded.Message, ack.Message)
	}
}

func TestMachineCommand_JSONSerialization(t *testing.T) {
	taskID := uuid.New()
	cmd := MachineCommand{
		CommandID: uuid.New(),
		MachineID: uuid.New(),
		MQTTTopic: "madfam/hel/production/line-1/cnc-01",
		Command:   "start_job",
		Parameters: map[string]interface{}{
			"file_path": "/gcode/part001.gcode",
		},
		TaskID:   &taskID,
		IssuedBy: uuid.New(),
		IssuedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("failed to marshal MachineCommand: %v", err)
	}

	var decoded MachineCommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal MachineCommand: %v", err)
	}

	if decoded.CommandID != cmd.CommandID {
		t.Errorf("CommandID mismatch")
	}
	if decoded.MachineID != cmd.MachineID {
		t.Errorf("MachineID mismatch")
	}
	if decoded.Command != cmd.Command {
		t.Errorf("Command mismatch: got %q, want %q", decoded.Command, cmd.Command)
	}
	if decoded.MQTTTopic != cmd.MQTTTopic {
		t.Errorf("MQTTTopic mismatch: got %q, want %q", decoded.MQTTTopic, cmd.MQTTTopic)
	}
}
