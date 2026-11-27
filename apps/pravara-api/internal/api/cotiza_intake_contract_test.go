package api

// The Cotiza order-intake contract (docs/contracts/cotiza-order-intake.v1.json,
// ruling C7 of the 2026-11-14 Pravara rulings) and its drift guard: the route
// and scope it names are the ones Pravara serves, its request shapes are the
// Go types the webhook binds, its events are the ones the handler switches
// on, and its example, signed as Cotiza signs, creates exactly one order and
// is a no-op when replayed. Cotiza keeps a verbatim copy and tests its sender
// against it, so a change on one side without the other fails a test instead
// of dropping accepted quotes.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
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

const cotizaIntakeContractPath = "../../../../docs/contracts/cotiza-order-intake.v1.json"

type cotizaIntakeContract struct {
	Contract string `json:"contract"`
	Version  int    `json:"version"`
	About    string `json:"about"`
	Route    struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	} `json:"route"`
	Audience string `json:"audience"`
	Scope    string `json:"scope"`
	Auth     struct {
		Bearer          string `json:"bearer"`
		SignatureHeader string `json:"signature_header"`
		Signature       string `json:"signature"`
	} `json:"auth"`
	Idempotency string            `json:"idempotency"`
	Events      map[string]string `json:"events"`
	Requests    map[string]struct {
		Fields map[string]string `json:"fields"`
		Of     map[string]string `json:"of"`
	} `json:"requests"`
	Responses map[string]struct {
		When   string            `json:"when"`
		Fields map[string]string `json:"fields"`
		Notes  string            `json:"notes"`
	} `json:"responses"`
	Retry   string          `json:"retry"`
	Sender  string          `json:"sender"`
	Example json.RawMessage `json:"example"`
}

func loadCotizaIntakeContract(t *testing.T) cotizaIntakeContract {
	t.Helper()
	raw, err := os.ReadFile(cotizaIntakeContractPath)
	require.NoError(t, err)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c cotizaIntakeContract
	require.NoError(t, dec.Decode(&c), "the contract file has a key this test does not know")
	return c
}

// cotizaIntakeRequestTypes maps every request shape to the Go type bound.
func cotizaIntakeRequestTypes() map[string]any {
	return map[string]any{
		"envelope": CotizaWebhookPayload{},
		"order":    CotizaOrderData{},
		"item":     CotizaItemData{},
	}
}

func TestCotizaIntakeContractNamesThisRoute(t *testing.T) {
	c := loadCotizaIntakeContract(t)
	assert.Equal(t, "pravara-mes.cotiza-order-intake", c.Contract)
	assert.Equal(t, 1, c.Version)
	assert.Equal(t, testAudience, c.Audience, "the audience Cotiza's machine token must name")
	assert.Equal(t, middleware.ScopeJobs, c.Scope)
	assert.Equal(t, "X-Cotiza-Signature", c.Auth.SignatureHeader)

	key := middleware.RouteKey(c.Route.Method, c.Route.Path)
	assert.True(t, registeredV1Routes(t)[key], "contract route %s is not a registered route", key)
	req, listed := MachineRouteScopes()[key]
	if assert.True(t, listed, "contract route %s is not in the machine matrix", key) {
		assert.Contains(t, req.AnyOf, c.Scope, "contract route %s does not accept %s", key, c.Scope)
	}
}

// Every request shape has exactly the fields, presence and JSON types of the
// Go type the webhook binds, and nested shapes are the nested Go types.
func TestCotizaIntakeContractRequestsMatchTheGoTypes(t *testing.T) {
	c := loadCotizaIntakeContract(t)
	goTypes := cotizaIntakeRequestTypes()

	var declared, mapped []string
	for name := range c.Requests {
		declared = append(declared, name)
	}
	for name := range goTypes {
		mapped = append(mapped, name)
	}
	sort.Strings(declared)
	sort.Strings(mapped)
	require.Equal(t, mapped, declared, "every request shape needs its Go type in cotizaIntakeRequestTypes, and only those")

	for name, shape := range c.Requests {
		outer := reflect.TypeOf(goTypes[name])
		assert.Equal(t, requestFields(outer), shape.Fields, "request shape %q drifted from %s", name, outer)
		for field, nested := range shape.Of {
			sf, ok := fieldByJSONName(outer, field)
			if !assert.True(t, ok, "shape %q: 'of' names field %q it does not have", name, field) {
				continue
			}
			elem := sf.Type
			for elem.Kind() == reflect.Slice || elem.Kind() == reflect.Pointer {
				elem = elem.Elem()
			}
			assert.Equal(t, reflect.TypeOf(goTypes[nested]), elem, "shape %q: field %q holds %s, the contract says %q", name, field, elem, nested)
		}
	}
}

// The contract's events are exactly the cases CotizaWebhook switches on.
func TestCotizaIntakeContractEventsAreTheHandledOnes(t *testing.T) {
	c := loadCotizaIntakeContract(t)
	parsed, err := parser.ParseFile(token.NewFileSet(), "webhook_handlers.go", nil, 0)
	require.NoError(t, err)
	handled := map[string]bool{}
	for _, decl := range parsed.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "CotizaWebhook" {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					v, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					handled[v] = true
				}
			}
			return true
		})
	}
	require.NotEmpty(t, handled, "CotizaWebhook switch not found")
	declared := map[string]bool{}
	for e := range c.Events {
		declared[e] = true
	}
	assert.Equal(t, handled, declared)
}

