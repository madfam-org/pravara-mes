package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

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

// The move's body as gin binds it: status required; from_status and
// position optional, and a position, when sent, at least 1 (RFC 0064 §4.1).
func TestMoveTaskRequest_Validation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name  string
		body  string
		valid bool
	}{
		{"status and position, as callers always sent", `{"status":"backlog","position":1}`, true},
		{"position further down", `{"status":"in_progress","position":5}`, true},
		{"no position: end of the column", `{"status":"in_progress"}`, true},
		{"from_status and no position", `{"status":"in_progress","from_status":"queued"}`, true},
		{"from_status and position", `{"status":"completed","from_status":"in_progress","position":2}`, true},
		{"no status", `{"position":1}`, false},
		{"position zero", `{"status":"queued","position":0}`, false},
		{"negative position", `{"status":"queued","position":-1}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			var req MoveTaskRequest
			err := c.ShouldBindJSON(&req)
			if tt.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
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
