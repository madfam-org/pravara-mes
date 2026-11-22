package api

// The terminal's narrow order view (owner ruling 2026-11-20: "Narrow the
// terminal's order view, no customer fields"): a terminal-only caller never
// receives a customer, price, metadata or CAD-file field; callers holding
// pravara-mes:read or pravara-mes:jobs, and people, are unchanged.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// orderOnlyFields are what a terminal must never receive.
var orderOnlyFields = []string{"customer_name", "customer_email", "shipping_address", "engagement_slug",
	"total_amount", "currency", "metadata"}
var orderItemOnlyFields = []string{"unit_price", "cad_file_url"}

func orderViewRouter(t *testing.T, caller middleware.Caller) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	h := NewOrderHandler(repositories.NewOrderRepository(sqlDB), repositories.NewOrderItemRepository(sqlDB), log)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyCaller), caller)
		c.Next()
	})
	router.GET("/v1/orders/:id", h.GetByID)
	return router, mock
}

func expectFullOrder(mock sqlmock.Sqlmock, id uuid.UUID) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM orders`).WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{
		"id", "tenant_id", "external_id", "customer_name", "customer_email", "status", "priority", "due_date",
		"total_amount", "currency", "shipping_address", "metadata", "created_at", "updated_at", "engagement_slug",
	}).AddRow(id, uuid.New(), "Q-1001", "Taller Ejemplo", "compras@example.test", "confirmed", 2, now,
		1250.5, "MXN", []byte(`{"contact_name":"Persona Ejemplo","contact_phone":"+52 000 000 0000"}`),
		[]byte(`{"note":"x"}`), now, now, "eng:taller-ejemplo"))
}

func getOrder(t *testing.T, caller middleware.Caller) map[string]any {
	t.Helper()
	router, mock := orderViewRouter(t, caller)
	id := uuid.New()
	expectFullOrder(mock, id)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/orders/"+id.String(), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, id.String(), got["id"])
	return got
}

func TestTerminalOnlyCallerGetsNoCustomerFields(t *testing.T) {
	got := getOrder(t, terminalOnlyCaller())
	for _, field := range orderOnlyFields {
		assert.NotContains(t, got, field)
	}
	assert.Equal(t, "Q-1001", got["external_id"])
	assert.Equal(t, "confirmed", got["status"])
	for _, leak := range []string{"Taller Ejemplo", "compras@example.test", "Persona Ejemplo", "+52", "1250.5", "taller-ejemplo"} {
		raw, _ := json.Marshal(got)
		assert.NotContains(t, string(raw), leak)
	}
}

func TestReadAndJobsCallersKeepTheFullOrder(t *testing.T) {
	for _, caller := range []middleware.Caller{
		{Kind: middleware.CallerServiceAccount, ID: "jnc_read", Scopes: []string{middleware.ScopeRead}},
		{Kind: middleware.CallerServiceAccount, ID: "jnc_jobs", Scopes: []string{middleware.ScopeJobs}},
		{Kind: middleware.CallerServiceAccount, ID: "jnc_both", Scopes: []string{middleware.ScopeTerminal, middleware.ScopeRead}},
		{Kind: middleware.CallerUser, ID: uuid.NewString()},
		{Kind: middleware.CallerAPIKey, ID: uuid.NewString(), Scopes: []string{middleware.ScopeWildcard}},
	} {
		got := getOrder(t, caller)
		for _, field := range []string{"customer_name", "customer_email", "shipping_address", "total_amount", "currency"} {
			assert.Contains(t, got, field, "%s %s", caller.Kind, caller.Scopes)
		}
	}
}

func TestTerminalOrderItemsCarryNoPriceOrFile(t *testing.T) {
	items := terminalOrderItems([]types.OrderItem{{ID: uuid.New(), ProductName: "Soporte", ProductSKU: "SOP-1", Quantity: 4,
		UnitPrice: 99.9, CADFileURL: "https://files.example.test/part.step", Specifications: map[string]any{"material": "PLA"}}})
	raw, err := json.Marshal(TerminalOrderItemsResponse{Items: items, Count: 1})
	require.NoError(t, err)
	for _, field := range orderItemOnlyFields {
		assert.NotContains(t, string(raw), `"`+field+`"`)
	}
	assert.NotContains(t, string(raw), "files.example.test")
	assert.Contains(t, string(raw), `"material":"PLA"`)
}

func TestTerminalOnlyCallerIsADeviceTokenWithoutReadOrJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		caller middleware.Caller
		want   bool
	}{
		{terminalOnlyCaller(), true},
		{middleware.Caller{Kind: middleware.CallerServiceAccount, Scopes: []string{middleware.ScopeTerminal, middleware.ScopeJobs}}, false},
		{middleware.Caller{Kind: middleware.CallerServiceAccount, Scopes: []string{middleware.ScopeRead}}, false},
		{middleware.Caller{Kind: middleware.CallerAPIKey, Scopes: []string{middleware.ScopeTerminal}}, false},
		{middleware.Caller{Kind: middleware.CallerUser}, false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(string(middleware.ContextKeyCaller), tc.caller)
		assert.Equal(t, tc.want, isTerminalOnly(c), "%s %v", tc.caller.Kind, tc.caller.Scopes)
	}
}

func TestTerminalTaskKeepsOnlyKnownMetadata(t *testing.T) {
	task := terminalTask(types.Task{ID: uuid.New(), Title: "Soporte", Metadata: map[string]any{
		"source": "order_decomposition", "product_name": "Soporte", "product_sku": "SOP-1", "quantity": 4,
		"specifications": map[string]any{"material": "PLA"}, "assignment_basis": "capability_match",
		"assigned_machine_code": "SIM-01", "cad_file_url": "https://files.example.test/part.step",
		"note_by_a_person": "anything else",
	}})
	assert.NotContains(t, task.Metadata, "cad_file_url")
	assert.NotContains(t, task.Metadata, "note_by_a_person")
	for _, k := range []string{"source", "product_name", "product_sku", "quantity", "specifications", "assignment_basis", "assigned_machine_code"} {
		assert.Contains(t, task.Metadata, k)
	}
	assert.Nil(t, terminalTask(types.Task{Metadata: map[string]any{"cad_file_url": "x"}}).Metadata)
}

func TestTerminalEventRemovesTheCustomerName(t *testing.T) {
	created := repositories.OutboxEvent{EventType: "order.created", Payload: json.RawMessage(
		`{"id":"e1","type":"order.created","data":{"entity_id":"o1","entity_type":"order","name":"Taller Ejemplo","metadata":{"item_count":2}}}`)}
	status := repositories.OutboxEvent{EventType: "order.status_changed", Payload: json.RawMessage(
		`{"id":"e2","type":"order.status_changed","data":{"order_id":"o1","new_status":"confirmed","customer_name":"Taller Ejemplo"}}`)}
	moved := repositories.OutboxEvent{EventType: "task.moved", Payload: json.RawMessage(`{"data":{"task_title":"Soporte"}}`)}
	for _, e := range []repositories.OutboxEvent{created, status} {
		got := terminalEvent(e)
		assert.NotContains(t, string(got.Payload), "Taller Ejemplo", e.EventType)
		assert.Contains(t, string(got.Payload), `"o1"`, "the order id stays")
	}
	assert.Contains(t, string(terminalEvent(created).Payload), `"item_count":2`)
	assert.Equal(t, string(moved.Payload), string(terminalEvent(moved).Payload), "other event types are untouched")
	broken := terminalEvent(repositories.OutboxEvent{EventType: "order.created", Payload: json.RawMessage(`not json`)})
	assert.Equal(t, "{}", string(broken.Payload), "an unreadable order payload is not passed on")
}
