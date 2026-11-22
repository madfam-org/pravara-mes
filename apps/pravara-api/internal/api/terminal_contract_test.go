package api

// The terminal contract (docs/contracts/terminal.v1.json, RFC 0064 §9) and
// its drift guard: the routes it lists are exactly the ones the terminal
// scope opens, and every shape it declares has exactly the JSON fields of
// the Go type Pravara answers with, with the same presence. A field added,
// removed or made optional on either side fails here.

import (
	"encoding"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

const terminalContractPath = "../../../../docs/contracts/terminal.v1.json"

type terminalContract struct {
	Contract string            `json:"contract"`
	Version  int               `json:"version"`
	About    string            `json:"about"`
	Audience string            `json:"audience"`
	Scopes   map[string]string `json:"scopes"`
	Reads    []struct {
		Method   string   `json:"method"`
		Path     string   `json:"path"`
		Query    []string `json:"query"`
		Response string   `json:"response"`
	} `json:"reads"`
	Shapes map[string]struct {
		Fields map[string]string `json:"fields"`
		Of     map[string]string `json:"of"`
	} `json:"shapes"`
	Writes []struct {
		Method    string            `json:"method"`
		Path      string            `json:"path"`
		Authority string            `json:"authority"`
		Request   string            `json:"request"`
		Responses map[string]string `json:"responses"`
	} `json:"writes"`
	Requests map[string]struct {
		Fields map[string]string `json:"fields"`
	} `json:"requests"`
}

func loadTerminalContract(t *testing.T) terminalContract {
	t.Helper()
	raw, err := os.ReadFile(terminalContractPath)
	require.NoError(t, err)
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var c terminalContract
	require.NoError(t, dec.Decode(&c), "the contract file has a key this test does not know")
	return c
}

// terminalShapeTypes maps every shape the contract declares to the Go value
// Pravara writes for it.
func terminalShapeTypes() map[string]any {
	return map[string]any{
		"terminal_order_list":    TerminalOrderList{},
		"terminal_order":         TerminalOrder{},
		"terminal_order_items":   TerminalOrderItemsResponse{},
		"terminal_order_item":    TerminalOrderItem{},
		"task_list":              TaskListResponse{},
		"task_board":             KanbanBoardResponse{},
		"task":                   types.Task{},
		"task_work_instructions": TaskWorkInstructionsResponse{},
		"task_work_instruction":  repositories.TaskWorkInstruction{},
		"lot":                    repositories.LotLookup{},
		"event_list":             EventListResponse{},
		"event_types":            EventTypesResponse{},
		"event_type_count":       repositories.EventTypeCount{},
		"event":                  repositories.OutboxEvent{},
		"task_moved":             TaskMoveResponse{},
		"task_status_conflict":   TaskStatusConflictResponse{},
		"task_board_busy":        TaskBoardBusyResponse{},
	}
}

// terminalRequestTypes maps every request body the contract declares to the
// Go type the route binds.
func terminalRequestTypes() map[string]any {
	return map[string]any{
		"task_move": MoveTaskRequest{},
	}
}

// requestFields returns a request type's JSON field names with "<presence>
// <type>": presence "required" (its binding tag says so) or "optional".
func requestFields(t reflect.Type) map[string]string {
	out := map[string]string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		presence := "optional"
		for _, rule := range strings.Split(f.Tag.Get("binding"), ",") {
			if rule == "required" {
				presence = "required"
			}
		}
		out[name] = presence + " " + jsonType(f.Type)
	}
	return out
}

// jsonFields returns a struct type's JSON field names with "<presence>
// <type>": presence "always" or "omitempty", type the JSON type (jsonType).
// It follows embedded structs the way encoding/json does.
func jsonFields(t reflect.Type) map[string]string {
	out := map[string]string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" || (!f.IsExported() && !f.Anonymous) {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			for k, v := range jsonFields(f.Type) {
				out[k] = v
			}
			continue
		}
		if name == "" {
			name = f.Name
		}
		presence := "always"
		for _, o := range strings.Split(opts, ",") {
			if o == "omitempty" {
				presence = "omitempty"
			}
		}
		out[name] = presence + " " + jsonType(f.Type)
	}
	return out
}

