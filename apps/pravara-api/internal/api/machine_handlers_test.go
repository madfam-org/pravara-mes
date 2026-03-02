package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func TestCreateMachineRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CreateMachineRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateMachineRequest{
				Name: "CNC Machine 1",
				Code: "CNC-01",
				Type: "cnc",
			},
			valid: true,
		},
		{
			name: "valid request with all fields",
			request: CreateMachineRequest{
				Name:        "CNC Machine 1",
				Code:        "CNC-01",
				Type:        "cnc",
				Description: "5-axis CNC milling machine",
				MQTTTopic:   "madfam/hel/production/line-1/cnc-01",
				Location:    "Building A, Floor 2",
			},
			valid: true,
		},
		{
			name: "invalid request missing name",
			request: CreateMachineRequest{
				Code: "CNC-01",
				Type: "cnc",
			},
			valid: false,
		},
		{
			name: "invalid request missing code",
			request: CreateMachineRequest{
				Name: "CNC Machine 1",
				Type: "cnc",
			},
			valid: false,
		},
		{
			name: "invalid request missing type",
			request: CreateMachineRequest{
				Name: "CNC Machine 1",
				Code: "CNC-01",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded CreateMachineRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Check required fields
			hasRequired := decoded.Name != "" && decoded.Code != "" && decoded.Type != ""
			if hasRequired != tt.valid {
				t.Errorf("validation: got %v, want %v", hasRequired, tt.valid)
			}
		})
	}
}

func TestUpdateMachineRequest_Fields(t *testing.T) {
	tests := []struct {
		name    string
		request UpdateMachineRequest
	}{
		{
			name: "update name only",
			request: UpdateMachineRequest{
				Name: "New Machine Name",
			},
		},
		{
			name: "update status",
			request: UpdateMachineRequest{
				Status: "running",
			},
		},
		{
			name: "update location",
			request: UpdateMachineRequest{
				Location: "Building B, Floor 1",
			},
		},
		{
			name: "update mqtt topic",
			request: UpdateMachineRequest{
				MQTTTopic: "madfam/hel/production/line-2/cnc-01",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded UpdateMachineRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Verify field preservation
			if tt.request.Name != "" && decoded.Name != tt.request.Name {
				t.Errorf("Name mismatch: got %q, want %q", decoded.Name, tt.request.Name)
			}
			if tt.request.Status != "" && decoded.Status != tt.request.Status {
				t.Errorf("Status mismatch: got %q, want %q", decoded.Status, tt.request.Status)
			}
		})
	}
}

func TestMachineStatus_Values(t *testing.T) {
	validStatuses := []types.MachineStatus{
		types.MachineStatusOffline,
		types.MachineStatusOnline,
		types.MachineStatusIdle,
		types.MachineStatusRunning,
		types.MachineStatusMaintenance,
		types.MachineStatusError,
	}

	expectedValues := []string{
		"offline",
		"online",
		"idle",
		"running",
		"maintenance",
		"error",
	}

	for i, status := range validStatuses {
		if string(status) != expectedValues[i] {
			t.Errorf("status %d: got %q, want %q", i, string(status), expectedValues[i])
		}
	}
}

func TestMachineTypes_Common(t *testing.T) {
	// Common machine types that should be supported
	commonTypes := []string{
		"cnc",
		"3d_printer",
		"laser_cutter",
		"injection_molder",
		"assembly_station",
		"packaging",
		"conveyor",
		"robot_arm",
	}

	for _, machineType := range commonTypes {
		req := CreateMachineRequest{
			Name: "Test Machine",
			Code: "TEST-01",
			Type: machineType,
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Errorf("failed to marshal machine type %q: %v", machineType, err)
			continue
		}

		var decoded CreateMachineRequest
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("failed to unmarshal machine type %q: %v", machineType, err)
			continue
		}

		if decoded.Type != machineType {
			t.Errorf("type mismatch: got %q, want %q", decoded.Type, machineType)
		}
	}
}

func TestSendCommandRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request SendCommandRequest
		valid   bool
	}{
		{
			name: "valid start_job command",
			request: SendCommandRequest{
				Command: "start_job",
			},
			valid: true,
		},
		{
			name: "valid pause command",
			request: SendCommandRequest{
				Command: "pause",
			},
			valid: true,
		},
		{
			name: "valid resume command",
			request: SendCommandRequest{
				Command: "resume",
			},
			valid: true,
		},
		{
			name: "valid stop command",
			request: SendCommandRequest{
				Command: "stop",
			},
			valid: true,
		},
		{
			name: "valid home command",
			request: SendCommandRequest{
				Command: "home",
			},
			valid: true,
		},
		{
			name: "valid calibrate command",
			request: SendCommandRequest{
				Command: "calibrate",
			},
			valid: true,
		},
		{
			name: "valid emergency_stop command",
			request: SendCommandRequest{
				Command: "emergency_stop",
			},
			valid: true,
		},
		{
			name: "valid preheat command with parameters",
			request: SendCommandRequest{
				Command: "preheat",
				Parameters: map[string]any{
					"temperature": 200,
					"bed_temp":    60,
				},
			},
			valid: true,
		},
		{
			name: "valid load_file command with parameters",
			request: SendCommandRequest{
				Command: "load_file",
				Parameters: map[string]any{
					"file_path": "/gcode/part001.gcode",
				},
			},
			valid: true,
		},
		{
			name: "missing command",
			request: SendCommandRequest{
				Command: "",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded SendCommandRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Check required field
			hasCommand := decoded.Command != ""
			if hasCommand != tt.valid {
				t.Errorf("validation: got %v, want %v", hasCommand, tt.valid)
			}
		})
	}
}

func TestValidCommands_AllDefined(t *testing.T) {
	// All valid command types that should be supported
	expectedCommands := []string{
		"start_job",
		"pause",
		"resume",
		"stop",
		"home",
		"calibrate",
		"emergency_stop",
		"preheat",
		"cooldown",
		"load_file",
		"unload_file",
		"set_origin",
		"probe",
	}

	for _, cmd := range expectedCommands {
		if _, ok := validCommands[cmd]; !ok {
			t.Errorf("command %q not found in validCommands map", cmd)
		}
	}

	// Check count matches
	if len(validCommands) != len(expectedCommands) {
		t.Errorf("validCommands count: got %d, want %d", len(validCommands), len(expectedCommands))
	}
}

func TestSendCommandRequest_WithOptionalFields(t *testing.T) {
	tests := []struct {
		name    string
		request SendCommandRequest
	}{
		{
			name: "command with task_id",
			request: SendCommandRequest{
				Command: "start_job",
				TaskID:  ptrUUID("f47ac10b-58cc-4372-a567-0e02b2c3d479"),
			},
		},
		{
			name: "command with order_id",
			request: SendCommandRequest{
				Command: "start_job",
				OrderID: ptrUUID("550e8400-e29b-41d4-a716-446655440000"),
			},
		},
		{
			name: "command with both task_id and order_id",
			request: SendCommandRequest{
				Command: "start_job",
				TaskID:  ptrUUID("f47ac10b-58cc-4372-a567-0e02b2c3d479"),
				OrderID: ptrUUID("550e8400-e29b-41d4-a716-446655440000"),
			},
		},
		{
			name: "command with complex parameters",
			request: SendCommandRequest{
				Command: "preheat",
				Parameters: map[string]any{
					"extruder_temp": 215,
					"bed_temp":      60,
					"wait":          true,
					"timeout_secs":  300,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded SendCommandRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.Command != tt.request.Command {
				t.Errorf("Command mismatch: got %q, want %q", decoded.Command, tt.request.Command)
			}

			// Verify optional fields preserved
			if tt.request.TaskID != nil && (decoded.TaskID == nil || *decoded.TaskID != *tt.request.TaskID) {
				t.Error("TaskID not preserved")
			}
			if tt.request.OrderID != nil && (decoded.OrderID == nil || *decoded.OrderID != *tt.request.OrderID) {
				t.Error("OrderID not preserved")
			}
			if tt.request.Parameters != nil && decoded.Parameters == nil {
				t.Error("Parameters not preserved")
			}
		})
	}
}

// ptrUUID is a helper to create UUID pointer from string
func ptrUUID(s string) *uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &u
}
