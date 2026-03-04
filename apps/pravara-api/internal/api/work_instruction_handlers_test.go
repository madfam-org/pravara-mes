package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// =============== CreateWorkInstructionRequest Tests ===============

func TestCreateWorkInstructionRequest_Validation(t *testing.T) {
	productDefID := uuid.New()
	machineType := "CNC_3axis"

	tests := []struct {
		name    string
		request CreateWorkInstructionRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateWorkInstructionRequest{
				Title:    "Setup Procedure for CNC Mill",
				Version:  "1.0",
				Category: "setup",
			},
			valid: true,
		},
		{
			name: "valid request with all optional fields",
			request: CreateWorkInstructionRequest{
				Title:               "Safety Protocol for Laser Cutter",
				Version:             "2.0",
				Category:            "safety",
				Description:         "Standard safety procedures for laser operations",
				ProductDefinitionID: &productDefID,
				MachineType:         &machineType,
				Steps:               json.RawMessage(`[{"step":1,"text":"Power off machine"}]`),
				ToolsRequired:       json.RawMessage(`["wrench","screwdriver"]`),
				PPERequired:         json.RawMessage(`["safety_glasses","gloves"]`),
				Metadata:            map[string]any{"department": "fabrication"},
			},
			valid: true,
		},
		{
			name: "invalid request missing title",
			request: CreateWorkInstructionRequest{
				Version:  "1.0",
				Category: "setup",
			},
			valid: false,
		},
		{
			name: "invalid request missing version",
			request: CreateWorkInstructionRequest{
				Title:    "Setup Procedure",
				Category: "setup",
			},
			valid: false,
		},
		{
			name: "invalid request missing category",
			request: CreateWorkInstructionRequest{
				Title:   "Setup Procedure",
				Version: "1.0",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/work-instructions", func(c *gin.Context) {
				var req CreateWorkInstructionRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/work-instructions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if tt.valid && w.Code != http.StatusOK {
				t.Errorf("expected valid request to return 200, got %d: %s", w.Code, w.Body.String())
			}
			if !tt.valid && w.Code != http.StatusBadRequest {
				t.Errorf("expected invalid request to return 400, got %d", w.Code)
			}
		})
	}
}

// =============== AttachWorkInstructionRequest Tests ===============

func TestAttachWorkInstructionRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request AttachWorkInstructionRequest
		valid   bool
	}{
		{
			name: "valid request with work_instruction_id",
			request: AttachWorkInstructionRequest{
				WorkInstructionID: uuid.New(),
			},
			valid: true,
		},
		{
			name:    "invalid request missing work_instruction_id",
			request: AttachWorkInstructionRequest{},
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/tasks/work-instructions", func(c *gin.Context) {
				var req AttachWorkInstructionRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/tasks/work-instructions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if tt.valid && w.Code != http.StatusOK {
				t.Errorf("expected valid request to return 200, got %d: %s", w.Code, w.Body.String())
			}
			if !tt.valid && w.Code != http.StatusBadRequest {
				t.Errorf("expected invalid request to return 400, got %d", w.Code)
			}
		})
	}
}

// =============== AcknowledgeStepRequest Tests ===============

func TestAcknowledgeStepRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request AcknowledgeStepRequest
		valid   bool
	}{
		{
			name: "valid request with step_number",
			request: AcknowledgeStepRequest{
				StepNumber: 1,
			},
			valid: true,
		},
		{
			name:    "invalid request missing step_number (zero value)",
			request: AcknowledgeStepRequest{},
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/acknowledge", func(c *gin.Context) {
				var req AcknowledgeStepRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/acknowledge", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if tt.valid && w.Code != http.StatusOK {
				t.Errorf("expected valid request to return 200, got %d: %s", w.Code, w.Body.String())
			}
			if !tt.valid && w.Code != http.StatusBadRequest {
				t.Errorf("expected invalid request to return 400, got %d", w.Code)
			}
		})
	}
}

// =============== UpdateWorkInstructionRequest Tests ===============

func TestUpdateWorkInstructionRequest_Fields(t *testing.T) {
	productDefID := uuid.New()
	machineType := "lathe"
	isActive := false

	tests := []struct {
		name    string
		request UpdateWorkInstructionRequest
	}{
		{
			name: "update title only",
			request: UpdateWorkInstructionRequest{
				Title: "Updated Procedure Title",
			},
		},
		{
			name: "update version and category",
			request: UpdateWorkInstructionRequest{
				Version:  "3.0",
				Category: "maintenance",
			},
		},
		{
			name: "update product definition and machine type",
			request: UpdateWorkInstructionRequest{
				ProductDefinitionID: &productDefID,
				MachineType:         &machineType,
			},
		},
		{
			name: "update is_active",
			request: UpdateWorkInstructionRequest{
				IsActive: &isActive,
			},
		},
		{
			name: "update multiple fields",
			request: UpdateWorkInstructionRequest{
				Title:       "Revised Safety Protocol",
				Description: "Updated safety procedures",
				Steps:       json.RawMessage(`[{"step":1,"text":"Updated step"}]`),
				Metadata:    map[string]any{"revision": 4},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			var decoded UpdateWorkInstructionRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if decoded.Title != tt.request.Title {
				t.Errorf("Title mismatch: got %q, want %q", decoded.Title, tt.request.Title)
			}
			if decoded.Version != tt.request.Version {
				t.Errorf("Version mismatch: got %q, want %q", decoded.Version, tt.request.Version)
			}
			if decoded.Category != tt.request.Category {
				t.Errorf("Category mismatch: got %q, want %q", decoded.Category, tt.request.Category)
			}
		})
	}
}