var (
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// jsonType is the JSON type encoding/json writes for t: string, integer,
// number, boolean, object, array, or any (raw JSON).
func jsonType(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == reflect.TypeOf(json.RawMessage{}):
		return "any"
	case t == reflect.TypeOf(time.Time{}):
		return "string"
	case t.Implements(jsonMarshalerType) || reflect.PointerTo(t).Implements(jsonMarshalerType):
		return "any"
	case t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType):
		return "string"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Map, reflect.Struct:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array"
	default:
		return "any"
	}
}

func TestTerminalContractNamesThisAPI(t *testing.T) {
	c := loadTerminalContract(t)
	assert.Equal(t, "pravara-mes.terminal", c.Contract)
	assert.Equal(t, 1, c.Version)
	assert.Equal(t, testAudience, c.Audience, "the audience a terminal's tokens must name")
	assert.Equal(t, map[string]string{"device": middleware.ScopeTerminal}, c.Scopes)
}

// The contract's reads are exactly the routes pravara-mes:terminal opens.
func TestTerminalContractReadsAreTheTerminalRoutes(t *testing.T) {
	c := loadTerminalContract(t)
	registered := registeredV1Routes(t)
	policy := MachineRouteScopes()

	var inContract []string
	for _, r := range c.Reads {
		key := middleware.RouteKey(r.Method, r.Path)
		inContract = append(inContract, key)
		assert.True(t, registered[key], "contract read %s is not a registered route", key)
		req, listed := policy[key]
		if assert.True(t, listed, "contract read %s is not in the machine matrix", key) {
			assert.Contains(t, req.AnyOf, middleware.ScopeTerminal, "contract read %s does not accept %s", key, middleware.ScopeTerminal)
		}
		_, ok := c.Shapes[r.Response]
		assert.True(t, ok, "contract read %s answers with undeclared shape %q", key, r.Response)
	}
	want := TerminalReadRoutes()
	sort.Strings(inContract)
	sort.Strings(want)
	assert.Equal(t, want, inContract, "the contract's reads and TerminalReadRoutes differ")
}

// Every declared shape has exactly the fields, and the presence, of the Go
// type Pravara answers with; its nested shapes are declared too.
func TestTerminalContractShapesMatchTheGoTypes(t *testing.T) {
	c := loadTerminalContract(t)
	goTypes := terminalShapeTypes()

	var declared, mapped []string
	for name := range c.Shapes {
		declared = append(declared, name)
	}
	for name := range goTypes {
		mapped = append(mapped, name)
	}
	sort.Strings(declared)
	sort.Strings(mapped)
	require.Equal(t, mapped, declared, "every contract shape needs its Go type in terminalShapeTypes, and only those")

	for name, shape := range c.Shapes {
		got := jsonFields(reflect.TypeOf(goTypes[name]))
		if !assert.Equal(t, got, shape.Fields, "shape %q drifted from its Go type", name) {
			blob, _ := json.Marshal(got)
			t.Logf("shape %q in Go: %s", name, blob)
		}
		for field, nested := range shape.Of {
			_, hasField := shape.Fields[field]
			assert.True(t, hasField, "shape %q: 'of' names field %q it does not have", name, field)
			_, declaredNested := c.Shapes[nested]
			assert.True(t, declaredNested, "shape %q: field %q holds undeclared shape %q", name, field, nested)
		}
	}
}

// Nested shapes whose element type reflection can see are held to it.
func TestTerminalContractNestedShapesMatchTheGoTypes(t *testing.T) {
	c := loadTerminalContract(t)
	goTypes := terminalShapeTypes()
	for name, shape := range c.Shapes {
		outer := reflect.TypeOf(goTypes[name])
		for field, nested := range shape.Of {
			sf, ok := fieldByJSONName(outer, field)
			if !ok {
				continue // reported by TestTerminalContractShapesMatchTheGoTypes
			}
			elem := sf.Type
			for elem.Kind() == reflect.Slice || elem.Kind() == reflect.Map || elem.Kind() == reflect.Pointer {
				elem = elem.Elem()
			}
			require.NotEqual(t, reflect.Interface, elem.Kind(), "shape %q: field %q must have a concrete element type", name, field)
			assert.Equal(t, reflect.TypeOf(goTypes[nested]), elem, "shape %q: field %q holds %s, the contract says %q", name, field, elem, nested)
		}
	}
}

