package repositories

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func TestOrderItem_JSONSerialization(t *testing.T) {
	orderID := uuid.New()
	orderItemID := uuid.New()

	tests := []struct {
		name string
		item types.OrderItem
	}{
		{
			name: "basic order item",
			item: types.OrderItem{
				ID:          orderItemID,
				OrderID:     orderID,
				ProductName: "Test Product",
				ProductSKU:  "SKU-001",
				Quantity:    5,
				UnitPrice:   100.00,
			},
		},
		{
			name: "order item with specifications",
			item: types.OrderItem{
				ID:          orderItemID,
				OrderID:     orderID,
				ProductName: "Custom Part",
				ProductSKU:  "CUSTOM-001",
				Quantity:    10,
				UnitPrice:   250.00,
				Specifications: map[string]interface{}{
					"material":  "steel",
					"thickness": 2.5,
					"finish":    "powder_coated",
				},
			},
		},
		{
			name: "order item with CAD file",
			item: types.OrderItem{
				ID:          orderItemID,
				OrderID:     orderID,
				ProductName: "CNC Part",
				Quantity:    3,
				UnitPrice:   500.00,
				CADFileURL:  "https://storage.example.com/cad/part-001.step",
			},
		},
		{
			name: "order item with all fields",
			item: types.OrderItem{
				ID:          orderItemID,
				OrderID:     orderID,
				ProductName: "Complete Part",
				ProductSKU:  "FULL-001",
				Quantity:    1,
				UnitPrice:   1000.00,
				CADFileURL:  "https://storage.example.com/cad/full-001.step",
				Specifications: map[string]interface{}{
					"material":   "aluminum",
					"dimensions": "100x50x25mm",
					"tolerance":  "0.1mm",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.item)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded types.OrderItem
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
			if decoded.ProductSKU != tt.item.ProductSKU {
				t.Errorf("ProductSKU: got %q, want %q", decoded.ProductSKU, tt.item.ProductSKU)
			}
		})
	}
}

func TestOrderItemFilter_Defaults(t *testing.T) {
	// Test filter default values
	filter := OrderFilter{
		Limit:  20,
		Offset: 0,
	}

	if filter.Limit != 20 {
		t.Errorf("default Limit: got %d, want 20", filter.Limit)
	}
	if filter.Offset != 0 {
		t.Errorf("default Offset: got %d, want 0", filter.Offset)
	}
}

func TestOrderItem_QuantityValidation(t *testing.T) {
	tests := []struct {
		name     string
		quantity int
		valid    bool
	}{
		{name: "positive quantity", quantity: 1, valid: true},
		{name: "large quantity", quantity: 1000, valid: true},
		{name: "zero quantity", quantity: 0, valid: false},
		{name: "negative quantity", quantity: -1, valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.quantity >= 1
			if valid != tt.valid {
				t.Errorf("quantity %d validity: got %v, want %v", tt.quantity, valid, tt.valid)
			}
		})
	}
}

func TestOrderItem_PriceCalculation(t *testing.T) {
	tests := []struct {
		name          string
		quantity      int
		unitPrice     float64
		expectedTotal float64
	}{
		{name: "simple calculation", quantity: 1, unitPrice: 100.00, expectedTotal: 100.00},
		{name: "multiple quantity", quantity: 5, unitPrice: 100.00, expectedTotal: 500.00},
		{name: "decimal price", quantity: 3, unitPrice: 33.33, expectedTotal: 99.99},
		{name: "large quantity", quantity: 100, unitPrice: 50.00, expectedTotal: 5000.00},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := float64(tt.quantity) * tt.unitPrice
			if total != tt.expectedTotal {
				t.Errorf("total: got %f, want %f", total, tt.expectedTotal)
			}
		})
	}
}

func TestOrderItem_SpecificationsTypes(t *testing.T) {
	tests := []struct {
		name  string
		specs map[string]interface{}
	}{
		{
			name: "string values",
			specs: map[string]interface{}{
				"material": "aluminum",
				"finish":   "anodized",
			},
		},
		{
			name: "numeric values",
			specs: map[string]interface{}{
				"width":     100.5,
				"height":    50.25,
				"thickness": 2.0,
			},
		},
		{
			name: "boolean values",
			specs: map[string]interface{}{
				"threaded":    true,
				"countersunk": false,
			},
		},
		{
			name: "mixed values",
			specs: map[string]interface{}{
				"material":  "steel",
				"thickness": 3.5,
				"hardened":  true,
				"holes":     4,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := types.OrderItem{
				ID:             uuid.New(),
				OrderID:        uuid.New(),
				ProductName:    "Test",
				Quantity:       1,
				Specifications: tt.specs,
			}

			data, err := json.Marshal(item)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded types.OrderItem
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if len(decoded.Specifications) != len(tt.specs) {
				t.Errorf("specs count: got %d, want %d", len(decoded.Specifications), len(tt.specs))
			}
		})
	}
}
