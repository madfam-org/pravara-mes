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

// =============== CreateGenealogyRequest Tests ===============

func TestCreateGenealogyRequest_Validation(t *testing.T) {
	productDefID := uuid.New()
	orderID := uuid.New()
	machineID := uuid.New()
	serialNumber := "SN-001"
	lotNumber := "LOT-2025-001"

	tests := []struct {
		name    string
		request CreateGenealogyRequest
		valid   bool
	}{
		{
			name:    "valid empty request (no required fields)",
			request: CreateGenealogyRequest{},
			valid:   true,
		},
		{
			name: "valid request with product definition and serial number",
			request: CreateGenealogyRequest{
				ProductDefinitionID: &productDefID,
				SerialNumber:        &serialNumber,
				Status:              "draft",
			},
			valid: true,
		},
		{
			name: "valid request with all optional fields",
			request: CreateGenealogyRequest{
				ProductDefinitionID: &productDefID,
				OrderID:             &orderID,
				MachineID:           &machineID,
				SerialNumber:        &serialNumber,
				LotNumber:           &lotNumber,
				Status:              "in_progress",
				Metadata:            map[string]any{"batch": "morning"},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/genealogy", func(c *gin.Context) {
				var req CreateGenealogyRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/genealogy", bytes.NewBuffer(body))
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

// =============== SealGenealogyRequest Tests ===============

func TestSealGenealogyRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request SealGenealogyRequest
		valid   bool
	}{
		{
			name: "valid request with sealed_by",
			request: SealGenealogyRequest{
				SealedBy: uuid.New(),
			},
			valid: true,
		},
		{
			name:    "invalid request missing sealed_by",
			request: SealGenealogyRequest{},
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.POST("/genealogy/seal", func(c *gin.Context) {
				var req SealGenealogyRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/genealogy/seal", bytes.NewBuffer(body))
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

// =============== UpdateGenealogyRequest Tests ===============

func TestUpdateGenealogyRequest_Fields(t *testing.T) {
	productDefID := uuid.New()
	serialNumber := "SN-UPDATED"

	tests := []struct {
		name    string
		request UpdateGenealogyRequest
	}{
		{
			name: "update status only",
			request: UpdateGenealogyRequest{
				Status: "completed",
			},
		},
		{
			name: "update product definition and serial number",
			request: UpdateGenealogyRequest{
				ProductDefinitionID: &productDefID,
				SerialNumber:        &serialNumber,
			},
		},
		{
			name: "update metadata",
			request: UpdateGenealogyRequest{
				Metadata: map[string]any{"verified": true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			var decoded UpdateGenealogyRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if decoded.Status != tt.request.Status {
				t.Errorf("Status mismatch: got %q, want %q", decoded.Status, tt.request.Status)
			}
		})
	}
}

// =============== Genealogy OptionalFields Tests ===============

func TestGenealogy_OptionalFields(t *testing.T) {
	tests := []struct {
		name            string
		json            string
		hasProductDef   bool
		hasOrder        bool
		hasMachine      bool
		hasSerialNumber bool
		hasLotNumber    bool
	}{
		{
			name:            "genealogy with product definition only",
			json:            `{"product_definition_id": "550e8400-e29b-41d4-a716-446655440000"}`,
			hasProductDef:   true,
			hasOrder:        false,
			hasMachine:      false,
			hasSerialNumber: false,
			hasLotNumber:    false,
		},
		{
			name:            "genealogy with all optional IDs",
			json:            `{"product_definition_id": "550e8400-e29b-41d4-a716-446655440000", "order_id": "550e8400-e29b-41d4-a716-446655440001", "machine_id": "550e8400-e29b-41d4-a716-446655440002", "serial_number": "SN-001", "lot_number": "LOT-001"}`,
			hasProductDef:   true,
			hasOrder:        true,
			hasMachine:      true,
			hasSerialNumber: true,
			hasLotNumber:    true,
		},
		{
			name:            "genealogy with no optional fields",
			json:            `{}`,
			hasProductDef:   false,
			hasOrder:        false,
			hasMachine:      false,
			hasSerialNumber: false,
			hasLotNumber:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CreateGenealogyRequest
			if err := json.Unmarshal([]byte(tt.json), &req); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			hasProductDef := req.ProductDefinitionID != nil
			hasOrder := req.OrderID != nil
			hasMachine := req.MachineID != nil
			hasSerialNumber := req.SerialNumber != nil
			hasLotNumber := req.LotNumber != nil

			if hasProductDef != tt.hasProductDef {
				t.Errorf("product_definition_id: got %v, want %v", hasProductDef, tt.hasProductDef)
			}
			if hasOrder != tt.hasOrder {
				t.Errorf("order_id: got %v, want %v", hasOrder, tt.hasOrder)
			}
			if hasMachine != tt.hasMachine {
				t.Errorf("machine_id: got %v, want %v", hasMachine, tt.hasMachine)
			}
			if hasSerialNumber != tt.hasSerialNumber {
				t.Errorf("serial_number: got %v, want %v", hasSerialNumber, tt.hasSerialNumber)
			}
			if hasLotNumber != tt.hasLotNumber {
				t.Errorf("lot_number: got %v, want %v", hasLotNumber, tt.hasLotNumber)
			}
		})
	}
}
