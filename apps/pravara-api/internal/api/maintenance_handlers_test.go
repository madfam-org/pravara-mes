package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// =============== CreateScheduleRequest Tests ===============

func TestCreateScheduleRequest_Validation(t *testing.T) {
	machineID := uuid.New()
	assignedTo := uuid.New()
	intervalDays := 30
	isActive := true

	tests := []struct {
		name    string
		request CreateScheduleRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateScheduleRequest{
				MachineID:   machineID,
				Name:        "Weekly Lubrication",
				TriggerType: "time_based",
			},
			valid: true,
		},
		{
			name: "valid request with all optional fields",
			request: CreateScheduleRequest{
				MachineID:    machineID,
				Name:         "Monthly Inspection",
				Description:  "Full machine inspection",
				TriggerType:  "time_based",
				Priority:     3,
				IntervalDays: &intervalDays,
				AssignedTo:   &assignedTo,
				IsActive:     &isActive,
				Metadata:     map[string]any{"zone": "A"},
			},
			valid: true,
		},
		{
			name: "invalid request missing machine_id",
			request: CreateScheduleRequest{
				Name:        "Weekly Lubrication",
				TriggerType: "time_based",
			},
			valid: false,
		},
		{
			name: "invalid request missing name",
			request: CreateScheduleRequest{
				MachineID:   machineID,
				TriggerType: "time_based",
			},
			valid: false,
		},
		{
			name: "invalid request missing trigger_type",
			request: CreateScheduleRequest{
				MachineID: machineID,
				Name:      "Weekly Lubrication",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/maintenance/schedules", func(c *gin.Context) {
				var req CreateScheduleRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/maintenance/schedules", bytes.NewBuffer(body))
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

// =============== CreateWorkOrderRequest Tests ===============

func TestCreateWorkOrderRequest_Validation(t *testing.T) {
	machineID := uuid.New()
	scheduleID := uuid.New()
	dueAt := time.Now().Add(48 * time.Hour)

	tests := []struct {
		name    string
		request CreateWorkOrderRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateWorkOrderRequest{
				MachineID:       machineID,
				WorkOrderNumber: "WO-001",
				Title:           "Replace bearings",
			},
			valid: true,
		},
		{
			name: "valid request with all optional fields",
			request: CreateWorkOrderRequest{
				ScheduleID:      &scheduleID,
				MachineID:       machineID,
				WorkOrderNumber: "WO-002",
				Title:           "Full overhaul",
				Description:     "Scheduled overhaul",
				Priority:        2,
				DueAt:           &dueAt,
				Notes:           "Bring spare parts",
				Metadata:        map[string]any{"shift": "morning"},
			},
			valid: true,
		},
		{
			name: "invalid request missing machine_id",
			request: CreateWorkOrderRequest{
				WorkOrderNumber: "WO-003",
				Title:           "Replace bearings",
			},
			valid: false,
		},
		{
			name: "invalid request missing work_order_number",
			request: CreateWorkOrderRequest{
				MachineID: machineID,
				Title:     "Replace bearings",
			},
			valid: false,
		},
		{
			name: "invalid request missing title",
			request: CreateWorkOrderRequest{
				MachineID:       machineID,
				WorkOrderNumber: "WO-004",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/maintenance/work-orders", func(c *gin.Context) {
				var req CreateWorkOrderRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/maintenance/work-orders", bytes.NewBuffer(body))
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

// =============== CompleteWorkOrderRequest Tests ===============

func TestCompleteWorkOrderRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CompleteWorkOrderRequest
		valid   bool
	}{
		{
			name: "valid request with notes",
			request: CompleteWorkOrderRequest{
				Notes: "All tasks completed successfully",
			},
			valid: true,
		},
		{
			name: "valid empty request (no required fields)",
			request: CompleteWorkOrderRequest{},
			valid:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/complete", func(c *gin.Context) {
				var req CompleteWorkOrderRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewBuffer(body))
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
