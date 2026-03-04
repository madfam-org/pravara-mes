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

// =============== CreateInventoryItemRequest Tests ===============

func TestCreateInventoryItemRequest_Validation(t *testing.T) {
	unitCost := 25.50
	forgeSightID := "fs-12345"

	tests := []struct {
		name    string
		request CreateInventoryItemRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateInventoryItemRequest{
				SKU:  "MAT-001",
				Name: "Steel Rod 10mm",
				Unit: "pcs",
			},
			valid: true,
		},
		{
			name: "valid request with all optional fields",
			request: CreateInventoryItemRequest{
				SKU:             "MAT-002",
				Name:            "Aluminum Sheet 2mm",
				Category:        "raw_material",
				Description:     "2mm thick aluminum sheet",
				Unit:            "m2",
				QuantityOnHand:  100.0,
				ReorderPoint:    20.0,
				ReorderQuantity: 50.0,
				ForgeSightID:    &forgeSightID,
				UnitCost:        &unitCost,
				Currency:        "USD",
				Metadata:        map[string]any{"warehouse": "A1"},
			},
			valid: true,
		},
		{
			name: "invalid request missing sku",
			request: CreateInventoryItemRequest{
				Name: "Steel Rod 10mm",
				Unit: "pcs",
			},
			valid: false,
		},
		{
			name: "invalid request missing name",
			request: CreateInventoryItemRequest{
				SKU:  "MAT-003",
				Unit: "pcs",
			},
			valid: false,
		},
		{
			name: "invalid request missing unit",
			request: CreateInventoryItemRequest{
				SKU:  "MAT-004",
				Name: "Steel Rod 10mm",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/inventory/items", func(c *gin.Context) {
				var req CreateInventoryItemRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/inventory/items", bytes.NewBuffer(body))
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

// =============== AdjustInventoryRequest Tests ===============

func TestAdjustInventoryRequest_Validation(t *testing.T) {
	refType := "work_order"
	refID := uuid.New()
	notes := "Monthly restock"

	tests := []struct {
		name    string
		request AdjustInventoryRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: AdjustInventoryRequest{
				Quantity:        10.0,
				TransactionType: "receipt",
			},
			valid: true,
		},
		{
			name: "valid request with all optional fields",
			request: AdjustInventoryRequest{
				Quantity:        -5.0,
				TransactionType: "consumption",
				ReferenceType:   &refType,
				ReferenceID:     &refID,
				Notes:           &notes,
			},
			valid: true,
		},
		{
			name: "invalid request missing quantity",
			request: AdjustInventoryRequest{
				TransactionType: "receipt",
			},
			valid: false,
		},
		{
			name: "invalid request missing transaction_type",
			request: AdjustInventoryRequest{
				Quantity: 10.0,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/inventory/items/adjust", func(c *gin.Context) {
				var req AdjustInventoryRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/inventory/items/adjust", bytes.NewBuffer(body))
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

// =============== UpdateInventoryItemRequest Tests ===============

func TestUpdateInventoryItemRequest_Fields(t *testing.T) {
	reorderPoint := 15.0
	unitCost := 30.0
	forgeSightID := "fs-updated"

	tests := []struct {
		name    string
		request UpdateInventoryItemRequest
	}{
		{
			name: "update name only",
			request: UpdateInventoryItemRequest{
				Name: "Updated Item Name",
			},
		},
		{
			name: "update sku and category",
			request: UpdateInventoryItemRequest{
				SKU:      "MAT-NEW",
				Category: "finished_goods",
			},
		},
		{
			name: "update reorder point and unit cost",
			request: UpdateInventoryItemRequest{
				ReorderPoint: &reorderPoint,
				UnitCost:     &unitCost,
			},
		},
		{
			name: "update forgesight id",
			request: UpdateInventoryItemRequest{
				ForgeSightID: &forgeSightID,
			},
		},
		{
			name: "update multiple fields",
			request: UpdateInventoryItemRequest{
				Name:        "Updated Name",
				Description: "Updated description",
				Unit:        "kg",
				Currency:    "EUR",
				Metadata:    map[string]any{"location": "B2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			var decoded UpdateInventoryItemRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if decoded.Name != tt.request.Name {
				t.Errorf("Name mismatch: got %q, want %q", decoded.Name, tt.request.Name)
			}
			if decoded.SKU != tt.request.SKU {
				t.Errorf("SKU mismatch: got %q, want %q", decoded.SKU, tt.request.SKU)
			}
			if decoded.Category != tt.request.Category {
				t.Errorf("Category mismatch: got %q, want %q", decoded.Category, tt.request.Category)
			}
		})
	}
}
