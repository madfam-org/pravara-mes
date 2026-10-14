package api_test

// Edge enrollment, broker authentication/ACL and Sparkplug machine binding on
// a real PostgreSQL as the application role (isolation rig: NOSUPERUSER,
// NOBYPASSRLS, FORCE RLS, two tenants). Skipped without
// PRAVARA_ISOLATION_DB_URL and in -short mode.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/api"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

const (
	boxPasswordOne = "Box1-generated-credential-0123456789abcdefgh" // test values only
	boxPasswordTwo = "Box2-generated-credential-9876543210zyxwvuts"
	internalKey    = "internal-test-key"
)

func decode(t *testing.T, res rigResponse) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoErrorf(t, json.Unmarshal(res.Body, &out), "%s", res.Body)
	return out
}

func (r *isolationRig) enroll(group, edge, password string) rigResponse {
	return r.doWith(nil, http.MethodPost, "/v1/edge/enrollments",
		map[string]string{"group_id": group, "edge_node_id": edge, "password": password})
}

// broker calls the internal router like EMQX does.
func broker(t *testing.T, internal http.Handler, path, key string, body map[string]string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set("X-Pravara-Internal-Key", key)
	w := httptest.NewRecorder()
	internal.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return w.Code, ""
	}
	var out struct {
		Result string `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return w.Code, out.Result
}

func TestEdgeEnrollmentAndBrokerAuthAgainstPostgres(t *testing.T) {
	r := newIsolationRig(t)
	log := logrus.New()
	log.SetOutput(io.Discard)
	cfg := &config.Config{}
	cfg.Edge.MQTTAuthInternalKey = internalKey
	internal := api.NewInternalRouter(&db.DB{DB: r.pool}, cfg, log)
	username := sparkplug.EdgeNodeUsername(r.A.Slug, "site-north")

	// Bad requests.
	require.Equal(t, http.StatusBadRequest, r.enroll(r.A.Slug, "site-north", "too-short").Code)
	require.Equal(t, http.StatusBadRequest, r.enroll(r.A.Slug, "site/north", boxPasswordOne).Code)
	require.Equal(t, http.StatusNotFound, r.enroll("no-such-group", "site-north", boxPasswordOne).Code)

	// The box enrolls with its own credential; nothing is active yet.
	res := r.enroll(r.A.Slug, "site-north", boxPasswordOne)
	require.Equalf(t, http.StatusCreated, res.Code, "%s", res.Body)
	created := decode(t, res)
	code, enrollmentID := created["user_code"].(string), created["enrollment_id"].(string)
	require.Regexp(t, `^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`, code)
	require.Equal(t, username, created["mqtt_username"])
	require.NotContains(t, string(res.Body), boxPasswordOne)
	poll := decode(t, r.doWith(nil, http.MethodGet, "/v1/edge/enrollments/"+enrollmentID, nil))
	require.Equal(t, "pending", poll["status"])
	status, result := broker(t, internal, "/v1/mqtt/auth", internalKey, map[string]string{"username": username, "password": boxPasswordOne})
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "ignore", result, "a pending enrollment must not authenticate")

	// Only the enrolling tenant's admin, a person, with the right edge node.
	require.Equal(t, http.StatusNotFound, r.do(r.B, http.MethodPost, "/v1/edge/enrollments/approve",
		map[string]string{"user_code": code, "edge_node_id": "site-north"}).Code)
	require.Equal(t, http.StatusConflict, r.do(r.A, http.MethodPost, "/v1/edge/enrollments/approve",
		map[string]string{"user_code": code, "edge_node_id": "site-south"}).Code)
	raw := "prv_" + strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(raw))
	r.seed(r.A.ID, `INSERT INTO api_keys (tenant_id, name, key_hash, key_prefix, scopes) VALUES ($1, 'wildcard', $2, $3, $4)`,
		r.A.ID, fmt.Sprintf("%x", sum), raw[:12], pq.Array([]string{"*"}))
	wildcard := map[string]string{"X-API-Key": raw}
	res = r.doWith(wildcard, http.MethodPost, "/v1/edge/enrollments/approve", map[string]string{"user_code": code, "edge_node_id": "site-north"})
	require.Equalf(t, http.StatusForbidden, res.Code, "%s", res.Body)
	require.Contains(t, string(res.Body), "human_only")
	require.Equal(t, http.StatusOK, r.doWith(wildcard, http.MethodGet, "/v1/edge/nodes", nil).Code, "reads stay available to machine callers")

	lower := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	res = r.do(r.A, http.MethodPost, "/v1/edge/enrollments/approve", map[string]string{"user_code": lower, "edge_node_id": "site-north"})
	require.Equalf(t, http.StatusOK, res.Code, "%s", res.Body)
	require.NotContains(t, string(res.Body), "password")
	poll = decode(t, r.doWith(nil, http.MethodGet, "/v1/edge/enrollments/"+enrollmentID, nil))
	require.Equal(t, "approved", poll["status"])

	// Only a bcrypt hash is stored; the enrollment no longer holds it.
	hash := r.scalar(r.A.ID, `SELECT password_hash FROM edge_nodes WHERE mqtt_username = $1`, username)
	require.True(t, strings.HasPrefix(hash, "$2"), "bcrypt hash expected")
	require.NotContains(t, hash, boxPasswordOne)
	require.Equal(t, "", r.scalar(r.A.ID, `SELECT password_hash FROM edge_enrollments WHERE id = $1`, enrollmentID))
	require.Equal(t, "0", r.scalar(r.B.ID, `SELECT count(*)::text FROM edge_nodes`), "tenant B sees no edge node of A")

	// Broker authentication and ACL: the tenant comes from the credential.
	auth := func(user, pw string) string {
		_, res := broker(t, internal, "/v1/mqtt/auth", internalKey, map[string]string{"username": user, "password": pw, "clientid": "c"})
		return res
	}
	acl := func(action, topic string) string {
		_, res := broker(t, internal, "/v1/mqtt/acl", internalKey, map[string]string{"username": username, "action": action, "topic": topic})
		return res
	}
	require.Equal(t, "allow", auth(username, boxPasswordOne))
	require.Equal(t, "deny", auth(username, boxPasswordTwo))
	require.Equal(t, "ignore", auth("pravara-mes-host", "whatever"))
	status, _ = broker(t, internal, "/v1/mqtt/auth", "wrong-key", map[string]string{"username": username, "password": boxPasswordOne})
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "allow", acl("publish", "spBv1.0/"+r.A.Slug+"/DDATA/site-north/VORON-01"))
	require.Equal(t, "allow", acl("subscribe", "spBv1.0/"+r.A.Slug+"/DCMD/site-north/+"))
	require.Equal(t, "deny", acl("publish", "spBv1.0/"+r.B.Slug+"/DDATA/site-north/VORON-01"))
	require.Equal(t, "deny", acl("subscribe", "spBv1.0/"+r.B.Slug+"/DCMD/site-north/+"))
	require.Equal(t, "deny", acl("subscribe", "spBv1.0/#"))

	// Pre-registration: attach a machine of A to the edge node.
	machineID := uuid.New()
	r.seed(r.A.ID, `INSERT INTO machines (id, tenant_id, name, code, type) VALUES ($1, $2, 'Voron', 'VORON-01', '3d_printer')`, machineID, r.A.ID)
	path := "/v1/machines/" + machineID.String()
	require.Equal(t, http.StatusNotFound, r.do(r.B, http.MethodPut, path+"/sparkplug", map[string]any{"edge_node_id": "site-north"}).Code)
	require.Equal(t, http.StatusBadRequest, r.do(r.A, http.MethodPut, path+"/sparkplug", map[string]any{"edge_node_id": "site/north"}).Code)
	res = r.do(r.A, http.MethodPut, path+"/sparkplug", map[string]any{"edge_node_id": "site-north"})
	require.Equalf(t, http.StatusOK, res.Code, "%s", res.Body)
	live := decode(t, r.do(r.A, http.MethodGet, path+"/live-state", nil))["data"].(map[string]any)
	require.Equal(t, "site-north", live["sparkplug_edge_id"])
	require.Equal(t, false, live["reported"])
	list := decode(t, r.do(r.A, http.MethodGet, "/v1/edge/live-state", nil))["data"].([]any)
	require.Len(t, list, 1)
	require.Equal(t, http.StatusNotFound, r.do(r.B, http.MethodGet, path+"/live-state", nil).Code)

	// Re-enrollment rotates the credential once a person approves it.
	res = r.enroll(r.A.Slug, "site-north", boxPasswordTwo)
	require.Equal(t, http.StatusCreated, res.Code)
	code2 := decode(t, res)["user_code"].(string)
	require.Equal(t, "allow", auth(username, boxPasswordOne), "the old credential stays valid until approval")
	require.Equal(t, http.StatusOK, r.do(r.A, http.MethodPost, "/v1/edge/enrollments/approve",
		map[string]string{"user_code": code2, "edge_node_id": "site-north"}).Code)
	require.Equal(t, "deny", auth(username, boxPasswordOne))
	require.Equal(t, "allow", auth(username, boxPasswordTwo))

	// Revocation: people only; then the broker denies everything.
	nodes := decode(t, r.do(r.A, http.MethodGet, "/v1/edge/nodes", nil))["data"].([]any)
	require.Len(t, nodes, 1)
	nodeID := nodes[0].(map[string]any)["id"].(string)
	require.Equal(t, http.StatusForbidden, r.doWith(wildcard, http.MethodPost, "/v1/edge/nodes/"+nodeID+"/disable", nil).Code)
	require.Equal(t, http.StatusNotFound, r.do(r.B, http.MethodPost, "/v1/edge/nodes/"+nodeID+"/disable", nil).Code)
	require.Equal(t, http.StatusOK, r.do(r.A, http.MethodPost, "/v1/edge/nodes/"+nodeID+"/disable", nil).Code)
	require.Equal(t, "deny", auth(username, boxPasswordTwo))
	require.Equal(t, "deny", acl("publish", "spBv1.0/"+r.A.Slug+"/DDATA/site-north/VORON-01"))

	// Unauthenticated callers reach none of the registry reads.
	require.Equal(t, http.StatusUnauthorized, r.doWith(nil, http.MethodGet, "/v1/edge/nodes", nil).Code)
}

// scalar reads one value in tenantID's scope as the application role.
func (r *isolationRig) scalar(tenantID uuid.UUID, query string, args ...any) string {
	r.t.Helper()
	var v string
	err := db.RunInTenantTx(context.Background(), r.pool, tenantID.String(), func(ctx context.Context) error {
		return r.tdb.QueryRowContext(ctx, query, args...).Scan(&v)
	})
	require.NoError(r.t, err, query)
	return v
}
