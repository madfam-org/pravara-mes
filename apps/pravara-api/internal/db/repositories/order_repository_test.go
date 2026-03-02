package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func TestOrderRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewOrderRepository(db)

	tests := []struct {
		name      string
		filter    OrderFilter
		mockSetup func(sqlmock.Sqlmock)
		wantCount int
		wantError bool
	}{
		{
			name: "list all orders",
			filter: OrderFilter{
				Limit:  10,
				Offset: 0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "external_id", "customer_name", "customer_email",
					"status", "priority", "due_date", "total_amount", "currency", "metadata",
					"created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), "EXT-001", "Customer A", "customer@example.com",
						types.OrderStatusReceived, 5, time.Now(), 1000.00, "USD", []byte("{}"),
						time.Now(), time.Now(),
					).
					AddRow(
						uuid.New(), uuid.New(), "EXT-002", "Customer B", "customer2@example.com",
						types.OrderStatusInProduction, 3, time.Now(), 2000.00, "USD", []byte("{}"),
						time.Now(), time.Now(),
					)

				mock.ExpectQuery("SELECT id, tenant_id").WillReturnRows(rows)
			},
			wantCount: 2,
			wantError: false,
		},
		{
			name: "filter by status",
			filter: OrderFilter{
				Status: func() *types.OrderStatus { s := types.OrderStatusReceived; return &s }(),
				Limit:  10,
				Offset: 0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND status").
					WithArgs(types.OrderStatusReceived).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "external_id", "customer_name", "customer_email",
					"status", "priority", "due_date", "total_amount", "currency", "metadata",
					"created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), "EXT-001", "Customer A", "customer@example.com",
						types.OrderStatusReceived, 5, time.Now(), 1000.00, "USD", []byte("{}"),
						time.Now(), time.Now(),
					)

				// Args: status=$1, limit=$2 (offset not added when 0)
				mock.ExpectQuery("SELECT id, tenant_id.*AND status").
					WithArgs(types.OrderStatusReceived, 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
		{
			name: "filter by priority",
			filter: OrderFilter{
				Priority: func() *int { p := 5; return &p }(),
				Limit:    10,
				Offset:   0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND priority").
					WithArgs(5).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "external_id", "customer_name", "customer_email",
					"status", "priority", "due_date", "total_amount", "currency", "metadata",
					"created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), "EXT-001", "Customer A", "customer@example.com",
						types.OrderStatusReceived, 5, time.Now(), 1000.00, "USD", []byte("{}"),
						time.Now(), time.Now(),
					)

				// Args: priority=$1, limit=$2 (offset not added when 0)
				mock.ExpectQuery("SELECT id, tenant_id.*AND priority").
					WithArgs(5, 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
		{
			name: "pagination",
			filter: OrderFilter{
				Limit:  5,
				Offset: 10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "external_id", "customer_name", "customer_email",
					"status", "priority", "due_date", "total_amount", "currency", "metadata",
					"created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), "EXT-011", "Customer K", "customerk@example.com",
						types.OrderStatusReceived, 3, time.Now(), 500.00, "USD", []byte("{}"),
						time.Now(), time.Now(),
					)

				mock.ExpectQuery("SELECT id, tenant_id.*LIMIT.*OFFSET").
					WithArgs(5, 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			orders, total, err := repo.List(context.Background(), tt.filter)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(orders) != tt.wantCount {
				t.Errorf("order count: got %d, want %d", len(orders), tt.wantCount)
			}

			if total < tt.wantCount {
				t.Errorf("total count should be >= order count, got %d", total)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestOrderRepository_GetByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewOrderRepository(db)

	tests := []struct {
		name      string
		orderID   uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantOrder bool
		wantError bool
	}{
		{
			name:    "order found",
			orderID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				metadata := map[string]interface{}{"key": "value"}
				metadataJSON, _ := json.Marshal(metadata)

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "external_id", "customer_name", "customer_email",
					"status", "priority", "due_date", "total_amount", "currency", "metadata",
					"created_at", "updated_at",
				}).AddRow(
					id, uuid.New(), "EXT-001", "Customer A", "customer@example.com",
					types.OrderStatusReceived, 5, time.Now(), 1000.00, "USD", metadataJSON,
					time.Now(), time.Now(),
				)

				mock.ExpectQuery("SELECT id, tenant_id.*FROM orders WHERE id").
					WithArgs(id).
					WillReturnRows(rows)
			},
			wantOrder: true,
			wantError: false,
		},
		{
			name:    "order not found",
			orderID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM orders WHERE id").
					WithArgs(id).
					WillReturnError(sql.ErrNoRows)
			},
			wantOrder: false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.orderID)

			order, err := repo.GetByID(context.Background(), tt.orderID)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantOrder && order == nil {
				t.Fatal("expected order, got nil")
			}
			if !tt.wantOrder && order != nil {
				t.Fatalf("expected nil order, got %+v", order)
			}

			if tt.wantOrder && order.ID != tt.orderID {
				t.Errorf("order ID: got %v, want %v", order.ID, tt.orderID)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestOrderRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewOrderRepository(db)

	tests := []struct {
		name      string
		order     *types.Order
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "create order success",
			order: &types.Order{
				TenantID:      uuid.New(),
				ExternalID:    "EXT-001",
				CustomerName:  "Customer A",
				CustomerEmail: "customer@example.com",
				Status:        types.OrderStatusReceived,
				Priority:      5,
				TotalAmount:   1000.00,
				Currency:      "USD",
				Metadata:      map[string]interface{}{"key": "value"},
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO orders").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create order with nil ID generates new ID",
			order: &types.Order{
				ID:           uuid.Nil,
				TenantID:     uuid.New(),
				CustomerName: "Customer B",
				Status:       types.OrderStatusReceived,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO orders").
					WillReturnRows(rows)
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			originalID := tt.order.ID
			err := repo.Create(context.Background(), tt.order)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.wantError {
				if tt.order.ID == uuid.Nil {
					t.Error("order ID should be generated if nil")
				}
				if originalID == uuid.Nil && tt.order.ID == uuid.Nil {
					t.Error("ID was nil and not generated")
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestOrderRepository_Update(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewOrderRepository(db)

	tests := []struct {
		name      string
		order     *types.Order
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "update order success",
			order: &types.Order{
				ID:            uuid.New(),
				CustomerName:  "Updated Customer",
				CustomerEmail: "updated@example.com",
				Status:        types.OrderStatusInProduction,
				Priority:      3,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"updated_at"}).
					AddRow(time.Now())

				mock.ExpectQuery("UPDATE orders SET").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "update order not found",
			order: &types.Order{
				ID:           uuid.New(),
				CustomerName: "Customer",
				Status:       types.OrderStatusReceived,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("UPDATE orders SET").
					WillReturnError(sql.ErrNoRows)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			err := repo.Update(context.Background(), tt.order)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestOrderRepository_UpdateStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewOrderRepository(db)

	tests := []struct {
		name      string
		orderID   uuid.UUID
		status    types.OrderStatus
		mockSetup func(sqlmock.Sqlmock, uuid.UUID, types.OrderStatus)
		wantError bool
	}{
		{
			name:    "update status success",
			orderID: uuid.New(),
			status:  types.OrderStatusDelivered,
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID, status types.OrderStatus) {
				mock.ExpectExec("UPDATE orders SET status").
					WithArgs(id, status).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:    "order not found",
			orderID: uuid.New(),
			status:  types.OrderStatusDelivered,
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID, status types.OrderStatus) {
				mock.ExpectExec("UPDATE orders SET status").
					WithArgs(id, status).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.orderID, tt.status)

			err := repo.UpdateStatus(context.Background(), tt.orderID, tt.status)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestOrderRepository_Delete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewOrderRepository(db)

	orderID := uuid.New()

	mock.ExpectExec("UPDATE orders SET status").
		WithArgs(orderID, types.OrderStatusCancelled).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.Delete(context.Background(), orderID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestOrderRepository_GetByExternalID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewOrderRepository(db)

	externalID := "EXT-12345"

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "external_id", "customer_name", "customer_email",
		"status", "priority", "due_date", "total_amount", "currency", "metadata",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New(), uuid.New(), externalID, "Customer A", "customer@example.com",
		types.OrderStatusReceived, 5, time.Now(), 1000.00, "USD", []byte("{}"),
		time.Now(), time.Now(),
	)

	mock.ExpectQuery("SELECT id, tenant_id.*FROM orders WHERE external_id").
		WithArgs(externalID).
		WillReturnRows(rows)

	order, err := repo.GetByExternalID(context.Background(), externalID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if order == nil {
		t.Fatal("expected order, got nil")
	}

	if order.ExternalID != externalID {
		t.Errorf("external ID: got %q, want %q", order.ExternalID, externalID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
