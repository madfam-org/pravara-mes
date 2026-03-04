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

func TestComputeOEERequest_Validation(t *testing.T) {
	machineID := uuid.New()

	tests := []struct {
		name    string
		request ComputeOEERequest
		valid   bool
	}{
		{
			name: "valid request with machine_id and date",
			request: ComputeOEERequest{
				MachineID: &machineID,
				Date:      "2025-01-15",
			},
			valid: true,
		},
		{
			name: "valid request without machine_id (compute all)",
			request: ComputeOEERequest{
				Date: "2025-01-15",
			},
			valid: true,
		},
		{
			name: "invalid request missing date",
			request: ComputeOEERequest{
				MachineID: &machineID,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/analytics/oee/compute", func(c *gin.Context) {
				var req ComputeOEERequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/analytics/oee/compute", bytes.NewBuffer(body))
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
