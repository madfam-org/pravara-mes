package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// =============== CreateProductRequest Tests ===============

func TestCreateProductRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CreateProductRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateProductRequest{
				SKU:      "PROD-001",
				Name:     "Steel Bracket",
				Version:  "1.0",
				Category: "structural",
			},
			valid: true,
		},
		{
			name: "valid request with all optional fields",
			request: CreateProductRequest{
				SKU:             "PROD-002",
				Name:            "Aluminum Frame",
				Version:         "2.1",
				Category:        "frame",
				Description:     "Lightweight aluminum frame for assembly",
				CADFileURL:      "https://cad.example.com/frame-v2.step",
				ParametricSpecs: map[string]any{"width_mm": 150, "height_mm": 300},
				Metadata:        map[string]any{"line": "B"},
			},
			valid: true,
		},
		{
			name: "invalid request missing sku",
			request: CreateProductRequest{
				Name:     "Steel Bracket",
				Version:  "1.0",
				Category: "structural",
			},
			valid: false,
		},
		{
			name: "invalid request missing name",
			request: CreateProductRequest{
				SKU:      "PROD-003",
				Version:  "1.0",
				Category: "structural",
			},
			valid: false,
		},
		{
			name: "invalid request missing version",
			request: CreateProductRequest{
				SKU:      "PROD-004",
				Name:     "Steel Bracket",
				Category: "structural",
			},
			valid: false,
		},
		{
			name: "invalid request missing category",
			request: CreateProductRequest{
				SKU:     "PROD-005",
				Name:    "Steel Bracket",
				Version: "1.0",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/products", func(c *gin.Context) {
				var req CreateProductRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/products", bytes.NewBuffer(body))
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

// =============== CreateBOMItemRequest Tests ===============

func TestCreateBOMItemRequest_Validation(t *testing.T) {
	estimatedCost := 12.50

	tests := []struct {
		name    string
		request CreateBOMItemRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateBOMItemRequest{
				MaterialName: "Steel Rod 10mm",
				Quantity:     5.0,
				Unit:         "pcs",
			},
			valid: true,
		},
		{
			name: "valid request with all optional fields",
			request: CreateBOMItemRequest{
				MaterialName:  "Aluminum Sheet 2mm",
				MaterialCode:  "AL-SH-2MM",
				Quantity:      2.5,
				Unit:          "m2",
				EstimatedCost: &estimatedCost,
				Currency:      "USD",
				Supplier:      "MetalCo",
				SortOrder:     1,
			},
			valid: true,
		},
		{
			name: "invalid request missing material_name",
			request: CreateBOMItemRequest{
				Quantity: 5.0,
				Unit:     "pcs",
			},
			valid: false,
		},
		{
			name: "invalid request missing quantity",
			request: CreateBOMItemRequest{
				MaterialName: "Steel Rod 10mm",
				Unit:         "pcs",
			},
			valid: false,
		},
		{
			name: "invalid request missing unit",
			request: CreateBOMItemRequest{
				MaterialName: "Steel Rod 10mm",
				Quantity:     5.0,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/bom/items", func(c *gin.Context) {
				var req CreateBOMItemRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/bom/items", bytes.NewBuffer(body))
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

// =============== UpdateProductRequest Tests ===============

func TestUpdateProductRequest_Fields(t *testing.T) {
	isActive := false

	tests := []struct {
		name    string
		request UpdateProductRequest
	}{
		{
			name: "update name only",
			request: UpdateProductRequest{
				Name: "Updated Product Name",
			},
		},
		{
			name: "update version and category",
			request: UpdateProductRequest{
				Version:  "3.0",
				Category: "premium",
			},
		},
		{
			name: "update is_active",
			request: UpdateProductRequest{
				IsActive: &isActive,
			},
		},
		{
			name: "update multiple fields",
			request: UpdateProductRequest{
				Name:        "Updated Name",
				Description: "Updated description",
				CADFileURL:  "https://cad.example.com/updated.step",
				Metadata:    map[string]any{"revision": 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			var decoded UpdateProductRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if decoded.Name != tt.request.Name {
				t.Errorf("Name mismatch: got %q, want %q", decoded.Name, tt.request.Name)
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
