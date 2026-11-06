package api

// The ForgeSight webhook is reference only: it authenticates, verifies the
// signature and parses the payload, and never writes inventory (no statement
// reaches the database at all). forgesight_webhook_pg_test.go proves the same
// on PostgreSQL through the real routes.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// refusingQuerier fails the test on any database statement.
type refusingQuerier struct {
	t     *testing.T
	calls atomic.Int32
}

func (q *refusingQuerier) refuse(query string) {
	q.calls.Add(1)
	q.t.Errorf("ForgeSight webhook issued a database statement: %s", query)
}

func (q *refusingQuerier) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	q.refuse(query)
	return nil, sql.ErrConnDone
}

func (q *refusingQuerier) QueryContext(_ context.Context, query string, _ ...any) (*sql.Rows, error) {
	q.refuse(query)
	return nil, sql.ErrConnDone
}

func (q *refusingQuerier) QueryRowContext(_ context.Context, query string, _ ...any) *sql.Row {
	q.refuse(query)
	return &sql.Row{}
}

func forgeSightRouter(t *testing.T, secret string) (*gin.Engine, *refusingQuerier) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	q := &refusingQuerier{t: t}
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	h := NewInventoryHandler(repositories.NewInventoryRepository(q), log)
	h.forgeSightSecret = secret
	r := gin.New()
	r.POST("/v1/webhooks/forgesight", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyTenantID), "11111111-1111-1111-1111-111111111111")
		c.Next()
	}, h.ForgeSightWebhook)
	return r, q
}

func forgeSightPayload(t *testing.T) []byte {
	t.Helper()
	cost := 999.99
	body, err := json.Marshal(ForgeSightWebhookPayload{Event: "inventory.sync", Items: []ForgeSightWebhookItem{
		{ForgeSightID: "fs-pla-1", SKU: "FIL-PLA-BLK", Name: "PLA black", Unit: "g", Quantity: 5, UnitCost: &cost, Currency: "USD"},
		{ForgeSightID: "fs-new-2", SKU: "FIL-NEW", Unit: "g", Quantity: 40},
		{SKU: "NO-ID", Quantity: 1},
	}})
	require.NoError(t, err)
	return body
}

func postForgeSight(r *gin.Engine, body []byte, header, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/forgesight", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set(header, signature)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func forgeSightSign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestForgeSightWebhookIsReferenceOnly(t *testing.T) {
	r, q := forgeSightRouter(t, "")
	refBefore := testutil.ToFloat64(observability.ForgeSightReferenceItemsTotal.WithLabelValues(forgeSightItemReference))
	ignBefore := testutil.ToFloat64(observability.ForgeSightReferenceItemsTotal.WithLabelValues(forgeSightItemIgnored))

	w := postForgeSight(r, forgeSightPayload(t), "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(3), resp["received"])
	assert.Equal(t, float64(2), resp["processed"], "items with a forgesight_id are accepted as reference")
	assert.Equal(t, true, resp["reference_only"])
	assert.Zero(t, q.calls.Load(), "no inventory statement: unit_cost and quantity_on_hand cannot change")

	assert.Equal(t, refBefore+2, testutil.ToFloat64(observability.ForgeSightReferenceItemsTotal.WithLabelValues(forgeSightItemReference)))
	assert.Equal(t, ignBefore+1, testutil.ToFloat64(observability.ForgeSightReferenceItemsTotal.WithLabelValues(forgeSightItemIgnored)))
}

func TestForgeSightWebhookStillVerifiesTheSignature(t *testing.T) {
	const secret = "forgesight-test-secret"
	r, q := forgeSightRouter(t, secret)
	body := forgeSightPayload(t)

	w := postForgeSight(r, body, "", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code, "missing signature")

	w = postForgeSight(r, body, "X-ForgeSight-Signature", forgeSightSign("another-secret", body))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "wrong signature")

	// ForgeSight's dispatcher spells the header X-Forgesight-Signature; HTTP
	// header names are case-insensitive.
	w = postForgeSight(r, body, "X-Forgesight-Signature", forgeSightSign(secret, body))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Zero(t, q.calls.Load())
}

func TestForgeSightWebhookRejectsInvalidJSON(t *testing.T) {
	r, q := forgeSightRouter(t, "")
	w := postForgeSight(r, []byte(`{"items": [`), "", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Zero(t, q.calls.Load())
}
