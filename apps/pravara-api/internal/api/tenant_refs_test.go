package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// runRefCheck runs requireTenantRefs in a request and returns the response
// and whether the handler would go on to write.
func runRefCheck(t *testing.T, q repositories.DBTX, refs ...tenantRef) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := logrus.New()
	log.SetOutput(io.Discard)
	var passed bool
	router := gin.New()
	router.POST("/x", func(c *gin.Context) {
		if !requireTenantRefs(c, q, log, refs...) {
			return
		}
		passed = true
		c.Status(http.StatusCreated)
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
	return w, passed
}

func TestRequireTenantRefs(t *testing.T) {
	const existsQuery = `SELECT EXISTS \(SELECT 1 FROM (\w+) WHERE id = \$1 AND tenant_id = NULLIF\(current_setting\('app.current_tenant_id', true\), ''\)::uuid\)`
	newMock := func(t *testing.T) (repositories.DBTX, sqlmock.Sqlmock) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		return db, mock
	}
	machine, user := uuid.New(), uuid.New()

	t.Run("all found", func(t *testing.T) {
		q, mock := newMock(t)
		mock.ExpectQuery(existsQuery).WithArgs(machine).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		mock.ExpectQuery(existsQuery).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		w, passed := runRefCheck(t, q,
			optRef(repositories.RefMachines, &machine),
			optRef(repositories.RefOrders, nil), // absent: not looked up
			ref(repositories.RefUsers, user))
		assert.True(t, passed)
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not in the tenant", func(t *testing.T) {
		q, mock := newMock(t)
		mock.ExpectQuery(existsQuery).WithArgs(machine).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		mock.ExpectQuery(existsQuery).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
		w, passed := runRefCheck(t, q, ref(repositories.RefMachines, machine), ref(repositories.RefUsers, user))
		assert.False(t, passed)
		assert.Equal(t, http.StatusNotFound, w.Code)
		var body map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "not_found", body["error"])
		assert.Equal(t, "User not found: "+user.String(), body["message"])
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("order items are scoped through their order", func(t *testing.T) {
		q, mock := newMock(t)
		item := uuid.New()
		mock.ExpectQuery(`FROM order_items WHERE id = \$1 AND order_id IN \(SELECT id FROM orders WHERE tenant_id = `).
			WithArgs(item).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
		w, passed := runRefCheck(t, q, ref(repositories.RefOrderItems, item))
		assert.False(t, passed)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("lookup fails", func(t *testing.T) {
		q, mock := newMock(t)
		mock.ExpectQuery(existsQuery).WithArgs(machine).WillReturnError(errors.New("boom"))
		w, passed := runRefCheck(t, q, ref(repositories.RefMachines, machine))
		assert.False(t, passed)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no lookup configured", func(t *testing.T) {
		w, passed := runRefCheck(t, nil, ref(repositories.RefMachines, machine))
		assert.False(t, passed)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("nothing referenced", func(t *testing.T) {
		w, passed := runRefCheck(t, nil, optRef(repositories.RefMachines, nil))
		assert.True(t, passed)
		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

// TestComputeRoutes_RefuseMachinesOutsideTenant: OEE and SPC computations
// write a row for the machine they name, so the machine is looked up in the
// caller's tenant first; a machine that is not found answers 404 and nothing
// else runs (sqlmock fails any unexpected statement).
func TestComputeRoutes_RefuseMachinesOutsideTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := logrus.New()
	log.SetOutput(io.Discard)
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oeeRepo := repositories.NewOEERepository(sqlDB)
	analytics := NewAnalyticsHandler(oeeRepo, sqlDB, log)
	analytics.SetOEEService(services.NewOEEService(oeeRepo, nil, log))
	spcRepo := repositories.NewSPCRepository(sqlDB)
	spc := NewSPCHandler(spcRepo, sqlDB, log)
	spc.SetSPCService(services.NewSPCService(spcRepo, nil, log))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyTenantID), uuid.NewString())
		c.Next()
	})
	router.POST("/v1/analytics/oee/compute", analytics.ComputeOEE)
	router.POST("/v1/analytics/spc/limits/compute", spc.ComputeLimits)

	machine := uuid.NewString()
	for path, body := range map[string]string{
		"/v1/analytics/oee/compute":        `{"machine_id":"` + machine + `","date":"2026-11-01"}`,
		"/v1/analytics/spc/limits/compute": `{"machine_id":"` + machine + `","metric_type":"temperature"}`,
	} {
		t.Run(path, func(t *testing.T) {
			mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM machines WHERE id = \$1`).
				WithArgs(machine).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
