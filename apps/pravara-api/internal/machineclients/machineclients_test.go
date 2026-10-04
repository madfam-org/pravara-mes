package machineclients

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeJanua serves POST /api/v1/oauth/token like Janua's client_credentials
// grant: HTTP Basic client authentication, form body, scope echo.
type fakeJanua struct {
	calls     atomic.Int32
	expiresIn int
	status    int
	grant     string // granted scope override
}

func (f *fakeJanua) handler(t *testing.T, wantID, wantSecret, wantScope string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := f.calls.Add(1)
		assert.Equal(t, "/api/v1/oauth/token", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		id, secret, ok := r.BasicAuth()
		require.True(t, ok)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "client_credentials", r.PostForm.Get("grant_type"))
		assert.Equal(t, wantScope, r.PostForm.Get("scope"))
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"detail":"invalid_client: Unknown client"}`))
			return
		}
		if id != wantID || secret != wantSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		scope := wantScope
		if f.grant != "" {
			scope = f.grant
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-" + string(rune('0'+n)), "token_type": "Bearer", "expires_in": f.expiresIn, "scope": scope,
		})
	}
}

func newSource(t *testing.T, f *fakeJanua, scope string) (*TokenSource, *httptest.Server) {
	srv := httptest.NewServer(f.handler(t, "jnc_placeholder", "jns_placeholder:with:colons", scope))
	t.Cleanup(srv.Close)
	ts := NewTokenSource(Credentials{Name: "test", TokenURL: srv.URL + "/api/v1/oauth/token",
		ClientID: "jnc_placeholder", ClientSecret: "jns_placeholder:with:colons", Scope: scope}, nil)
	return ts, srv
}

func TestTokenIsCachedAndRefreshedEarly(t *testing.T) {
	f := &fakeJanua{expiresIn: 3600}
	ts, _ := newSource(t, f, "yantra4d:render")
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	ts.now = func() time.Time { return now }

	tok, err := ts.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "token-1", tok)
	_, _ = ts.Token(context.Background())
	assert.EqualValues(t, 1, f.calls.Load(), "second call is served from the cache")

	// 3600 s lifetime → refreshed 5 min (the cap) before expiry.
	now = now.Add(54*time.Minute + 59*time.Second)
	tok, _ = ts.Token(context.Background())
	assert.Equal(t, "token-1", tok)
	now = now.Add(2 * time.Second)
	tok, _ = ts.Token(context.Background())
	assert.Equal(t, "token-2", tok)
	assert.EqualValues(t, 2, f.calls.Load())
}

func TestRefreshMarginBounds(t *testing.T) {
	assert.Equal(t, 30*time.Second, refreshMargin(60*time.Second))
	assert.Equal(t, 2*time.Minute, refreshMargin(10*time.Minute))
	assert.Equal(t, 5*time.Minute, refreshMargin(time.Hour))
}

func TestConcurrentCallersShareOneTokenRequest(t *testing.T) {
	f := &fakeJanua{expiresIn: 3600}
	ts, _ := newSource(t, f, "fabrication-prep:slice")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ts.Token(context.Background())
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, f.calls.Load())
}

func TestRejectedCredentialsAreNotRetryableAndNeverLeakTheSecret(t *testing.T) {
	f := &fakeJanua{status: http.StatusUnauthorized}
	ts, _ := newSource(t, f, "asset-shells:read")
	_, err := ts.Token(context.Background())
	var te *TokenError
	require.True(t, errors.As(err, &te))
	assert.False(t, te.Retryable())
	assert.Contains(t, err.Error(), "401")
	assert.NotContains(t, err.Error(), "jns_placeholder")
	assert.NotContains(t, err.Error(), "jnc_placeholder")

	f.status = http.StatusServiceUnavailable
	_, err = ts.Token(context.Background())
	require.True(t, errors.As(err, &te))
	assert.True(t, te.Retryable())
}

func TestGrantedScopeMustCoverRequestedScope(t *testing.T) {
	f := &fakeJanua{expiresIn: 3600, grant: "asset-shells:read"}
	ts, _ := newSource(t, f, AssetShellsScope)
	_, err := ts.Token(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "asset-shells:publish-instances")
}

func TestNotConfigured(t *testing.T) {
	ts := NewTokenSource(Credentials{Name: "empty", TokenURL: "https://auth.example.test/token"}, nil)
	_, err := ts.Token(context.Background())
	assert.ErrorIs(t, err, ErrNotConfigured)
	assert.False(t, ts.Configured())
}

func TestDoRetriesOnceAfter401WithAFreshToken(t *testing.T) {
	f := &fakeJanua{expiresIn: 3600}
	ts, _ := newSource(t, f, "yantra4d:render")
	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if len(seen) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer api.Close()
	resp, err := Do(context.Background(), api.Client(), ts, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, api.URL+"/api/render", strings.NewReader("{}"))
	})
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []string{"Bearer token-1", "Bearer token-2"}, seen)
}

func TestSetMapsTenantToOrgBoundPublisher(t *testing.T) {
	env := map[string]string{
		"ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM_CLIENT_ID":     "jnc_placeholder",
		"ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM_CLIENT_SECRET": "jns_placeholder",
	}
	s := NewSet(SetConfig{TokenURL: "https://auth.example.test/api/v1/oauth/token",
		Yantra4DClientID: "jnc_y", Yantra4DClientSecret: "jns_y",
		AssetShellsTenantPrefixes: map[string]string{"madfam": "ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM", "other": "ASSET_SHELLS_PUBLISHER_OTHER"},
		Getenv:                    func(k string) string { return env[k] }}, nil)
	ts, err := s.AssetShellsPublisher("madfam")
	require.NoError(t, err)
	assert.Equal(t, "asset-shells-publisher/madfam", ts.Name())
	_, err = s.AssetShellsPublisher("other")
	assert.ErrorIs(t, err, ErrNotConfigured)
	_, err = s.AssetShellsPublisher("unknown")
	assert.ErrorIs(t, err, ErrTenantNotMapped)
	assert.Equal(t, []string{"asset-shells-publisher/other", "fabrication-prep"}, s.Missing())
	for name := range s.Status() {
		assert.NotContains(t, name, "jnc_")
	}
}
