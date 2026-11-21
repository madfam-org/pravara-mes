package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// memoryIdempotencyStore stands in for the table. Claims made by a request
// that answers 400 or more are dropped, as the request transaction's
// rollback drops them in PostgreSQL.
type memoryIdempotencyStore struct {
	mu      sync.Mutex
	rows    map[string]*repositories.IdempotencyRecord
	pending []string
	now     func() time.Time
}

func newMemoryIdempotencyStore() *memoryIdempotencyStore {
	return &memoryIdempotencyStore{rows: map[string]*repositories.IdempotencyRecord{}, now: time.Now}
}

func (s *memoryIdempotencyStore) Claim(_ context.Context, client, key, _ string, fingerprint string, notBefore time.Time) (*repositories.IdempotencyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := client + "\x00" + key
	if row, ok := s.rows[id]; ok && !row.CreatedAt.Before(notBefore) {
		copied := *row
		return &copied, nil
	}
	s.rows[id] = &repositories.IdempotencyRecord{RequestSHA256: fingerprint, CreatedAt: s.now()}
	s.pending = append(s.pending, id)
	return nil, nil
}

func (s *memoryIdempotencyStore) Complete(_ context.Context, client, key string, status int, contentType string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.rows[client+"\x00"+key]
	row.Completed, row.ResponseStatus, row.ResponseContentType = true, status, contentType
	row.ResponseBody = append([]byte(nil), body...)
	return nil
}

// end plays the request transaction's end: a status of 400 or more rolls
// the request's claims back.
func (s *memoryIdempotencyStore) end(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status >= http.StatusBadRequest {
		for _, id := range s.pending {
			if row := s.rows[id]; row != nil && !row.Completed {
				delete(s.rows, id)
			}
		}
	}
	s.pending = nil
}

type idempotencyRig struct {
	router *gin.Engine
	store  *memoryIdempotencyStore
	runs   int
	answer int
}

func newIdempotencyRig(t *testing.T) *idempotencyRig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rig := &idempotencyRig{store: newMemoryIdempotencyStore(), answer: http.StatusCreated}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if client := c.GetHeader("X-Test-Client"); client != "" {
			c.Set(string(ContextKeyClaims), &auth.Claims{ClientID: client})
		}
		c.Set(string(ContextKeyCaller), Caller{Kind: CallerUser, ID: "0b6b1f9e-0d7e-4c55-9a43-7c6f2b1d2e10"})
		c.Next()
		rig.store.end(c.Writer.Status())
	})
	router.POST("/v1/tasks/:id/move", Idempotent(rig.store, time.Hour, nil), func(c *gin.Context) {
		rig.runs++
		var body map[string]any
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error"})
			return
		}
		c.JSON(rig.answer, gin.H{"run": rig.runs, "status": body["status"]})
	})
	rig.router = router
	return rig
}

func (r *idempotencyRig) post(key, client, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/7/move", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(IdempotencyKeyHeader, key)
	}
	if client != "" {
		req.Header.Set("X-Test-Client", client)
	}
	w := httptest.NewRecorder()
	r.router.ServeHTTP(w, req)
	return w
}

const ulidKey = "01JDF3Q8Z5X7K2M4N6P8R0T2V4"

func TestIdempotentWithoutHeaderRunsEveryTime(t *testing.T) {
	rig := newIdempotencyRig(t)
	for i := 0; i < 2; i++ {
		w := rig.post("", "", `{"status":"in_progress"}`)
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Empty(t, w.Header().Get(IdempotentReplayedHeader))
	}
	assert.Equal(t, 2, rig.runs)
}

func TestIdempotentReplaysTheFirstAnswer(t *testing.T) {
	rig := newIdempotencyRig(t)
	first := rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress","from_status":"queued"}`)
	require.Equal(t, http.StatusCreated, first.Code)
	assert.Empty(t, first.Header().Get(IdempotentReplayedHeader))

	// The same request again, its JSON re-encoded in another key order.
	again := rig.post(ulidKey, "jnc_device_1", `{ "from_status": "queued", "status": "in_progress" }`)
	assert.Equal(t, http.StatusCreated, again.Code, "the stored status")
	assert.Equal(t, "true", again.Header().Get(IdempotentReplayedHeader))
	assert.Equal(t, first.Body.String(), again.Body.String(), "the stored body")
	assert.Equal(t, first.Header().Get("Content-Type"), again.Header().Get("Content-Type"))
	assert.Equal(t, 1, rig.runs, "nothing ran twice")
}

func TestIdempotentRefusesAKeyReusedForAnotherRequest(t *testing.T) {
	rig := newIdempotencyRig(t)
	require.Equal(t, http.StatusCreated, rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`).Code)
	w := rig.post(ulidKey, "jnc_device_1", `{"status":"completed"}`)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), `"idempotency_key_reused"`)
	assert.Equal(t, 1, rig.runs)
}

func TestIdempotentKeysBelongToOneClient(t *testing.T) {
	rig := newIdempotencyRig(t)
	require.Equal(t, http.StatusCreated, rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`).Code)
	// Another device with the same key: its own key, its own write.
	w := rig.post(ulidKey, "jnc_device_2", `{"status":"completed"}`)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Empty(t, w.Header().Get(IdempotentReplayedHeader))
	// A person with no client: the caller is the owner of the key.
	w = rig.post(ulidKey, "", `{"status":"blocked"}`)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, 3, rig.runs)
}

