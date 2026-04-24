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
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// =============== Quality Certificates Tests ===============

func TestCreateCertificateRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CreateCertificateRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateCertificateRequest{
				CertificateNumber: "CERT-001",
				Type:              types.QualityCertTypeCOC,
				Title:             "Certificate of Conformance",
			},
			valid: true,
		},
		{
			name: "valid request with all fields",
			request: CreateCertificateRequest{
				CertificateNumber: "CERT-002",
				Type:              types.QualityCertTypeCOA,
				Status:            types.QualityCertStatusDraft,
				Title:             "Certificate of Analysis",
				Description:       "Test certificate",
				DocumentURL:       "https://example.com/cert.pdf",
			},
			valid: true,
		},
		{
			name: "invalid request missing certificate number",
			request: CreateCertificateRequest{
				Type:  types.QualityCertTypeCOC,
				Title: "Certificate",
			},
			valid: false,
		},
		{
			name: "invalid request missing type",
			request: CreateCertificateRequest{
				CertificateNumber: "CERT-003",
				Title:             "Certificate",
			},
			valid: false,
		},
		{
			name: "invalid request missing title",
			request: CreateCertificateRequest{
				CertificateNumber: "CERT-004",
				Type:              types.QualityCertTypeCOC,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			log := logrus.New()
			log.SetLevel(logrus.PanicLevel)
			handler := NewQualityHandler(nil, nil, nil, log)

			router := gin.New()
			router.POST("/certificates", func(c *gin.Context) {
				var req CreateCertificateRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})
			_ = handler // handler used for type reference

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/certificates", bytes.NewBuffer(body))
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

func TestUpdateCertificateRequest_Fields(t *testing.T) {
	tests := []struct {
		name    string
		request UpdateCertificateRequest
	}{
		{
			name: "update status only",
			request: UpdateCertificateRequest{
				Status: types.QualityCertStatusApproved,
			},
		},
		{
			name: "update title and description",
			request: UpdateCertificateRequest{
				Title:       "Updated Certificate",
				Description: "Updated description",
			},
		},
		{
			name: "update dates",
			request: UpdateCertificateRequest{
				IssuedDate: &time.Time{},
				ExpiryDate: &time.Time{},
			},
		},
		{
			name: "update multiple fields",
			request: UpdateCertificateRequest{
				Status:      types.QualityCertStatusPendingReview,
				Title:       "New Title",
				Description: "New Description",
				DocumentURL: "https://example.com/new.pdf",
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

			var decoded UpdateCertificateRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if decoded.Status != tt.request.Status {
				t.Errorf("Status mismatch: got %q, want %q", decoded.Status, tt.request.Status)
			}
			if decoded.Title != tt.request.Title {
				t.Errorf("Title mismatch: got %q, want %q", decoded.Title, tt.request.Title)
			}
		})
	}
}

func TestQualityCertType_Values(t *testing.T) {
	validTypes := []types.QualityCertType{
		types.QualityCertTypeCOC,
		types.QualityCertTypeCOA,
		types.QualityCertTypeInspection,
		types.QualityCertTypeTestReport,
		types.QualityCertTypeCalibration,
	}

	expectedValues := []string{
		"coc",
		"coa",
		"inspection",
		"test_report",
		"calibration",
	}

	for i, certType := range validTypes {
		if string(certType) != expectedValues[i] {
			t.Errorf("cert type %d: got %q, want %q", i, string(certType), expectedValues[i])
		}
	}
}

func TestQualityCertStatus_Values(t *testing.T) {
	validStatuses := []types.QualityCertStatus{
		types.QualityCertStatusDraft,
		types.QualityCertStatusPendingReview,
		types.QualityCertStatusApproved,
		types.QualityCertStatusRejected,
		types.QualityCertStatusExpired,
	}

	expectedValues := []string{
		"draft",
		"pending_review",
		"approved",
		"rejected",
		"expired",
	}

	for i, status := range validStatuses {
		if string(status) != expectedValues[i] {
			t.Errorf("status %d: got %q, want %q", i, string(status), expectedValues[i])
		}
	}
}

func TestCertificate_OptionalFields(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		hasOrder   bool
		hasTask    bool
		hasBatch   bool
		hasMachine bool
	}{
		{
			name:       "certificate with order only",
			json:       `{"order_id": "550e8400-e29b-41d4-a716-446655440000"}`,
			hasOrder:   true,
			hasTask:    false,
			hasBatch:   false,
			hasMachine: false,
		},
		{
			name:       "certificate with task only",
			json:       `{"task_id": "550e8400-e29b-41d4-a716-446655440001"}`,
			hasOrder:   false,
			hasTask:    true,
			hasBatch:   false,
			hasMachine: false,
		},
		{
			name:       "certificate with all optional IDs",
			json:       `{"order_id": "550e8400-e29b-41d4-a716-446655440000", "task_id": "550e8400-e29b-41d4-a716-446655440001", "batch_lot_id": "550e8400-e29b-41d4-a716-446655440002", "machine_id": "550e8400-e29b-41d4-a716-446655440003"}`,
			hasOrder:   true,
			hasTask:    true,
			hasBatch:   true,
			hasMachine: true,
		},
		{
			name:       "certificate with no optional IDs",
			json:       `{}`,
			hasOrder:   false,
			hasTask:    false,
			hasBatch:   false,
			hasMachine: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CreateCertificateRequest
			if err := json.Unmarshal([]byte(tt.json), &req); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			hasOrder := req.OrderID != nil
			hasTask := req.TaskID != nil
			hasBatch := req.BatchLotID != nil
			hasMachine := req.MachineID != nil

			if hasOrder != tt.hasOrder {
				t.Errorf("order_id: got %v, want %v", hasOrder, tt.hasOrder)
			}
			if hasTask != tt.hasTask {
				t.Errorf("task_id: got %v, want %v", hasTask, tt.hasTask)
			}
			if hasBatch != tt.hasBatch {
				t.Errorf("batch_lot_id: got %v, want %v", hasBatch, tt.hasBatch)
			}
			if hasMachine != tt.hasMachine {
				t.Errorf("machine_id: got %v, want %v", hasMachine, tt.hasMachine)
			}
		})
	}
}

