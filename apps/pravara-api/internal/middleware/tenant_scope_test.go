package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

const setTenantPattern = `SELECT set_config\('app.current_tenant_id', \$1, true\)`

// scopedRouter mounts handler behind runInTenantScope for tenantID.
func scopedRouter(t *testing.T, tenantID string, handler gin.HandlerFunc) (*gin.Engine, sqlmock.Sqlmock, *db.TenantDB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pool, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	database := &db.DB{DB: pool}
	r := gin.New()
	r.Use(func(c *gin.Context) { runInTenantScope(c, database, tenantID, newTestLogger()) })
	r.GET("/x", handler)
	return r, mock, database.Tenant(nil)
}

func TestTenantScope_CommitsBeforeFirstByte(t *testing.T) {
	tenantID := uuid.New().String()
	var tdb *db.TenantDB
	r, mock, q := scopedRouter(t, tenantID, func(c *gin.Context) {
		_, err := tdb.ExecContext(c.Request.Context(), "UPDATE machines SET status = 'idle'")
		require.NoError(t, err)
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})
	tdb = q
	mock.ExpectBegin()
	mock.ExpectExec(setTenantPattern).WithArgs(tenantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE machines").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.JSONEq(t, `{"ok":true}`, w.Body.String())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantScope_RollsBackOnErrorStatus(t *testing.T) {
	tenantID := uuid.New().String()
	var tdb *db.TenantDB
	r, mock, q := scopedRouter(t, tenantID, func(c *gin.Context) {
		_, _ = tdb.ExecContext(c.Request.Context(), "DELETE FROM machines")
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	})
	tdb = q
	mock.ExpectBegin()
	mock.ExpectExec(setTenantPattern).WithArgs(tenantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM machines").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantScope_CommitFailureBecomes500(t *testing.T) {
	tenantID := uuid.New().String()
	var tdb *db.TenantDB
	r, mock, q := scopedRouter(t, tenantID, func(c *gin.Context) {
		_, _ = tdb.ExecContext(c.Request.Context(), "INSERT INTO orders DEFAULT VALUES")
		c.JSON(http.StatusCreated, gin.H{"id": "would-be-lost"})
	})
	tdb = q
	mock.ExpectBegin()
	mock.ExpectExec(setTenantPattern).WithArgs(tenantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO orders").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(errors.New("serialization failure"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "would-be-lost")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantScope_NoStatementsOpensNoTransaction(t *testing.T) {
	r, mock, _ := scopedRouter(t, uuid.New().String(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantScope_InvalidTenantIsForbidden(t *testing.T) {
	r, mock, _ := scopedRouter(t, "not-a-uuid", func(c *gin.Context) {
		t.Fatal("handler must not run")
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantScope_PanicRollsBack(t *testing.T) {
	tenantID := uuid.New().String()
	var tdb *db.TenantDB
	r, mock, q := scopedRouter(t, tenantID, func(c *gin.Context) {
		_, _ = tdb.ExecContext(c.Request.Context(), "UPDATE tasks SET status = 'blocked'")
		panic("handler bug")
	})
	tdb = q
	r.Use(gin.Recovery())
	mock.ExpectBegin()
	mock.ExpectExec(setTenantPattern).WithArgs(tenantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE tasks").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	assert.Panics(t, func() {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	})
	assert.NoError(t, mock.ExpectationsWereMet())
}