func TestIdempotentStoresNoFailedAnswer(t *testing.T) {
	rig := newIdempotencyRig(t)
	rig.answer = http.StatusConflict
	assert.Equal(t, http.StatusConflict, rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`).Code)
	rig.answer = http.StatusInternalServerError
	assert.Equal(t, http.StatusInternalServerError, rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`).Code)
	// The key stayed free: a retry runs and its success is stored.
	rig.answer = http.StatusOK
	assert.Equal(t, http.StatusOK, rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`).Code)
	w := rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "true", w.Header().Get(IdempotentReplayedHeader))
	assert.Equal(t, 3, rig.runs)
}

func TestIdempotentKeyExpires(t *testing.T) {
	rig := newIdempotencyRig(t)
	rig.store.now = func() time.Time { return time.Now().Add(-2 * time.Hour) } // older than the rig's hour
	require.Equal(t, http.StatusCreated, rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`).Code)
	rig.store.now = time.Now
	w := rig.post(ulidKey, "jnc_device_1", `{"status":"completed"}`)
	assert.Equal(t, http.StatusCreated, w.Code, "an expired key counts as unused")
	assert.Equal(t, 2, rig.runs)
}

func TestIdempotentRefusesABadKey(t *testing.T) {
	rig := newIdempotencyRig(t)
	for _, key := range []string{" ", strings.Repeat("k", 101), "tab\there", "ñandú"} {
		w := rig.post(key, "jnc_device_1", `{"status":"in_progress"}`)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%q", key)
		assert.Contains(t, w.Body.String(), `"invalid_idempotency_key"`)
	}
	assert.Equal(t, http.StatusCreated, rig.post(strings.Repeat("k", 100), "jnc_device_1", `{"status":"in_progress"}`).Code)

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/7/move", strings.NewReader(`{}`))
	req.Header.Add(IdempotencyKeyHeader, "a")
	req.Header.Add(IdempotencyKeyHeader, "b")
	w := httptest.NewRecorder()
	rig.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "two keys")
}

func TestIdempotentReplayRunsNoHandler(t *testing.T) {
	rig := newIdempotencyRig(t)
	rig.answer = http.StatusOK
	require.Equal(t, http.StatusOK, rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`).Code)
	for i := 0; i < 3; i++ {
		assert.Equal(t, http.StatusOK, rig.post(ulidKey, "jnc_device_1", `{"status":"in_progress"}`).Code)
	}
	assert.Equal(t, 1, rig.runs)
}

func TestIdempotencyClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Empty(t, idempotencyClient(c), "no caller: no client")

	c.Set(string(ContextKeyCaller), Caller{Kind: CallerAPIKey, ID: "k1"})
	assert.Equal(t, "api_key:k1", idempotencyClient(c))

	c.Set(string(ContextKeyCaller), Caller{Kind: CallerServiceAccount, ID: "jnc_device_1"})
	c.Set(string(ContextKeyClaims), &auth.Claims{ClientID: "jnc_device_1"})
	assert.Equal(t, "client:jnc_device_1", idempotencyClient(c))
}

func TestRequestFingerprintCanonicalJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/7/move", nil)
	a := requestFingerprint(req, []byte(`{"a":1,"b":[1,2]}`))
	assert.Equal(t, a, requestFingerprint(req, []byte("{ \"b\": [1, 2], \"a\": 1 }\n")))
	assert.NotEqual(t, a, requestFingerprint(req, []byte(`{"a":1.0,"b":[1,2]}`)), "numbers as written")
	other := httptest.NewRequest(http.MethodPost, "/v1/tasks/8/move", nil)
	assert.NotEqual(t, a, requestFingerprint(other, []byte(`{"a":1,"b":[1,2]}`)), "another task")
}

func TestIdempotentOnGuardsOnlyTheListedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMemoryIdempotencyStore()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyCaller), Caller{Kind: CallerUser, ID: "0b6b1f9e-0d7e-4c55-9a43-7c6f2b1d2e10"})
		c.Next()
		store.end(c.Writer.Status())
	})
	router.Use(IdempotentOn([]string{RouteKey(http.MethodPost, "/v1/tasks/:id/move")}, store, time.Hour, nil))
	runs := map[string]int{}
	router.POST("/v1/tasks/:id/move", func(c *gin.Context) { runs["move"]++; c.Status(http.StatusOK) })
	router.POST("/v1/tasks/:id/assign", func(c *gin.Context) { runs["assign"]++; c.Status(http.StatusOK) })

	for i := 0; i < 2; i++ {
		for _, path := range []string{"/v1/tasks/7/move", "/v1/tasks/7/assign"} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set(IdempotencyKeyHeader, ulidKey)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, path)
		}
	}
	assert.Equal(t, 1, runs["move"], "the listed route ran once")
	assert.Equal(t, 2, runs["assign"], "an unlisted route ignores the header")
}

type busyStore struct{ *memoryIdempotencyStore }

func (busyStore) Claim(context.Context, string, string, string, string, time.Time) (*repositories.IdempotencyRecord, error) {
	return nil, repositories.ErrIdempotencyBusy
}

// A key another request still holds is a retryable 503, and the write does
// not run.
func TestIdempotentBusyKeyIsRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyCaller), Caller{Kind: CallerUser, ID: "0b6b1f9e-0d7e-4c55-9a43-7c6f2b1d2e10"})
		c.Next()
	})
	ran := false
	router.POST("/v1/tasks/:id/move", Idempotent(busyStore{newMemoryIdempotencyStore()}, time.Hour, nil), func(c *gin.Context) { ran = true })
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/7/move", strings.NewReader(`{}`))
	req.Header.Set(IdempotencyKeyHeader, ulidKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
	assert.Contains(t, w.Body.String(), `"idempotency_key_busy"`)
	assert.False(t, ran)
}
