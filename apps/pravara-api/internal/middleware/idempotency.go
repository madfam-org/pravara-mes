package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// IdempotencyKeyHeader is the request header that makes a write safe to
// deliver more than once (RFC 0064 §5, ruling PV-4).
const IdempotencyKeyHeader = "Idempotency-Key"

// IdempotencyBusyRetryAfterSeconds is the Retry-After of the 503 a request
// gets while another request holds its key.
const IdempotencyBusyRetryAfterSeconds = 1

// IdempotentReplayedHeader marks an answer that is a stored one, sent again
// for a repeated request: nothing ran.
const IdempotentReplayedHeader = "Idempotent-Replayed"

const (
	// idempotencyKeyMaxLen matches receipts' idempotency_key (1-100).
	idempotencyKeyMaxLen = 100
	// idempotencyMaxBody bounds the request body read to fingerprint it.
	idempotencyMaxBody = 1 << 20
	// DefaultIdempotencyRetention is how long a key is kept when the
	// configuration says nothing: 35 days, a placeholder that spans a month
	// end until the terminal's outbox limits are measured (ruling PV-4).
	DefaultIdempotencyRetention = 35 * 24 * time.Hour
)

// IdempotencyStore keeps keys in the request's tenant transaction
// (repositories.IdempotencyRepository).
type IdempotencyStore interface {
	Claim(ctx context.Context, client, key, route, fingerprint string, notBefore time.Time) (*repositories.IdempotencyRecord, error)
	Complete(ctx context.Context, client, key string, status int, contentType string, body []byte) error
}

// Idempotent honours an Idempotency-Key header on the write it guards, with
// the semantics receipts already have for their body field:
//
//   - no header: the write runs as it always did;
//   - first use of a key: the write runs, and an answer below 400 is stored
//     with a fingerprint of the request (method, path, query, body);
//   - the same key and the same request: the stored answer (status, content
//     type, body) is sent again with "Idempotent-Replayed: true", and nothing
//     runs twice;
//   - the same key and another request: 409 idempotency_key_reused;
//   - an answer of 400 or more stores nothing, so the key stays free and the
//     request may be retried;
//   - while another request holds the key for longer than
//     repositories.IdempotencyLockTimeout: 503 idempotency_key_busy with
//     Retry-After.
//
// "Nothing runs twice" covers what the write does in Pravara's database
// transaction. What it does outside that transaction is not undone or
// deduplicated by the key: a realtime publish, an automation command written
// in its own transaction and its append to the dispatch stream happen once
// per run of the write, and a run whose transaction then fails (a commit
// error, a 5xx after them) may already have done them.
//
// A key belongs to the tenant and to the client: the OAuth client the token
// was issued to (a terminal's device), else the caller itself. Two clients
// never share a key. A key older than retention counts as unused.
//
// It must run inside the request's tenant transaction (after the auth
// middleware): the key is claimed in that transaction before the write and
// its answer stored before it commits, so the claim, the write and the
// answer commit or roll back together, and a concurrent request with the
// same key waits until the first ends.
func Idempotent(store IdempotencyStore, retention time.Duration, log *logrus.Logger) gin.HandlerFunc {
	if retention <= 0 {
		retention = DefaultIdempotencyRetention
	}
	return func(c *gin.Context) {
		values := c.Request.Header.Values(IdempotencyKeyHeader)
		if len(values) == 0 {
			c.Next()
			return
		}
		key := values[0]
		if len(values) > 1 || !validIdempotencyKey(key) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_idempotency_key",
				"message": "Idempotency-Key is one header of 1-100 printable characters",
			})
			return
		}
		client := idempotencyClient(c)
		if client == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Authentication required",
			})
			return
		}

		body, err := io.ReadAll(io.LimitReader(c.Request.Body, idempotencyMaxBody+1))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "validation_error", "message": "Failed to read the request body"})
			return
		}
		if len(body) > idempotencyMaxBody {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request_too_large", "message": "The request body is too large"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		fingerprint := requestFingerprint(c.Request, body)

		ctx := c.Request.Context()
		existing, err := store.Claim(ctx, client, key, c.FullPath(), fingerprint, time.Now().Add(-retention))
		if errors.Is(err, repositories.ErrIdempotencyBusy) {
			// Another delivery of this key is still running: retry shortly.
			c.Header("Retry-After", strconv.Itoa(IdempotencyBusyRetryAfterSeconds))
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":   "idempotency_key_busy",
				"message": "Another request with this Idempotency-Key is still running; retry",
			})
			return
		}
		if err != nil {
			idempotencyFailure(c, log, err, "claim idempotency key")
			return
		}
		if existing != nil {
			switch {
			case existing.RequestSHA256 != fingerprint:
				c.AbortWithStatusJSON(http.StatusConflict, gin.H{
					"error":   "idempotency_key_reused",
					"message": "This Idempotency-Key was already used with a different request",
				})
			case !existing.Completed:
				c.AbortWithStatusJSON(http.StatusConflict, gin.H{
					"error":   "idempotency_key_unfinished",
					"message": "A request with this Idempotency-Key did not record its answer; a person must check the write",
				})
			default:
				if existing.ResponseContentType != "" {
					c.Header("Content-Type", existing.ResponseContentType)
				}
				c.Header(IdempotentReplayedHeader, "true")
				c.Status(existing.ResponseStatus)
				_, _ = c.Writer.Write(existing.ResponseBody)
				c.Abort()
			}
			return
		}

		rec := &bufferedResponseWriter{ResponseWriter: c.Writer, status: http.StatusOK}
		c.Writer = rec
		c.Next()
		c.Writer = rec.ResponseWriter

		status := rec.status
		if status < http.StatusBadRequest {
			if err := store.Complete(ctx, client, key, status, rec.Header().Get("Content-Type"), rec.body.Bytes()); err != nil {
				// The write and the claim roll back with this answer.
				idempotencyFailure(c, log, err, "store idempotent response")
				return
			}
		}
		c.Writer.WriteHeader(status)
		if rec.body.Len() > 0 {
			_, _ = c.Writer.Write(rec.body.Bytes())
		} else {
			c.Writer.WriteHeaderNow()
		}
	}
}

