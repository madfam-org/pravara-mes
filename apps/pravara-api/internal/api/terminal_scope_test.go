package api

// The terminal scope (RFC 0064, rulings PV-1 and PV-10): pravara-mes:terminal
// opens exactly TerminalReadRoutes, and a token holding only that scope is
// refused on every other route, every write among them.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
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
)

var routeParam = regexp.MustCompile(`[:*][A-Za-z]+`)

// everyRouteBehindTheMatrix serves every registered /v1 route pattern with a
// 200 handler behind the real machine route table, with caller standing in
// for authentication.
func everyRouteBehindTheMatrix(t *testing.T, caller middleware.Caller) (*gin.Engine, []gin.RouteInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyCaller), caller)
		c.Next()
	})
	router.Use(middleware.EnforceRouteScopes(MachineRouteScopes(), nil))
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	var routes []gin.RouteInfo
	nonJWT := NonJWTRoutes()
	for _, r := range registeredRouter(t).Routes() {
		if !strings.HasPrefix(r.Path, "/v1/") {
			continue
		}
		if _, unauthenticated := nonJWT[middleware.RouteKey(r.Method, r.Path)]; unauthenticated {
			continue // no token reaches these; NonJWTRoutes documents their guard
		}
		router.Handle(r.Method, r.Path, ok)
		routes = append(routes, r)
	}
	require.NotEmpty(t, routes)
	return router, routes
}

func terminalOnlyCaller() middleware.Caller {
	return middleware.Caller{
		Kind:   middleware.CallerServiceAccount,
		ID:     "jnc_terminal_test",
		Scopes: []string{middleware.ScopeTerminal},
	}
}

// Both directions over every registered route: the terminal reads answer,
// everything else is 403, writes included.
func TestTerminalScopeOpensExactlyTheTerminalReads(t *testing.T) {
	router, routes := everyRouteBehindTheMatrix(t, terminalOnlyCaller())
	open := map[string]bool{}
	for _, key := range TerminalReadRoutes() {
		open[key] = true
	}
	reached := 0
	for _, r := range routes {
		key := middleware.RouteKey(r.Method, r.Path)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(r.Method, routeParam.ReplaceAllString(r.Path, "x"), nil))
		if open[key] {
			reached++
			assert.Equal(t, http.StatusOK, w.Code, "%s is a terminal read and must accept %s", key, middleware.ScopeTerminal)
			continue
		}
		assert.Equal(t, http.StatusForbidden, w.Code, "%s must refuse a token holding only %s", key, middleware.ScopeTerminal)
	}
	assert.Equal(t, len(open), reached, "every terminal read is a registered route")
}

// PV-1: no machine scope opens an operator's act. Every terminal route is a
// read, and the writes a terminal's operator makes stay out of the matrix.
func TestTerminalScopeOpensNoWrite(t *testing.T) {
	for _, key := range TerminalReadRoutes() {
		assert.True(t, strings.HasPrefix(key, http.MethodGet+" "), "%s: the terminal scope opens reads only", key)
	}
	policy := MachineRouteScopes()
	for _, key := range []string{
		"POST /v1/tasks/:id/move",
		"POST /v1/tasks/:id/consumption",
		"POST /v1/tasks/:id/work-instructions/:wiId/acknowledge",
		"POST /v1/tasks",
		"PATCH /v1/tasks/:id",
		"DELETE /v1/tasks/:id",
		"POST /v1/tasks/:id/assign",
		"POST /v1/jobs/consumption/:id/corrections",
		"POST /v1/quality/inspections",
		"PATCH /v1/quality/inspections/:id",
		"POST /v1/quality/inspections/:id/complete",
		"POST /v1/quality/batches",
		"PATCH /v1/quality/batches/:id",
		"DELETE /v1/quality/batches/:id",
	} {
		_, listed := policy[key]
		assert.False(t, listed, "%s is an operator's act: no machine scope may open it (PV-1)", key)
	}
}

// PV-10: the task reads, a task's work instructions and the lot lookup are
// the terminal's alone; pravara-mes:read does not open them.
func TestTerminalOnlyReadsAreNotOpenedByTheReadScope(t *testing.T) {
	policy := MachineRouteScopes()
	for _, key := range []string{
		"GET /v1/tasks",
		"GET /v1/tasks/board",
		"GET /v1/tasks/:id",
		"GET /v1/tasks/:id/work-instructions",
		"GET /v1/quality/batches/lookup",
	} {
		req, listed := policy[key]
		if assert.True(t, listed, key) {
			assert.Equal(t, []string{middleware.ScopeTerminal}, req.AnyOf, "%s is under %s only", key, middleware.ScopeTerminal)
			assert.Empty(t, req.LegacyAPIKeyScopes, key)
		}
	}
}

