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

// =============== ComputeLimitsRequest Tests ===============

func TestComputeLimitsRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request ComputeLimitsRequest
		valid   bool
	}{
		{
			name: "valid request with machine_id and metric_type",
			request: ComputeLimitsRequest{
				MachineID:  uuid.New(),
				MetricType: "temperature",
			},
			valid: true,
		},
		{
			name: "valid request with optional sample_days",
			request: ComputeLimitsRequest{
				MachineID:  uuid.New(),
				MetricType: "vibration",
				SampleDays: 60,
			},
			valid: true,
		},
		{
			name: "invalid request missing machine_id",
			request: ComputeLimitsRequest{
				MetricType: "temperature",
			},
			valid: false,
		},
		{
			name: "invalid request missing metric_type",
			request: ComputeLimitsRequest{
				MachineID: uuid.New(),
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/spc/limits/compute", func(c *gin.Context) {
				var req ComputeLimitsRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/spc/limits/compute", bytes.NewBuffer(body))
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

// =============== AcknowledgeViolationRequest Tests ===============

func TestAcknowledgeViolationRequest_Validation(t *testing.T) {
	notes := "Investigated and resolved root cause"

	tests := []struct {
		name    string
		request AcknowledgeViolationRequest
		valid   bool
	}{
		{
			name: "valid request with notes",
			request: AcknowledgeViolationRequest{
				Notes: &notes,
			},
			valid: true,
		},
		{
			name:    "valid request without notes (all optional)",
			request: AcknowledgeViolationRequest{},
			valid:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/spc/violations/acknowledge", func(c *gin.Context) {
				var req AcknowledgeViolationRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/spc/violations/acknowledge", bytes.NewBuffer(body))
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