// IdempotentOn applies Idempotent to the routes listed (RouteKey form) and
// lets every other request through untouched.
func IdempotentOn(routes []string, store IdempotencyStore, retention time.Duration, log *logrus.Logger) gin.HandlerFunc {
	guarded := make(map[string]bool, len(routes))
	for _, r := range routes {
		guarded[r] = true
	}
	inner := Idempotent(store, retention, log)
	return func(c *gin.Context) {
		if !guarded[RouteKey(c.Request.Method, c.FullPath())] {
			c.Next()
			return
		}
		inner(c)
	}
}

func idempotencyFailure(c *gin.Context, log *logrus.Logger, err error, what string) {
	if log != nil {
		log.WithError(err).WithField("route", c.FullPath()).Error(what)
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to process the Idempotency-Key"})
}

// validIdempotencyKey: 1-100 printable ASCII characters, not all spaces.
func validIdempotencyKey(key string) bool {
	if len(key) == 0 || len(key) > idempotencyKeyMaxLen || strings.TrimSpace(key) == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

// idempotencyClient names who a key belongs to inside the tenant: the OAuth
// client the token was issued to, else the caller ("user:<sub>",
// "api_key:<id>"). A terminal's device token and its operators' tokens name
// the same client, the device, so the device has one set of keys.
func idempotencyClient(c *gin.Context) string {
	if claims, ok := GetClaims(c); ok && claims != nil && claims.ClientID != "" {
		return "client:" + claims.ClientID
	}
	caller, ok := GetCaller(c)
	if !ok || caller.ID == "" {
		return ""
	}
	return caller.Actor()
}

// requestFingerprint hashes the method, the path, the sorted query and the
// body. A JSON body is hashed in canonical form (keys sorted, numbers as
// written), so a retry that re-encodes the same object matches.
func requestFingerprint(r *http.Request, body []byte) string {
	h := sha256.New()
	h.Write([]byte(r.Method))
	h.Write([]byte{0})
	h.Write([]byte(r.URL.Path))
	h.Write([]byte{0})
	h.Write([]byte(r.URL.Query().Encode()))
	h.Write([]byte{0})
	h.Write(canonicalJSON(body))
	return hex.EncodeToString(h.Sum(nil))
}

func canonicalJSON(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return trimmed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return trimmed // trailing data: not one JSON value
	}
	out, err := json.Marshal(v)
	if err != nil {
		return trimmed
	}
	return out
}

// bufferedResponseWriter holds the guarded write's answer until it is
// stored, so the request transaction commits (or rolls back) only once the
// answer and the key are both written.
type bufferedResponseWriter struct {
	gin.ResponseWriter
	status  int
	written bool
	body    bytes.Buffer
}

func (w *bufferedResponseWriter) WriteHeader(code int) {
	if code > 0 && !w.written {
		w.status = code
	}
}

func (w *bufferedResponseWriter) WriteHeaderNow() { w.written = true }

func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.body.Write(b)
}

func (w *bufferedResponseWriter) WriteString(s string) (int, error) {
	w.written = true
	return w.body.WriteString(s)
}

func (w *bufferedResponseWriter) Status() int { return w.status }

func (w *bufferedResponseWriter) Size() int {
	if !w.written {
		return -1
	}
	return w.body.Len()
}

func (w *bufferedResponseWriter) Written() bool { return w.written }

// Flush is held back with the rest of the answer.
func (w *bufferedResponseWriter) Flush() {}
