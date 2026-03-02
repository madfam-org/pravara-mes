package api

import (
	"encoding/json"
	"testing"

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