// The terminal scope is a device token's, never an API key's.
func TestTerminalScopeIsNotAnAPIKeyScope(t *testing.T) {
	assert.False(t, middleware.IsValidAPIKeyScope(middleware.ScopeTerminal))
	assert.Contains(t, middleware.MachineScopes, middleware.ScopeTerminal)
}

// A terminal also reaches nothing a person's role gates, nor the live stream.
func TestTerminalScopeLeavesTheStreamAndAdminClosed(t *testing.T) {
	router, _ := everyRouteBehindTheMatrix(t, terminalOnlyCaller())
	for _, path := range []string{"/v1/events/stream", "/v1/api-keys", "/v1/inventory", "/v1/quality/batches", "/v1/quality/batches/x"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusForbidden, w.Code, path)
	}
}

func lotLookupRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	h := NewQualityHandler(nil, nil, repositories.NewBatchLotRepository(sqlDB), nil, log)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/v1/quality/batches/lookup", h.LookupBatchLot)
	return router, mock
}

var lotLookupColumns = []string{"id", "lot_number", "status", "product_name", "product_code",
	"quantity", "unit", "expiry_date", "item_id", "sku", "material_id", "code"}

func TestLookupBatchLotAnswersTheLotAndItsMaterial(t *testing.T) {
	router, mock := lotLookupRouter(t)
	lotID, itemID, materialID := uuid.New(), uuid.New(), uuid.New()
	expiry := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM batch_lots bl`).WithArgs("LOT-PLA-0042").WillReturnRows(
		sqlmock.NewRows(lotLookupColumns).AddRow(lotID, "LOT-PLA-0042", "active", "PLA negro 1.75", "PLA-BLK",
			750.0, "g", expiry, itemID, "FIL-PLA-BLK", materialID, "FIL-PLA-BLK"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/quality/batches/lookup?lot_number=LOT-PLA-0042", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, lotID.String(), got["id"])
	assert.Equal(t, "active", got["status"])
	assert.Equal(t, itemID.String(), got["inventory_item_id"])
	assert.Equal(t, "FIL-PLA-BLK", got["sku"])
	assert.Equal(t, materialID.String(), got["material_id"])
	assert.Equal(t, "FIL-PLA-BLK", got["material_code"])
	for _, field := range []string{"supplier_name", "purchase_order", "metadata", "order_id", "tenant_id"} {
		assert.NotContains(t, got, field, "the lookup answers the lot's identity and material only")
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLookupBatchLotUnlinkedLotHasNoMaterial(t *testing.T) {
	router, mock := lotLookupRouter(t)
	mock.ExpectQuery(`FROM batch_lots bl`).WithArgs("LOT-1").WillReturnRows(
		sqlmock.NewRows(lotLookupColumns).AddRow(uuid.New(), "LOT-1", "quarantine", "Resina", nil,
			1.0, "l", nil, nil, nil, nil, nil))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/quality/batches/lookup?lot_number=LOT-1", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "quarantine", got["status"])
	for _, field := range []string{"inventory_item_id", "sku", "material_id", "material_code", "expiry_date", "product_code"} {
		assert.NotContains(t, got, field)
	}
}

func TestLookupBatchLotUnknownNumberIs404(t *testing.T) {
	router, mock := lotLookupRouter(t)
	mock.ExpectQuery(`FROM batch_lots bl`).WithArgs("NOPE").WillReturnRows(sqlmock.NewRows(lotLookupColumns))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/quality/batches/lookup?lot_number=NOPE", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), `"lot_not_found"`)
}

func TestLookupBatchLotNeedsANumber(t *testing.T) {
	router, _ := lotLookupRouter(t)
	for _, q := range []string{"", "?lot_number=", "?lot_number=%20", "?lot_number=" + strings.Repeat("A", 101)} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/quality/batches/lookup"+q, nil))
		assert.Equal(t, http.StatusBadRequest, w.Code, q)
		assert.Contains(t, w.Body.String(), `"invalid_lot_number"`, q)
	}
}
