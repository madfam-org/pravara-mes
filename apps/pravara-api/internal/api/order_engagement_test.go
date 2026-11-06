package api

import (
	"bytes"
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
)

func orderRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	h := NewOrderHandler(repositories.NewOrderRepository(db), repositories.NewOrderItemRepository(db), log)
	tenantID := uuid.New()
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyTenantID), tenantID.String()) })
	router.POST("/orders", h.Create)
	router.PATCH("/orders/:id", h.Update)
	return router, mock, tenantID
}

func send(router *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewBuffer(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestOrderCreate_EngagementSlug(t *testing.T) {
	for _, bad := range []string{"Acme", "-acme", "acme_2026", "eng:acme", "acmé"} {
		router, mock, _ := orderRouter(t)
		w := send(router, http.MethodPost, "/orders", map[string]any{"customer_name": "c", "engagement_slug": bad})
		assert.Equal(t, http.StatusBadRequest, w.Code, bad)
		assert.Contains(t, w.Body.String(), "invalid_engagement_slug", bad)
		assert.NoError(t, mock.ExpectationsWereMet(), "nothing is written for %q", bad)
	}

	router, mock, _ := orderRouter(t)
	a := sqlmock.AnyArg()
	mock.ExpectQuery("INSERT INTO orders").
		WithArgs(a, a, a, a, a, a, a, a, a, a, a, a, "acme-2026").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(time.Now(), time.Now()))
	w := send(router, http.MethodPost, "/orders", map[string]any{"customer_name": "c", "engagement_slug": "acme-2026"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"engagement_slug":"acme-2026"`)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOrderUpdate_EngagementSlug(t *testing.T) {
	cols := []string{"id", "tenant_id", "external_id", "customer_name", "customer_email",
		"status", "priority", "due_date", "total_amount", "currency",
		"shipping_address", "metadata", "created_at", "updated_at", "engagement_slug"}
	load := func(mock sqlmock.Sqlmock, id uuid.UUID, slug any) {
		now := time.Now()
		mock.ExpectQuery("SELECT id, tenant_id.*FROM orders WHERE id").WithArgs(id).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(id, uuid.New(), nil, "c", nil,
				"received", 5, nil, nil, "MXN", nil, []byte("{}"), now, now, slug))
	}
	a := sqlmock.AnyArg()
	var null *string
	set := "acme-2026"

	cases := []struct {
		name   string
		body   map[string]any
		before any
		want   any // 11th UPDATE argument
	}{
		{"set", map[string]any{"engagement_slug": "acme-2026"}, nil, &set},
		{"absent leaves it unchanged", map[string]any{"priority": 3}, "acme-2026", &set},
		{"empty clears it", map[string]any{"engagement_slug": ""}, "acme-2026", null},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, mock, _ := orderRouter(t)
			id := uuid.New()
			load(mock, id, tc.before)
			mock.ExpectQuery("UPDATE orders SET").WithArgs(a, a, a, a, a, a, a, a, a, a, tc.want).
				WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))
			w := send(router, http.MethodPatch, "/orders/"+id.String(), tc.body)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}

	t.Run("malformed is rejected", func(t *testing.T) {
		router, mock, _ := orderRouter(t)
		id := uuid.New()
		load(mock, id, nil)
		w := send(router, http.MethodPatch, "/orders/"+id.String(), map[string]any{"engagement_slug": "Not A Slug"})
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_engagement_slug")
		assert.NoError(t, mock.ExpectationsWereMet(), "no UPDATE is issued")
	})
}
