package api

// The kept-bytes read route under load: it holds no database connection
// while it reads the store, serves a bounded number of downloads at once,
// lifts the write deadline for its own response only, and stops reading when
// the client goes away. PostgreSQL-backed (opt-in, PRAVARA_TEST_DATABASE_URL).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/artifactstore"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// observedStore wraps the memory store: it records, for every Open, whether
// the request still held its tenant transaction, and can hold the body's
// reads until released (a slow store).
type observedStore struct {
	*artifactstore.Memory
	mu        sync.Mutex
	opens     int
	scoped    int // opens whose context carried the tenant scope
	heldTx    int // opens during which that scope held a transaction
	gate      chan struct{}
	cancelled int
	opened    chan struct{}
}

func (o *observedStore) Open(ctx context.Context, key string) (io.ReadCloser, *artifactstore.Info, error) {
	o.mu.Lock()
	o.opens++
	if _, ok := db.TenantIDFromContext(ctx); ok {
		o.scoped++
	}
	if db.ScopeActive(ctx) {
		o.heldTx++
	}
	gate, opened := o.gate, o.opened
	o.mu.Unlock()
	body, info, err := o.Memory.Open(ctx, key)
	if err != nil || gate == nil {
		return body, info, err
	}
	return &gatedBody{ReadCloser: body, ctx: ctx, gate: gate, opened: opened, o: o}, info, nil
}

type gatedBody struct {
	io.ReadCloser
	ctx       context.Context
	gate      chan struct{}
	opened    chan struct{}
	signalled bool
	o         *observedStore
}

// Read reports (once) that a read is waiting, then waits for the gate.
func (g *gatedBody) Read(p []byte) (int, error) {
	if !g.signalled && g.opened != nil {
		g.signalled = true
		g.opened <- struct{}{}
	}
	select {
	case <-g.gate:
		return g.ReadCloser.Read(p)
	case <-g.ctx.Done():
		g.o.mu.Lock()
		g.o.cancelled++
		g.o.mu.Unlock()
		return 0, g.ctx.Err()
	}
}

func TestDispatchArtifactReadUnderLoad(t *testing.T) {
	r := newDispatchRig(t)
	r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	id := r.create(t, r.seedTask(t, boxSpecs))
	r.runUntil(t, id, repositories.DispatchRendered)
	store := &observedStore{Memory: r.store}
	r.svc.Store = store
	person := r.userToken(t, "operator")
	path := "/v1/dispatches/" + id.String() + "/artifacts/artifact"

	t.Run("no tenant transaction is held during the store read", func(t *testing.T) {
		w := r.do(http.MethodGet, path, person, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		store.mu.Lock()
		defer store.mu.Unlock()
		require.Equal(t, 1, store.opens)
		assert.Equal(t, 1, store.scoped, "the request's tenant scope is still attached (RLS unchanged)")
		assert.Zero(t, store.heldTx, "but its transaction (and pooled connection) was released before the store read")
	})

	t.Run("the write deadline is lifted for this response, through the real server", func(t *testing.T) {
		type call struct {
			deadline time.Time
			err      error
		}
		var mu sync.Mutex
		var calls []call
		real := artifactWriteDeadline
		artifactWriteDeadline = func(w http.ResponseWriter, d time.Time) error {
			err := real(w, d)
			mu.Lock()
			calls = append(calls, call{d, err})
			mu.Unlock()
			return err
		}
		defer func() { artifactWriteDeadline = real }()

		srv := httptest.NewServer(r.router)
		defer srv.Close()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+person)
		started := time.Now()
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		mu.Lock()
		defer mu.Unlock()
		require.Len(t, calls, 1)
		assert.NoError(t, calls[0].err, "http.ResponseController reaches net/http's writer through the tenant-scope writer")
		size := int64(len(r.yantra.Geometry))
		assert.WithinDuration(t, started.Add(artifactReadBound(size)), calls[0].deadline, 5*time.Second)
		assert.Equal(t, 2*time.Minute+time.Second, artifactReadBound(size), "two minutes plus a second per MiB")
		assert.Equal(t, 2*time.Minute+257*time.Second, artifactReadBound(256<<20))
	})

	t.Run("at most two downloads at once; the third gets 503 and Retry-After", func(t *testing.T) {
		store.mu.Lock()
		store.gate, store.opened = make(chan struct{}), make(chan struct{}, 2)
		store.mu.Unlock()
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i := range codes {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				codes[i] = r.do(http.MethodGet, path, person, nil).Code
			}(i)
		}
		<-store.opened
		<-store.opened
		w := r.do(http.MethodGet, path, person, nil)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Equal(t, "5", w.Header().Get("Retry-After"))
		assert.Contains(t, w.Body.String(), "artifact_reads_busy")
		close(store.gate)
		wg.Wait()
		assert.Equal(t, []int{http.StatusOK, http.StatusOK}, codes)
		assert.Equal(t, http.StatusOK, r.do(http.MethodGet, path, person, nil).Code, "slots are given back")
	})

	t.Run("the store read stops when the client goes away", func(t *testing.T) {
		store.mu.Lock()
		store.gate, store.opened = make(chan struct{}), make(chan struct{}, 1)
		store.mu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+person)
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { r.router.ServeHTTP(w, req); close(done) }()
		<-store.opened
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the handler kept reading after the client went away")
		}
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "artifact_read_cancelled")
		store.mu.Lock()
		assert.Equal(t, 1, store.cancelled, "the read waiting on the store was stopped")
		store.mu.Unlock()
		close(store.gate)
	})
}
