package api

import (
	"encoding/json"
	"testing"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func TestCreateTaskRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CreateTaskRequest
		valid   bool
	}{
		{
			name: "valid request with title only",
			request: CreateTaskRequest{
				Title: "Test Task",
			},
			valid: true,
		},
		{
			name: "valid request with all fields",
			request: CreateTaskRequest{
				Title:            "Test Task",
				Description:      "Test description",
				Priority:         1,
				EstimatedMinutes: 30,
			},
			valid: true,
		},
		{
			name:    "invalid request missing title",
			request: CreateTaskRequest{},
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test JSON marshaling
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded CreateTaskRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Validate title requirement
			hasTitle := decoded.Title != ""
			if tt.valid && !hasTitle {
				t.Error("expected valid request to have title")
			}
		})
	}
}

func TestMoveTaskRequest_Validation(t *testing.T) {
	tests := []struct {
		name     string
		request  MoveTaskRequest
		validPos bool
	}{
		{
			name: "valid move to backlog",
			request: MoveTaskRequest{
				Status:   "backlog",
				Position: 1,
			},
			validPos: true,
		},
		{
			name: "valid move to in_progress",
			request: MoveTaskRequest{
				Status:   "in_progress",
				Position: 5,
			},
			validPos: true,
		},
		{
			name: "invalid position zero",
			request: MoveTaskRequest{
				Status:   "queued",
				Position: 0,
			},
			validPos: false,
		},
		{
			name: "invalid negative position",
			request: MoveTaskRequest{
				Status:   "queued",
				Position: -1,
			},
			validPos: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded MoveTaskRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			validPosition := decoded.Position >= 1
			if validPosition != tt.validPos {
				t.Errorf("position validity: got %v, want %v", validPosition, tt.validPos)
			}
		})
	}
}

func TestTaskStatus_Values(t *testing.T) {
	validStatuses := []types.TaskStatus{
		types.TaskStatusBacklog,
		types.TaskStatusQueued,
		types.TaskStatusInProgress,
		types.TaskStatusQualityCheck,
		types.TaskStatusCompleted,
		types.TaskStatusBlocked,
	}

	expectedValues := []string{
		"backlog",
		"queued",
		"in_progress",
		"quality_check",
		"completed",
		"blocked",
	}

	for i, status := range validStatuses {
		if string(status) != expectedValues[i] {
			t.Errorf("status %d: got %q, want %q", i, string(status), expectedValues[i])
		}
	}
}

func TestAssignTaskRequest_Fields(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		userID  bool
		machine bool
	}{
		{
			name:    "assign to user only",
			json:    `{"user_id": "550e8400-e29b-41d4-a716-446655440000"}`,
			userID:  true,
			machine: false,
		},
		{
			name:    "assign to machine only",
			json:    `{"machine_id": "550e8400-e29b-41d4-a716-446655440001"}`,
			userID:  false,
			machine: true,
		},
		{
			name:    "assign to both",
			json:    `{"user_id": "550e8400-e29b-41d4-a716-446655440000", "machine_id": "550e8400-e29b-41d4-a716-446655440001"}`,
			userID:  true,
			machine: true,
		},
		{
			name:    "empty assignment",
			json:    `{}`,
			userID:  false,
			machine: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req AssignTaskRequest
			if err := json.Unmarshal([]byte(tt.json), &req); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			hasUser := req.UserID != nil
			hasMachine := req.MachineID != nil

			if hasUser != tt.userID {
				t.Errorf("user_id: got %v, want %v", hasUser, tt.userID)
			}
			if hasMachine != tt.machine {
				t.Errorf("machine_id: got %v, want %v", hasMachine, tt.machine)
			}
		})
	}
}
