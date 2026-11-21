package api

// The terminal contract (docs/contracts/terminal.v1.json, RFC 0064 §9) and
// its drift guard: the routes it lists are exactly the ones the terminal
// scope opens, and every shape it declares has exactly the JSON fields of
// the Go type Pravara answers with, with the same presence. A field added,
// removed or made optional on either side fails here.

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

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
	Writes []json.RawMessage `json:"writes"`
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
		"order_list":             ListResponse{},
		"order":                  types.Order{},
		"order_items":            OrderItemsResponse{},
		"order_item":             types.OrderItem{},
		"task_list":              ListResponse{},
		"task_board":             KanbanBoardResponse{},
		"task":                   types.Task{},
		"task_work_instructions": TaskWorkInstructionsResponse{},
		"task_work_instruction":  repositories.TaskWorkInstruction{},
		"lot":                    repositories.LotLookup{},
		"event_list":             EventListResponse{},
		"event_types":            EventTypesResponse{},
		"event_type_count":       repositories.EventTypeCount{},
		"event":                  repositories.OutboxEvent{},
	}
}

// jsonFields returns a struct type's JSON field names with "always" or
// "omitempty", following embedded structs the way encoding/json does.
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
		out[name] = presence
	}
	return out
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
			if elem.Kind() == reflect.Interface {
				continue // ListResponse.Data: the handler decides; see the route tests
			}
			assert.Equal(t, reflect.TypeOf(goTypes[nested]), elem, "shape %q: field %q holds %s, the contract says %q", name, field, elem, nested)
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

// Writes join the contract with the pull request that builds them, each
// with its own test.
func TestTerminalContractHasNoUntestedWrites(t *testing.T) {
	assert.Empty(t, loadTerminalContract(t).Writes)
}