// The example is a body Pravara knows field for field.
func TestCotizaIntakeContractExampleIsStrict(t *testing.T) {
	c := loadCotizaIntakeContract(t)
	dec := json.NewDecoder(bytes.NewReader(c.Example))
	dec.DisallowUnknownFields()
	var p CotizaWebhookPayload
	require.NoError(t, dec.Decode(&p), "the example carries a field the webhook does not bind")
	assert.Equal(t, "order.confirmed", p.Event, "Cotiza sends order.confirmed")
	require.NotEmpty(t, p.Order.Items)
}

type cotizaIntakeRig struct {
	router   *gin.Engine
	mock     sqlmock.Sqlmock
	tenantID uuid.UUID
	secret   string
}

func newCotizaIntakeRig(t *testing.T, path string) *cotizaIntakeRig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	rig := &cotizaIntakeRig{mock: mock, tenantID: uuid.New(), secret: "contract-test-secret"}
	h := NewWebhookHandler(repositories.NewOrderRepository(db), repositories.NewOrderItemRepository(db), log, rig.secret)
	rig.router = gin.New()
	rig.router.POST(path, func(c *gin.Context) {
		// What the auth middleware sets from the machine token.
		c.Set(string(middleware.ContextKeyTenantID), rig.tenantID.String())
		c.Next()
	}, h.CotizaWebhook)
	return rig
}

func (r *cotizaIntakeRig) post(path, header string, body []byte, sign bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sign {
		mac := hmac.New(sha256.New, []byte(r.secret))
		mac.Write(body)
		req.Header.Set(header, hex.EncodeToString(mac.Sum(nil)))
	}
	w := httptest.NewRecorder()
	r.router.ServeHTTP(w, req)
	return w
}

var orderColumns = []string{
	"id", "tenant_id", "external_id", "customer_name", "customer_email",
	"status", "priority", "due_date", "total_amount", "currency",
	"shipping_address", "metadata", "created_at", "updated_at", "engagement_slug",
}

func responseKeys(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	out := map[string]string{}
	for k, v := range got {
		_, isString := v.(string)
		require.True(t, isString, "response field %q is not a string", k)
		out[k] = "always string"
	}
	return out
}

// The example, signed as Cotiza signs it, creates one order with one item
// per example item and answers with the contract's 200 shape.
func TestCotizaIntakeContractExampleCreatesOneOrder(t *testing.T) {
	c := loadCotizaIntakeContract(t)
	var p CotizaWebhookPayload
	require.NoError(t, json.Unmarshal(c.Example, &p))
	rig := newCotizaIntakeRig(t, c.Route.Path)
	now := time.Now()

	rig.mock.ExpectQuery("SELECT id, tenant_id.*FROM orders.*WHERE external_id").
		WithArgs(p.Order.ID).
		WillReturnRows(sqlmock.NewRows(orderColumns))
	rig.mock.ExpectQuery("INSERT INTO orders").
		WithArgs(sqlmock.AnyArg(), rig.tenantID, p.Order.ID, p.Order.CustomerName,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			p.Order.TotalAmount, p.Order.Currency, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))
	for _, item := range p.Order.Items {
		rig.mock.ExpectQuery("INSERT INTO order_items").
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), item.ProductName, sqlmock.AnyArg(),
				item.Quantity, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	}

	w := rig.post(c.Route.Path, c.Auth.SignatureHeader, c.Example, true)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, c.Responses["200"].Fields, responseKeys(t, w.Body.Bytes()))
	assert.Contains(t, w.Body.String(), `"order_id":"`+p.Order.ID+`"`, "order_id echoes the external id")
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

// A replay of the same body finds the order by its external id and writes
// nothing: the idempotency the contract promises.
func TestCotizaIntakeContractReplayWritesNothing(t *testing.T) {
	c := loadCotizaIntakeContract(t)
	var p CotizaWebhookPayload
	require.NoError(t, json.Unmarshal(c.Example, &p))
	rig := newCotizaIntakeRig(t, c.Route.Path)
	now := time.Now()

	rig.mock.ExpectQuery("SELECT id, tenant_id.*FROM orders.*WHERE external_id").
		WithArgs(p.Order.ID).
		WillReturnRows(sqlmock.NewRows(orderColumns).AddRow(
			uuid.New(), rig.tenantID, p.Order.ID, p.Order.CustomerName, nil,
			"received", 5, nil, p.Order.TotalAmount, p.Order.Currency, nil, []byte("{}"), now, now, nil,
		))

	w := rig.post(c.Route.Path, c.Auth.SignatureHeader, c.Example, true)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, c.Responses["200"].Fields, responseKeys(t, w.Body.Bytes()))
	assert.NoError(t, rig.mock.ExpectationsWereMet(), "a replay must not write")
}

// With the secret set, an unsigned body or a wrong signature is refused with
// the contract's 401 shape, before any database read.
func TestCotizaIntakeContractRefusesUnsignedBodies(t *testing.T) {
	c := loadCotizaIntakeContract(t)
	rig := newCotizaIntakeRig(t, c.Route.Path)

	w := rig.post(c.Route.Path, c.Auth.SignatureHeader, c.Example, false)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, c.Responses["401"].Fields, responseKeys(t, w.Body.Bytes()))

	tampered := []byte(strings.Replace(string(c.Example), "Example Customer", "Someone Else", 1))
	req := httptest.NewRequest(http.MethodPost, c.Route.Path, bytes.NewReader(tampered))
	mac := hmac.New(sha256.New, []byte(rig.secret))
	mac.Write(c.Example)
	req.Header.Set(c.Auth.SignatureHeader, hex.EncodeToString(mac.Sum(nil)))
	w = httptest.NewRecorder()
	rig.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}
