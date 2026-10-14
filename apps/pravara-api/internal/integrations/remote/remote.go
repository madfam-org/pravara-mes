// Package remote holds the error type and JSON helpers shared by the
// clients pravara uses to call other MADFAM services during dispatch
// (yantra4d, fabrication-prep, asset-shells).
package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

// MaxBody bounds every response body pravara reads from another service.
const MaxBody = 8 << 20

// Error is a failed call to another service, classified for retry.
type Error struct {
	Service   string
	Op        string
	Status    int
	Code      string
	Message   string
	Retryable bool
}

func (e *Error) Error() string {
	parts := []string{e.Service, e.Op}
	if e.Status != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", e.Status))
	}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, ": ")
}

// IsRetryable reports whether err may succeed on a later attempt. Unknown
// errors (transport failures) are retryable; classified errors say so.
func IsRetryable(err error) bool {
	var re *Error
	if errors.As(err, &re) {
		return re.Retryable
	}
	var te *machineclients.TokenError
	if errors.As(err, &te) {
		return te.Retryable()
	}
	return !errors.Is(err, machineclients.ErrNotConfigured) && !errors.Is(err, machineclients.ErrTenantNotMapped)
}

// RetryableStatus is the default classification of an HTTP status.
func RetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout ||
		status == http.StatusUnauthorized || status >= 500
}

// Transport wraps a transport-level failure (always retryable).
func Transport(service, op string, err error) *Error {
	return &Error{Service: service, Op: op, Message: err.Error(), Retryable: true}
}

// ReadJSON reads a bounded body into out.
func ReadJSON(resp *http.Response, out any) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxBody {
		return nil, fmt.Errorf("response body exceeds %d bytes", MaxBody)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return body, fmt.Errorf("decode response: %w", err)
		}
	}
	return body, nil
}

// StatusError builds a classified error from a non-success response. It
// understands the bodies of the services it calls: fabrication-prep
// {"errors": [{code, message, path}]}, Part 2 Result {"messages": [{code,
// text, path}]}, FastAPI {"detail"} and {"code","message"} / {"error"}.
func StatusError(service, op string, resp *http.Response) *Error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	e := &Error{Service: service, Op: op, Status: resp.StatusCode, Retryable: RetryableStatus(resp.StatusCode)}
	var problem struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   any    `json:"error"`
		Detail  any    `json:"detail"`
		Errors  []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Path    string `json:"path"`
		} `json:"errors"`
		Messages []struct {
			Code    string `json:"code"`
			Text    string `json:"text"`
			Message string `json:"message"`
			Path    string `json:"path"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &problem) == nil {
		e.Code = problem.Code
		switch {
		case problem.Message != "":
			e.Message = problem.Message
		case len(problem.Errors) > 0:
			m := problem.Errors[0]
			e.Code = m.Code
			e.Message = m.Message
			if m.Path != "" {
				e.Message += " (at " + m.Path + ")"
			}
		case len(problem.Messages) > 0:
			m := problem.Messages[0]
			if e.Code == "" {
				e.Code = m.Code
			}
			e.Message = strings.TrimSpace(m.Text + m.Message)
			if m.Path != "" {
				e.Message += " (at " + m.Path + ")"
			}
		case problem.Detail != nil:
			e.Message = fmt.Sprint(problem.Detail)
		case problem.Error != nil:
			e.Message = fmt.Sprint(problem.Error)
		}
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(body))
	}
	if len(e.Message) > 300 {
		e.Message = e.Message[:300]
	}
	return e
}

// Wrap passes classified errors (remote, token, configuration) through and
// wraps anything else as a retryable transport failure.
func Wrap(service, op string, err error) error {
	var re *Error
	var te *machineclients.TokenError
	switch {
	case errors.As(err, &re), errors.As(err, &te),
		errors.Is(err, machineclients.ErrNotConfigured), errors.Is(err, machineclients.ErrTenantNotMapped):
		return err
	}
	return Transport(service, op, err)
}
