package brokerauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

const goodPassword = "aZ3_9xQ-77kLmNoPqRsTuVwXyZ0123456789abcdEFG" // 43 chars, test only

type mapStore struct {
	creds map[string]*Credential
	err   error
}

func (m mapStore) LookupCredential(_ context.Context, u string) (*Credential, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.creds[u], nil
}

func newStore(t *testing.T) mapStore {
	t.Helper()
	h, err := HashPassword(goodPassword, bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	north := sparkplug.EdgeNodeUsername("acme", "site-north")
	south := sparkplug.EdgeNodeUsername("globex", "site-south")
	off := sparkplug.EdgeNodeUsername("acme", "site-off")
	return mapStore{creds: map[string]*Credential{
		north: {Username: north, Group: "acme", EdgeNodeID: "site-north", PasswordHash: h},
		south: {Username: south, Group: "globex", EdgeNodeID: "site-south", PasswordHash: h},
		off:   {Username: off, Group: "acme", EdgeNodeID: "site-off", PasswordHash: h, Disabled: true},
	}}
}

func TestValidatePassword(t *testing.T) {
	for _, bad := range []string{"", "short", strings.Repeat("a", 31), strings.Repeat("a", 73), strings.Repeat("a", 31) + " ", strings.Repeat("ñ", 32)} {
		if ValidatePassword(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := ValidatePassword(goodPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := HashPassword("short", bcrypt.MinCost); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password hashed: %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	a := &Authorizer{Store: newStore(t)}
	ctx := context.Background()
	north := sparkplug.EdgeNodeUsername("acme", "site-north")
	cases := []struct {
		user, pass string
		want       Decision
	}{
		{north, goodPassword, Allow},
		{north, goodPassword + "x", Deny},
		{north, "", Deny},
		{sparkplug.EdgeNodeUsername("acme", "site-off"), goodPassword, Deny},
		{"pravara-mes-host", "anything", Ignore},
		{"", goodPassword, Ignore},
	}
	for _, c := range cases {
		got, err := a.Authenticate(ctx, c.user, c.pass)
		if err != nil || got != c.want {
			t.Errorf("Authenticate(%q) = %v, %v; want %v", c.user, got, err, c.want)
		}
	}
}

func TestAuthorizeUsesTheCredentialsTenantNotTheTopics(t *testing.T) {
	a := &Authorizer{Store: newStore(t)}
	ctx := context.Background()
	north := sparkplug.EdgeNodeUsername("acme", "site-north")
	pub, sub := sparkplug.ACLPublish, sparkplug.ACLSubscribe
	cases := []struct {
		user   string
		action sparkplug.ACLAction
		topic  string
		want   Decision
	}{
		{north, pub, "spBv1.0/acme/NBIRTH/site-north", Allow},
		{north, pub, "spBv1.0/acme/DDATA/site-north/VORON-01", Allow},
		{north, sub, "spBv1.0/acme/DCMD/site-north/+", Allow},
		{north, sub, "spBv1.0/STATE/+", Allow},
		// cross-tenant: the topic names another tenant
		{north, pub, "spBv1.0/globex/NBIRTH/site-north", Deny},
		{north, pub, "spBv1.0/globex/DDATA/site-south/X", Deny},
		{north, sub, "spBv1.0/globex/DCMD/site-south/+", Deny},
		{north, sub, "spBv1.0/#", Deny},
		{north, pub, "spBv1.0/STATE/pravara-mes", Deny},
		{north, "all", "spBv1.0/acme/NBIRTH/site-north", Deny},
		{sparkplug.EdgeNodeUsername("acme", "site-off"), pub, "spBv1.0/acme/NBIRTH/site-off", Deny},
		{"pravara-mes-host", sub, "spBv1.0/#", Ignore},
	}
	for _, c := range cases {
		got, err := a.Authorize(ctx, c.user, c.action, c.topic)
		if err != nil || got != c.want {
			t.Errorf("Authorize(%q, %s, %q) = %v, %v; want %v", c.user, c.action, c.topic, got, err, c.want)
		}
	}
}

func post(t *testing.T, h http.HandlerFunc, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	if key != "" {
		req.Header.Set(InternalKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestEMQXHandlers(t *testing.T) {
	h := &Handlers{Authorizer: &Authorizer{Store: newStore(t)}, InternalKey: "internal-test-key"}
	north := sparkplug.EdgeNodeUsername("acme", "site-north")

	rec := post(t, h.Auth, "internal-test-key", map[string]string{"username": north, "password": goodPassword, "clientid": "c"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"result":"allow"`) || !strings.Contains(rec.Body.String(), `"is_superuser":false`) {
		t.Fatalf("auth: %d %s", rec.Code, rec.Body)
	}
	rec = post(t, h.ACL, "internal-test-key", map[string]string{"username": north, "action": "publish", "topic": "spBv1.0/globex/NDATA/site-north"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"result":"deny"`) {
		t.Fatalf("acl: %d %s", rec.Code, rec.Body)
	}
	if rec := post(t, h.Auth, "wrong", map[string]string{"username": north, "password": goodPassword}); rec.Code != 401 {
		t.Fatalf("bad key: %d", rec.Code)
	}
	if rec := post(t, h.ACL, "", map[string]string{"username": north}); rec.Code != 401 {
		t.Fatalf("missing key: %d", rec.Code)
	}
	unset := &Handlers{Authorizer: h.Authorizer}
	if rec := post(t, unset.Auth, "", map[string]string{"username": north, "password": goodPassword}); rec.Code != 503 {
		t.Fatalf("unconfigured key must refuse: %d", rec.Code)
	}
	failing := &Handlers{Authorizer: &Authorizer{Store: mapStore{err: errors.New("db down")}}, InternalKey: "k"}
	if rec := post(t, failing.Auth, "k", map[string]string{"username": north, "password": goodPassword}); rec.Code != 500 {
		t.Fatalf("lookup failure must not allow: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(InternalKeyHeader, "internal-test-key")
	rec = httptest.NewRecorder()
	h.Auth(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
}
