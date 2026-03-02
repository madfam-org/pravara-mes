package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestOrderHandler_CreateOrderRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CreateOrderRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateOrderRequest{
				CustomerName: "Test Customer",
			},
			valid: true,
		},
		{
			name: "valid request with all fields",
			request: CreateOrderRequest{
				ExternalID:    "EXT-001",
				CustomerName:  "Test Customer",
				CustomerEmail: "test@example.com",
				Priority:      1,
				TotalAmount:   100.50,
				Currency:      "USD",
			},
			valid: true,
		},
		{
			name: "invalid request missing customer name",
			request: CreateOrderRequest{
				CustomerEmail: "test@example.com",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			log := logrus.New()
			log.SetLevel(logrus.PanicLevel)
			handler := NewOrderHandler(nil, log)

			router := gin.New()
			router.POST("/orders", func(c *gin.Context) {
				var req CreateOrderRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})
			_ = handler // handler used for type reference

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBuffer(body))
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

func TestOrderHandler_UpdateOrderRequest_Fields(t *testing.T) {
	tests := []struct {
		name    string
		request UpdateOrderRequest
	}{
		{
			name: "update customer name only",
			request: UpdateOrderRequest{
				CustomerName: "New Name",
			},
		},
		{
			name: "update status",
			request: UpdateOrderRequest{
				Status: "confirmed",
			},
		},
		{
			name: "update priority",
			request: UpdateOrderRequest{
				Priority: 1,
			},
		},
		{
			name: "update multiple fields",
			request: UpdateOrderRequest{
				CustomerName:  "New Name",
				CustomerEmail: "new@example.com",
				Status:        "in_production",
				Priority:      2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the struct can be marshaled/unmarshaled correctly
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			var decoded UpdateOrderRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if decoded.CustomerName != tt.request.CustomerName {
				t.Errorf("CustomerName mismatch: got %q, want %q", decoded.CustomerName, tt.request.CustomerName)
			}
			if decoded.Status != tt.request.Status {
				t.Errorf("Status mismatch: got %q, want %q", decoded.Status, tt.request.Status)
			}
		})
	}
}

func TestListResponse_Pagination(t *testing.T) {
	tests := []struct {
		name     string
		response ListResponse
	}{
		{
			name: "empty list",
			response: ListResponse{
				Data:   []interface{}{},
				Total:  0,
				Limit:  20,
				Offset: 0,
			},
		},
		{
			name: "first page",
			response: ListResponse{
				Data:   []interface{}{"item1", "item2"},
				Total:  100,
				Limit:  20,
				Offset: 0,
			},
		},
		{
			name: "middle page",
			response: ListResponse{
				Data:   []interface{}{"item1", "item2"},
				Total:  100,
				Limit:  20,
				Offset: 40,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("failed to marshal response: %v", err)
			}

			var decoded ListResponse
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			if decoded.Total != tt.response.Total {
				t.Errorf("Total mismatch: got %d, want %d", decoded.Total, tt.response.Total)
			}
			if decoded.Limit != tt.response.Limit {
				t.Errorf("Limit mismatch: got %d, want %d", decoded.Limit, tt.response.Limit)
			}
			if decoded.Offset != tt.response.Offset {
				t.Errorf("Offset mismatch: got %d, want %d", decoded.Offset, tt.response.Offset)
			}
		})
	}
}
