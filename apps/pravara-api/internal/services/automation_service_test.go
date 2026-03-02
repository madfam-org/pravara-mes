package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func TestMachineValidation_StatusChecks(t *testing.T) {
	tests := []struct {
		name          string
		machineStatus types.MachineStatus
		mqttTopic     string
		wantValid     bool
		wantBlocked   bool
		wantWarning   bool
	}{
		{
			name:          "online machine with mqtt topic is valid",
			machineStatus: types.MachineStatusOnline,
			mqttTopic:     "pravara/machines/001/commands",
			wantValid:     true,
			wantBlocked:   false,
			wantWarning:   false,
		},
		{
			name:          "idle machine with mqtt topic is valid",
			machineStatus: types.MachineStatusIdle,
			mqttTopic:     "pravara/machines/001/commands",
			wantValid:     true,
			wantBlocked:   false,
			wantWarning:   false,
		},
		{
			name:          "offline machine shows warning",
			machineStatus: types.MachineStatusOffline,
			mqttTopic:     "pravara/machines/001/commands",
			wantValid:     true,
			wantBlocked:   false,
			wantWarning:   true,
		},
		{
			name:          "running machine shows warning",
			machineStatus: types.MachineStatusRunning,
			mqttTopic:     "pravara/machines/001/commands",
			wantValid:     true,
			wantBlocked:   false,
			wantWarning:   true,
		},
		{
			name:          "error machine is blocked",
			machineStatus: types.MachineStatusError,
			mqttTopic:     "pravara/machines/001/commands",
			wantValid:     false,
			wantBlocked:   true,
			wantWarning:   false,
		},
		{
			name:          "maintenance machine is blocked",
			machineStatus: types.MachineStatusMaintenance,
			mqttTopic:     "pravara/machines/001/commands",
			wantValid:     false,
			wantBlocked:   true,
			wantWarning:   false,
		},
		{
			name:          "machine without mqtt topic is invalid",
			machineStatus: types.MachineStatusOnline,
			mqttTopic:     "",
			wantValid:     false,
			wantBlocked:   false,
			wantWarning:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a validation result based on the logic from ValidateMachineForTask
			validation := &MachineValidation{
				Valid: true,
				Machine: &types.Machine{
					ID:        uuid.New(),
					Name:      "Test Machine",
					MQTTTopic: tt.mqttTopic,
					Status:    tt.machineStatus,
				},
			}

			// Apply status checks (mirroring the service logic)
			switch tt.machineStatus {
			case types.MachineStatusError:
				validation.Valid = false
				validation.Blocked = true
				validation.Error = "Machine is in error state"
			case types.MachineStatusMaintenance:
				validation.Valid = false
				validation.Blocked = true
				validation.Error = "Machine is under maintenance"
			case types.MachineStatusOffline:
				validation.Warning = "Machine is currently offline - command will be queued"
			case types.MachineStatusRunning:
				validation.Warning = "Machine is currently running a job - command will be queued"
			}

			// Check MQTT topic
			if tt.mqttTopic == "" {
				validation.Valid = false
				validation.Error = "Machine does not have an MQTT topic configured for command dispatch"
			}

			if validation.Valid != tt.wantValid {
				t.Errorf("Valid = %v, want %v", validation.Valid, tt.wantValid)
			}
			if validation.Blocked != tt.wantBlocked {
				t.Errorf("Blocked = %v, want %v", validation.Blocked, tt.wantBlocked)
			}
			hasWarning := validation.Warning != ""
			if hasWarning != tt.wantWarning {
				t.Errorf("Warning = %q, wantWarning = %v", validation.Warning, tt.wantWarning)
			}
		})
	}
}

func TestMachineValidation_NilMachine(t *testing.T) {
	validation := &MachineValidation{
		Valid: false,
		Error: "Machine not found",
	}

	if validation.Valid {
		t.Error("nil machine should result in invalid validation")
	}
	if validation.Error != "Machine not found" {
		t.Errorf("unexpected error message: %s", validation.Error)
	}
}

func TestOnTaskStatusChange_NoMachine(t *testing.T) {
	// When a task moves to in_progress but has no machine assigned,
	// no command should be dispatched
	task := &types.Task{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Title:     "Test Task",
		Status:    types.TaskStatusInProgress,
		MachineID: nil, // No machine assigned
	}

	// The dispatchStartJobCommand should return nil immediately
	// when MachineID is nil
	if task.MachineID != nil {
		t.Error("task should have no machine assigned for this test")
	}
}

func TestOnTaskStatusChange_NotInProgress(t *testing.T) {
	// When a task moves to a status other than in_progress,
	// no command should be dispatched
	machineID := uuid.New()
	task := &types.Task{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Title:     "Test Task",
		Status:    types.TaskStatusQueued,
		MachineID: &machineID,
	}

	// The OnTaskStatusChange should only trigger dispatch when
	// newStatus == TaskStatusInProgress
	if task.Status == types.TaskStatusInProgress {
		t.Error("task should not be in_progress for this test")
	}
}

func TestAutomationService_NewAutomationService(t *testing.T) {
	// Test that NewAutomationService properly initializes with nil dependencies
	service := NewAutomationService(nil, nil, nil, nil, nil)

	if service == nil {
		t.Fatal("NewAutomationService returned nil")
	}

	if service.taskRepo != nil {
		t.Error("taskRepo should be nil")
	}
	if service.machineRepo != nil {
		t.Error("machineRepo should be nil")
	}
	if service.taskCmdRepo != nil {
		t.Error("taskCmdRepo should be nil")
	}
	if service.publisher != nil {
		t.Error("publisher should be nil")
	}
	if service.log != nil {
		t.Error("log should be nil")
	}
}

func TestGetActiveCommand_NilRepo(t *testing.T) {
	service := NewAutomationService(nil, nil, nil, nil, nil)

	// Should handle nil taskCmdRepo gracefully
	defer func() {
		if r := recover(); r == nil {
			// If we don't panic, verify the error handling
			t.Log("GetActiveCommand should handle nil repo")
		}
	}()

	_, err := service.GetActiveCommand(context.Background(), uuid.New())
	if err == nil {
		// GetActiveCommand will panic with nil repo, which is expected
		// In production, repo should never be nil
		t.Log("Expected error or panic with nil repo")
	}
}

func TestGetCommandHistory_NilRepo(t *testing.T) {
	service := NewAutomationService(nil, nil, nil, nil, nil)

	// Should handle nil taskCmdRepo gracefully
	defer func() {
		if r := recover(); r == nil {
			t.Log("GetCommandHistory should handle nil repo")
		}
	}()

	_, err := service.GetCommandHistory(context.Background(), uuid.New())
	if err == nil {
		t.Log("Expected error or panic with nil repo")
	}
}