// =============== Inspections Tests ===============

func TestCreateInspectionRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CreateInspectionRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateInspectionRequest{
				InspectionNumber: "INS-001",
				Type:             "incoming",
			},
			valid: true,
		},
		{
			name: "valid request with all fields",
			request: CreateInspectionRequest{
				InspectionNumber: "INS-002",
				Type:             "final",
				Notes:            "Test inspection",
				Checklist:        []any{"item1", "item2"},
			},
			valid: true,
		},
		{
			name: "invalid request missing inspection number",
			request: CreateInspectionRequest{
				Type: "incoming",
			},
			valid: false,
		},
		{
			name: "invalid request missing type",
			request: CreateInspectionRequest{
				InspectionNumber: "INS-003",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			log := logrus.New()
			log.SetLevel(logrus.PanicLevel)
			handler := NewQualityHandler(nil, nil, nil, log)

			router := gin.New()
			router.POST("/inspections", func(c *gin.Context) {
				var req CreateInspectionRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})
			_ = handler // handler used for type reference

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/inspections", bytes.NewBuffer(body))
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

func TestUpdateInspectionRequest_Fields(t *testing.T) {
	tests := []struct {
		name    string
		request UpdateInspectionRequest
	}{
		{
			name: "update result only",
			request: UpdateInspectionRequest{
				Result: types.InspectionResultPass,
			},
		},
		{
			name: "update notes",
			request: UpdateInspectionRequest{
				Notes: "Updated notes",
			},
		},
		{
			name: "update checklist",
			request: UpdateInspectionRequest{
				Checklist: []any{"check1", "check2", "check3"},
			},
		},
		{
			name: "update multiple fields",
			request: UpdateInspectionRequest{
				Result:    types.InspectionResultFail,
				Notes:     "Failed inspection",
				Checklist: []any{"failed_item"},
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

			var decoded UpdateInspectionRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if decoded.Result != tt.request.Result {
				t.Errorf("Result mismatch: got %q, want %q", decoded.Result, tt.request.Result)
			}
			if decoded.Notes != tt.request.Notes {
				t.Errorf("Notes mismatch: got %q, want %q", decoded.Notes, tt.request.Notes)
			}
		})
	}
}

func TestCompleteInspectionRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CompleteInspectionRequest
		valid   bool
	}{
		{
			name: "valid completion with pass result",
			request: CompleteInspectionRequest{
				Result: types.InspectionResultPass,
				Notes:  "All checks passed",
			},
			valid: true,
		},
		{
			name: "valid completion with fail result",
			request: CompleteInspectionRequest{
				Result:    types.InspectionResultFail,
				Notes:     "Failed quality check",
				Checklist: []any{"failed_item"},
			},
			valid: true,
		},
		{
			name: "valid completion with conditional result",
			request: CompleteInspectionRequest{
				Result: types.InspectionResultConditional,
				Notes:  "Requires follow-up",
			},
			valid: true,
		},
		{
			name:    "invalid completion missing result",
			request: CompleteInspectionRequest{},
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			log := logrus.New()
			log.SetLevel(logrus.PanicLevel)
			handler := NewQualityHandler(nil, nil, nil, log)

			router := gin.New()
			router.POST("/complete", func(c *gin.Context) {
				var req CompleteInspectionRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})
			_ = handler // handler used for type reference

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

func TestInspectionResult_Values(t *testing.T) {
	validResults := []types.InspectionResult{
		types.InspectionResultPass,
		types.InspectionResultFail,
		types.InspectionResultConditional,
		types.InspectionResultPending,
	}

	expectedValues := []string{
		"pass",
		"fail",
		"conditional",
		"pending",
	}

	for i, result := range validResults {
		if string(result) != expectedValues[i] {
			t.Errorf("result %d: got %q, want %q", i, string(result), expectedValues[i])
		}
	}
}

func TestInspection_OptionalFields(t *testing.T) {
	tests := []struct {
		name           string
		json           string
		hasOrder       bool
		hasTask        bool
		hasMachine     bool
		hasInspector   bool
		hasCertificate bool
	}{
		{
			name:           "inspection with order only",
			json:           `{"order_id": "550e8400-e29b-41d4-a716-446655440000"}`,
			hasOrder:       true,
			hasTask:        false,
			hasMachine:     false,
			hasInspector:   false,
			hasCertificate: false,
		},
		{
			name:           "inspection with task and inspector",
			json:           `{"task_id": "550e8400-e29b-41d4-a716-446655440001", "inspector_id": "550e8400-e29b-41d4-a716-446655440004"}`,
			hasOrder:       false,
			hasTask:        true,
			hasMachine:     false,
			hasInspector:   true,
			hasCertificate: false,
		},
		{
			name:           "inspection with all optional IDs",
			json:           `{"order_id": "550e8400-e29b-41d4-a716-446655440000", "task_id": "550e8400-e29b-41d4-a716-446655440001", "machine_id": "550e8400-e29b-41d4-a716-446655440003", "inspector_id": "550e8400-e29b-41d4-a716-446655440004"}`,
			hasOrder:       true,
			hasTask:        true,
			hasMachine:     true,
			hasInspector:   true,
			hasCertificate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CreateInspectionRequest
			if err := json.Unmarshal([]byte(tt.json), &req); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			hasOrder := req.OrderID != nil
			hasTask := req.TaskID != nil
			hasMachine := req.MachineID != nil
			hasInspector := req.InspectorID != nil

			if hasOrder != tt.hasOrder {
				t.Errorf("order_id: got %v, want %v", hasOrder, tt.hasOrder)
			}
			if hasTask != tt.hasTask {
				t.Errorf("task_id: got %v, want %v", hasTask, tt.hasTask)
			}
			if hasMachine != tt.hasMachine {
				t.Errorf("machine_id: got %v, want %v", hasMachine, tt.hasMachine)
			}
			if hasInspector != tt.hasInspector {
				t.Errorf("inspector_id: got %v, want %v", hasInspector, tt.hasInspector)
			}
		})
	}
}

// =============== Batch Lots Tests ===============

func TestCreateBatchLotRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request CreateBatchLotRequest
		valid   bool
	}{
		{
			name: "valid request with required fields",
			request: CreateBatchLotRequest{
				LotNumber:   "LOT-001",
				ProductName: "Test Product",
				Quantity:    100.0,
				Unit:        "kg",
			},
			valid: true,
		},
		{
			name: "valid request with all fields",
			request: CreateBatchLotRequest{
				LotNumber:         "LOT-002",
				ProductName:       "Test Product 2",
				ProductCode:       "PROD-002",
				Quantity:          50.5,
				Unit:              "liters",
				SupplierName:      "Test Supplier",
				SupplierLotNumber: "SUP-LOT-001",
				PurchaseOrder:     "PO-001",
				Status:            "active",
			},
			valid: true,
		},
		{
			name: "invalid request missing lot number",
			request: CreateBatchLotRequest{
				ProductName: "Test Product",
				Quantity:    100.0,
				Unit:        "kg",
			},
			valid: false,
		},
		{
			name: "invalid request missing product name",
			request: CreateBatchLotRequest{
				LotNumber: "LOT-003",
				Quantity:  100.0,
				Unit:      "kg",
			},
			valid: false,
		},
		{
			name: "invalid request missing quantity",
			request: CreateBatchLotRequest{
				LotNumber:   "LOT-004",
				ProductName: "Test Product",
				Unit:        "kg",
			},
			valid: false,
		},
		{
			name: "invalid request missing unit",
			request: CreateBatchLotRequest{
				LotNumber:   "LOT-005",
				ProductName: "Test Product",
				Quantity:    100.0,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			log := logrus.New()
			log.SetLevel(logrus.PanicLevel)
			handler := NewQualityHandler(nil, nil, nil, log)

			router := gin.New()
			router.POST("/batch-lots", func(c *gin.Context) {
				var req CreateBatchLotRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"valid": true})
			})
			_ = handler // handler used for type reference

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/batch-lots", bytes.NewBuffer(body))
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

func TestUpdateBatchLotRequest_Fields(t *testing.T) {
	tests := []struct {
		name    string
		request UpdateBatchLotRequest
	}{
		{
			name: "update product name only",
			request: UpdateBatchLotRequest{
				ProductName: "Updated Product",
			},
		},
		{
			name: "update quantity and unit",
			request: UpdateBatchLotRequest{
				Quantity: 200.5,
				Unit:     "liters",
			},
		},
		{
			name: "update supplier information",
			request: UpdateBatchLotRequest{
				SupplierName:      "New Supplier",
				SupplierLotNumber: "NEW-LOT-001",
				PurchaseOrder:     "PO-002",
			},
		},
		{
			name: "update status",
			request: UpdateBatchLotRequest{
				Status: "quarantine",
			},
		},
		{
			name: "update multiple fields",
			request: UpdateBatchLotRequest{
				ProductName:       "Updated Product",
				ProductCode:       "PROD-NEW",
				Quantity:          150.0,
				Unit:              "kg",
				SupplierName:      "Updated Supplier",
				SupplierLotNumber: "UPD-LOT-001",
				Status:            "released",
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

			var decoded UpdateBatchLotRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if decoded.ProductName != tt.request.ProductName {
				t.Errorf("ProductName mismatch: got %q, want %q", decoded.ProductName, tt.request.ProductName)
			}
			if decoded.Status != tt.request.Status {
				t.Errorf("Status mismatch: got %q, want %q", decoded.Status, tt.request.Status)
			}
			if decoded.Quantity != tt.request.Quantity {
				t.Errorf("Quantity mismatch: got %f, want %f", decoded.Quantity, tt.request.Quantity)
			}
		})
	}
}

func TestBatchLot_OptionalDates(t *testing.T) {
	now := time.Now()
	expiry := now.AddDate(0, 6, 0)    // 6 months from now
	received := now.AddDate(0, 0, -7) // 7 days ago

	tests := []struct {
		name            string
		json            string
		hasManufactured bool
		hasExpiry       bool
		hasReceived     bool
	}{
		{
			name:            "batch lot with manufactured date only",
			json:            `{"manufactured_date": "2024-01-01T00:00:00Z"}`,
			hasManufactured: true,
			hasExpiry:       false,
			hasReceived:     false,
		},
		{
			name:            "batch lot with all dates",
			json:            `{"manufactured_date": "2024-01-01T00:00:00Z", "expiry_date": "2024-12-31T00:00:00Z", "received_date": "2024-01-15T00:00:00Z"}`,
			hasManufactured: true,
			hasExpiry:       true,
			hasReceived:     true,
		},
		{
			name:            "batch lot with no dates",
			json:            `{}`,
			hasManufactured: false,
			hasExpiry:       false,
			hasReceived:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CreateBatchLotRequest
			if err := json.Unmarshal([]byte(tt.json), &req); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			hasManufactured := req.ManufacturedDate != nil
			hasExpiry := req.ExpiryDate != nil
			hasReceived := req.ReceivedDate != nil

			if hasManufactured != tt.hasManufactured {
				t.Errorf("manufactured_date: got %v, want %v", hasManufactured, tt.hasManufactured)
			}
			if hasExpiry != tt.hasExpiry {
				t.Errorf("expiry_date: got %v, want %v", hasExpiry, tt.hasExpiry)
			}
			if hasReceived != tt.hasReceived {
				t.Errorf("received_date: got %v, want %v", hasReceived, tt.hasReceived)
			}
		})
	}

	// Suppress unused variable warnings
	_ = now
	_ = expiry
	_ = received
}

func TestBatchLot_SupplierFields(t *testing.T) {
	tests := []struct {
		name         string
		json         string
		hasSupplier  bool
		hasLotNumber bool
		hasPO        bool
	}{
		{
			name:         "batch lot with supplier name only",
			json:         `{"supplier_name": "Test Supplier"}`,
			hasSupplier:  true,
			hasLotNumber: false,
			hasPO:        false,
		},
		{
			name:         "batch lot with all supplier fields",
			json:         `{"supplier_name": "Test Supplier", "supplier_lot_number": "SUP-001", "purchase_order": "PO-001"}`,
			hasSupplier:  true,
			hasLotNumber: true,
			hasPO:        true,
		},
		{
			name:         "batch lot with no supplier fields",
			json:         `{}`,
			hasSupplier:  false,
			hasLotNumber: false,
			hasPO:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CreateBatchLotRequest
			if err := json.Unmarshal([]byte(tt.json), &req); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			hasSupplier := req.SupplierName != ""
			hasLotNumber := req.SupplierLotNumber != ""
			hasPO := req.PurchaseOrder != ""

			if hasSupplier != tt.hasSupplier {
				t.Errorf("supplier_name: got %v, want %v", hasSupplier, tt.hasSupplier)
			}
			if hasLotNumber != tt.hasLotNumber {
				t.Errorf("supplier_lot_number: got %v, want %v", hasLotNumber, tt.hasLotNumber)
			}
			if hasPO != tt.hasPO {
				t.Errorf("purchase_order: got %v, want %v", hasPO, tt.hasPO)
			}
		})
	}
}

