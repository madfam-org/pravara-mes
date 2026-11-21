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
