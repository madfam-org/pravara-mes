// Package mcp is the native Model Context Protocol endpoint of pravara-api
// (RFC 0051): Streamable HTTP, JSON-RPC 2.0, stateless, JSON responses only.
//
// The package knows the protocol, the gates and the audit line; the tools
// themselves live in internal/api (mcp_tools.go), where they call the same
// repositories as the HTTP routes they cover. Authentication and the tenant
// transaction come from the same middleware as the HTTP API
// (middleware.AuthMiddleware), mounted in front of Server.Handle.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// ToolNamePattern is the tool-name format every client accepts: Claude
// (Anthropic tool use) rejects any other character, a dot included.
const ToolNamePattern = `^[a-zA-Z0-9_-]{1,64}$`

var toolNameRE = regexp.MustCompile(ToolNamePattern)

// ValidToolName reports whether name matches ToolNamePattern.
func ValidToolName(name string) bool { return toolNameRE.MatchString(name) }

// Tool is one MCP tool. Every tool is read-only (RFC 0051 Phase A).
type Tool struct {
	Name        string
	Title       string
	Description string
	// InputSchema and OutputSchema are JSON Schema objects. The arguments
	// must be an object; Call validates them (ArgumentError on failure).
	InputSchema  map[string]any
	OutputSchema map[string]any
	// Call runs the tool. It returns the structured result, an
	// *ArgumentError for arguments the caller can fix, or any other error
	// for a failure the caller cannot fix (answered as an internal error).
	Call func(ctx context.Context, req Request) (any, error)
}

// Request is one tools/call.
type Request struct {
	// Caller is the authenticated Janua machine client.
	Caller middleware.Caller
	// TenantID is the tenant from the token; ctx already carries its
	// tenant-scoped transaction.
	TenantID string
	// Args are the decoded arguments (numbers as json.Number).
	Args map[string]any
}

// ArgumentError is an argument the caller can fix. It is answered as a tool
// result with isError true, so the model can correct the call.
type ArgumentError struct {
	Message string
}

func (e *ArgumentError) Error() string { return e.Message }

// Invalid builds an *ArgumentError.
func Invalid(format string, a ...any) error {
	return &ArgumentError{Message: fmt.Sprintf(format, a...)}
}

// OnlyArgs fails when args holds a name outside allowed (the input schemas
// set additionalProperties false).
func (r Request) OnlyArgs(allowed ...string) error {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	var unknown []string
	for k := range r.Args {
		if !ok[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return Invalid("unknown argument(s): %s (allowed: %s)", strings.Join(unknown, ", "), strings.Join(allowed, ", "))
	}
	return nil
}

// String returns a string argument: ("", false, nil) when absent or null.
func (r Request) String(name string) (string, bool, error) {
	v, present := r.Args[name]
	if !present || v == nil {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", false, Invalid("%s must be a string", name)
	}
	return s, true, nil
}

// Integer returns an integer argument in decimal form, for parsers shared
// with the HTTP query string: ("", false, nil) when absent or null.
func (r Request) Integer(name string) (string, bool, error) {
	v, present := r.Args[name]
	if !present || v == nil {
		return "", false, nil
	}
	n, ok := v.(json.Number)
	if !ok {
		return "", false, Invalid("%s must be an integer", name)
	}
	i, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		return "", false, Invalid("%s must be an integer", name)
	}
	return strconv.FormatInt(i, 10), true, nil
}

// ReadOnlyAnnotations are the MCP tool annotations of every Phase A tool.
func ReadOnlyAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
}