// =============== Metadata Tests ===============

func TestMetadata_Marshaling(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
	}{
		{
			name:     "empty metadata",
			metadata: map[string]any{},
		},
		{
			name: "simple metadata",
			metadata: map[string]any{
				"key1": "value1",
				"key2": 123,
			},
		},
		{
			name: "nested metadata",
			metadata: map[string]any{
				"outer": map[string]any{
					"inner": "value",
				},
			},
		},
		{
			name: "array metadata",
			metadata: map[string]any{
				"items": []any{"item1", "item2", "item3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test with certificate request
			certReq := CreateCertificateRequest{
				CertificateNumber: "CERT-001",
				Type:              types.QualityCertTypeCOC,
				Title:             "Test",
				Metadata:          tt.metadata,
			}

			data, err := json.Marshal(certReq)
			if err != nil {
				t.Fatalf("failed to marshal certificate request: %v", err)
			}

			var decoded CreateCertificateRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal certificate request: %v", err)
			}

			if len(decoded.Metadata) != len(tt.metadata) {
				t.Errorf("metadata length mismatch: got %d, want %d", len(decoded.Metadata), len(tt.metadata))
			}
		})
	}
}

// =============== Pagination Tests ===============

func TestListResponse_QualityPagination(t *testing.T) {
	certID := uuid.New()
	mockCerts := []any{
		types.QualityCertificate{
			ID:                certID,
			TenantID:          uuid.New(),
			CertificateNumber: "CERT-001",
			Type:              types.QualityCertTypeCOC,
			Status:            types.QualityCertStatusDraft,
			Title:             "Certificate 1",
		},
	}

	tests := []struct {
		name     string
		response ListResponse
	}{
		{
			name: "empty certificates list",
			response: ListResponse{
				Data:   []any{},
				Total:  0,
				Limit:  20,
				Offset: 0,
			},
		},
		{
			name: "first page of certificates",
			response: ListResponse{
				Data:   mockCerts,
				Total:  100,
				Limit:  20,
				Offset: 0,
			},
		},
		{
			name: "middle page of certificates",
			response: ListResponse{
				Data:   mockCerts,
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
