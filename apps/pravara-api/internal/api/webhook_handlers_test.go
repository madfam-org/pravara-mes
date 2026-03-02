package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestWebhookHandler_CotizaPayload_Parsing(t *testing.T) {
	tests := []struct {
		name    string
		payload CotizaWebhookPayload
		valid   bool
	}{
		{
			name: "valid order.created event",
			payload: CotizaWebhookPayload{
				Event:     "order.created",
				Timestamp: time.Now().Format(time.RFC3339),
				Order: CotizaOrderData{
					ID:            "cotiza-123",
					CustomerName:  "Test Customer",
					CustomerEmail: "test@example.com",
					TotalAmount:   1500.00,
					Currency:      "MXN",
					Items: []CotizaItemData{
						{
							ProductName: "Custom Part",
							Quantity:    10,
							UnitPrice:   150.00,
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "valid order.updated event",
			payload: CotizaWebhookPayload{
				Event:     "order.updated",
				Timestamp: time.Now().Format(time.RFC3339),
				Order: CotizaOrderData{
					ID:           "cotiza-123",
					CustomerName: "Updated Customer",
				},
			},
			valid: true,
		},
		{
			name: "valid order.cancelled event",
			payload: CotizaWebhookPayload{
				Event:     "order.cancelled",
				Timestamp: time.Now().Format(time.RFC3339),
				Order: CotizaOrderData{
					ID: "cotiza-123",
				},
			},
			valid: true,
		},
		{
			name: "payload with multiple items",
			payload: CotizaWebhookPayload{
				Event:     "order.created",
				Timestamp: time.Now().Format(time.RFC3339),
				Order: CotizaOrderData{
					ID:            "cotiza-456",
					CustomerName:  "Multi-Item Customer",
					CustomerEmail: "multi@example.com",
					TotalAmount:   5000.00,
					Currency:      "USD",
					Items: []CotizaItemData{
						{ProductName: "Part A", Quantity: 5, UnitPrice: 500.00, ProductSKU: "SKU-A"},
						{ProductName: "Part B", Quantity: 10, UnitPrice: 250.00, ProductSKU: "SKU-B"},
					},
				},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded CotizaWebhookPayload
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.Event != tt.payload.Event {
				t.Errorf("Event: got %q, want %q", decoded.Event, tt.payload.Event)
			}
			if decoded.Order.ID != tt.payload.Order.ID {
				t.Errorf("Order.ID: got %q, want %q", decoded.Order.ID, tt.payload.Order.ID)
			}
			if len(decoded.Order.Items) != len(tt.payload.Order.Items) {
				t.Errorf("Items count: got %d, want %d", len(decoded.Order.Items), len(tt.payload.Order.Items))
			}
		})
	}
}

func TestWebhookHandler_SignatureVerification(t *testing.T) {
	secret := "test_webhook_secret"

	tests := []struct {
		name           string
		payload        string
		secret         string
		validSignature bool
	}{
		{
			name:           "valid signature",
			payload:        `{"event":"order.created","order":{"id":"123"}}`,
			secret:         secret,
			validSignature: true,
		},
		{
			name:           "invalid signature with wrong secret",
			payload:        `{"event":"order.created","order":{"id":"123"}}`,
			secret:         "wrong_secret",
			validSignature: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate signature with the test secret
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(tt.payload))
			expectedSig := hex.EncodeToString(mac.Sum(nil))

			// Generate signature with the provided secret (may differ)
			mac2 := hmac.New(sha256.New, []byte(tt.secret))
			mac2.Write([]byte(tt.payload))
			providedSig := hex.EncodeToString(mac2.Sum(nil))

			// Verify signature
			isValid := hmac.Equal([]byte(expectedSig), []byte(providedSig))

			if isValid != tt.validSignature {
				t.Errorf("signature validation: got %v, want %v", isValid, tt.validSignature)
			}
		})
	}
}

func TestWebhookHandler_InvalidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	handler := NewWebhookHandler(nil, nil, log, "")

	router := gin.New()
	router.POST("/webhooks/cotiza", handler.CotizaWebhook)

	tests := []struct {
		name           string
		body           string
		expectedStatus int
	}{
		{
			name:           "invalid json",
			body:           `{invalid json}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing event field",
			body:           `{"order":{"id":"123","customer_name":"Test"}}`,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/webhooks/cotiza", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("status: got %d, want %d, body: %s", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

func TestCotizaItemData_Fields(t *testing.T) {
	tests := []struct {
		name string
		item CotizaItemData
	}{
		{
			name: "basic item",
			item: CotizaItemData{
				ProductName: "Test Product",
				ProductSKU:  "SKU-001",
				Quantity:    5,
				UnitPrice:   100.00,
			},
		},
		{
			name: "item with specifications",
			item: CotizaItemData{
				ProductName: "Custom Part",
				ProductSKU:  "CUSTOM-001",
				Quantity:    1,
				UnitPrice:   500.00,
				Specifications: map[string]interface{}{
					"material":  "aluminum",
					"thickness": 2.5,
					"finish":    "anodized",
				},
			},
		},
		{
			name: "item with CAD file",
			item: CotizaItemData{
				ProductName: "CNC Part",
				Quantity:    3,
				UnitPrice:   250.00,
				CADFileURL:  "https://storage.example.com/cad/part-001.step",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.item)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded CotizaItemData
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.ProductName != tt.item.ProductName {
				t.Errorf("ProductName: got %q, want %q", decoded.ProductName, tt.item.ProductName)
			}
			if decoded.Quantity != tt.item.Quantity {
				t.Errorf("Quantity: got %d, want %d", decoded.Quantity, tt.item.Quantity)
			}
			if decoded.UnitPrice != tt.item.UnitPrice {
				t.Errorf("UnitPrice: got %f, want %f", decoded.UnitPrice, tt.item.UnitPrice)
			}
		})
	}
}

func TestCotizaEventTypes(t *testing.T) {
	validEvents := []string{
		"order.created",
		"order.confirmed",
		"order.updated",
		"order.cancelled",
	}

	for _, event := range validEvents {
		t.Run(event, func(t *testing.T) {
			payload := CotizaWebhookPayload{
				Event:     event,
				Timestamp: time.Now().Format(time.RFC3339),
				Order: CotizaOrderData{
					ID:           "test-123",
					CustomerName: "Test Customer",
				},
			}

			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded CotizaWebhookPayload
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.Event != event {
				t.Errorf("Event: got %q, want %q", decoded.Event, event)
			}
		})
	}
}

func TestCotizaOrderData_Fields(t *testing.T) {
	dueDate := time.Now().Add(7 * 24 * time.Hour)

	tests := []struct {
		name  string
		order CotizaOrderData
	}{
		{
			name: "minimal order",
			order: CotizaOrderData{
				ID:           "order-001",
				CustomerName: "Test Customer",
			},
		},
		{
			name: "full order",
			order: CotizaOrderData{
				ID:            "order-002",
				CustomerName:  "Full Customer",
				CustomerEmail: "full@example.com",
				TotalAmount:   2500.00,
				Currency:      "USD",
				DueDate:       &dueDate,
				Priority:      1,
				Metadata: map[string]any{
					"source":     "cotiza",
					"project_id": "PROJ-001",
				},
			},
		},
		{
			name: "order with items",
			order: CotizaOrderData{
				ID:           "order-003",
				CustomerName: "Customer With Items",
				Items: []CotizaItemData{
					{ProductName: "Item 1", Quantity: 1, UnitPrice: 100.00},
					{ProductName: "Item 2", Quantity: 2, UnitPrice: 200.00},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.order)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded CotizaOrderData
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.ID != tt.order.ID {
				t.Errorf("ID: got %q, want %q", decoded.ID, tt.order.ID)
			}
			if decoded.CustomerName != tt.order.CustomerName {
				t.Errorf("CustomerName: got %q, want %q", decoded.CustomerName, tt.order.CustomerName)
			}
		})
	}
}