// terminalReadHandlers names the handler of each contract read, so the
// query parameters the contract names can be checked against its source.
var terminalReadHandlers = map[string]struct{ file, recv, fn string }{
	"GET /v1/orders":                 {"order_handlers.go", "OrderHandler", "List"},
	"GET /v1/tasks":                  {"task_handlers.go", "TaskHandler", "List"},
	"GET /v1/quality/batches/lookup": {"quality_handlers.go", "QualityHandler", "LookupBatchLot"},
	"GET /v1/events":                 {"event_history_handlers.go", "EventHistoryHandler", "ListEvents"},
}

// queryParamsRead returns the string literals a handler passes as the name
// to c.Query, c.DefaultQuery, c.GetQuery or queryInt(c, ...).
func queryParamsRead(t *testing.T, file, recv, fn string) map[string]bool {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	require.NoError(t, err)
	out := map[string]bool{}
	for _, decl := range parsed.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != fn || fd.Recv == nil || len(fd.Recv.List) != 1 {
			continue
		}
		star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
		if !ok || star.X.(*ast.Ident).Name != recv {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var name string
			var arg int
			switch f := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = f.Sel.Name
			case *ast.Ident:
				name, arg = f.Name, 1
			}
			switch name {
			case "Query", "DefaultQuery", "GetQuery", "queryInt":
			default:
				return true
			}
			if name == "queryInt" && len(call.Args) < 2 {
				return true
			}
			if lit, ok := call.Args[arg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				v, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				out[v] = true
			}
			return true
		})
		return out
	}
	t.Fatalf("handler %s.%s not found in %s", recv, fn, file)
	return nil
}

// Every query parameter the contract names is one its route's handler reads.
func TestTerminalContractQueryParametersAreRead(t *testing.T) {
	c := loadTerminalContract(t)
	for _, r := range c.Reads {
		key := middleware.RouteKey(r.Method, r.Path)
		if len(r.Query) == 0 {
			continue
		}
		h, ok := terminalReadHandlers[key]
		if !assert.True(t, ok, "%s names query parameters; add its handler to terminalReadHandlers", key) {
			continue
		}
		read := queryParamsRead(t, h.file, h.recv, h.fn)
		for _, q := range r.Query {
			assert.True(t, read[q], "%s: the contract names query parameter %q, which %s.%s does not read", key, q, h.recv, h.fn)
		}
	}
}

func fieldByJSONName(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tagName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tagName == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// Every write is a registered route that no machine scope opens (PV-1); its
// request body matches the Go type the route binds, and its answers are
// declared shapes.
func TestTerminalContractWrites(t *testing.T) {
	c := loadTerminalContract(t)
	registered := registeredV1Routes(t)
	policy := MachineRouteScopes()
	requestTypes := terminalRequestTypes()

	used := map[string]bool{}
	for _, w := range c.Writes {
		key := middleware.RouteKey(w.Method, w.Path)
		assert.True(t, registered[key], "contract write %s is not a registered route", key)
		assert.Equal(t, "person", w.Authority, "%s: an operator's act carries a person's authority", key)
		_, listed := policy[key]
		assert.False(t, listed, "%s: no machine scope may open an operator's act (PV-1)", key)

		goType, ok := requestTypes[w.Request]
		if assert.True(t, ok, "%s: request %q has no Go type in terminalRequestTypes", key, w.Request) {
			declared, ok := c.Requests[w.Request]
			if assert.True(t, ok, "%s: request %q is not declared", key, w.Request) {
				assert.Equal(t, requestFields(reflect.TypeOf(goType)), declared.Fields, "request %q drifted from its Go type", w.Request)
			}
			used[w.Request] = true
		}
		assert.NotEmpty(t, w.Responses, key)
		for status, shape := range w.Responses {
			_, ok := c.Shapes[shape]
			assert.True(t, ok, "%s %s answers with undeclared shape %q", key, status, shape)
		}
	}
	for name := range c.Requests {
		assert.True(t, used[name], "request %q is declared but no write uses it", name)
	}
	assert.Len(t, requestTypes, len(c.Requests), "every Go request type in terminalRequestTypes is declared")
}
