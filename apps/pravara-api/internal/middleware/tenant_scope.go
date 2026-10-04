package middleware

import (
	"bufio"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

// commitFailedBody replaces a success response whose transaction did not commit.
const commitFailedBody = `{"error":"internal_error","message":"Failed to commit request"}`

// runInTenantScope binds a lazy tenant-scoped transaction to the request and
// runs the rest of the chain. The transaction is committed before the first
// response byte when the status is below 400 and rolled back otherwise, so a
// client never sees a success whose writes were lost. Statements issued after
// the response started open a fresh transaction that is ended when the chain
// returns.
func runInTenantScope(c *gin.Context, database *db.DB, tenantID string, log *logrus.Logger) {
	ctx, scope, err := db.NewTenantScope(c.Request.Context(), database.DB, tenantID)
	if err != nil {
		log.WithError(err).Warn("Rejected request with an invalid tenant id")
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error":   "forbidden",
			"message": "Invalid tenant context",
		})
		return
	}
	c.Request = c.Request.WithContext(ctx)

	w := &scopeCommitWriter{ResponseWriter: c.Writer, scope: scope, log: log}
	c.Writer = w
	defer func() {
		if r := recover(); r != nil {
			_ = scope.Rollback()
			c.Writer = w.ResponseWriter
			panic(r)
		}
	}()

	c.Next()

	if !w.decided {
		w.decide()
	} else if scope.Active() {
		// Statements after the response started: end them with the same rule.
		var endErr error
		if w.Status() < http.StatusBadRequest {
			endErr = scope.Commit()
		} else {
			endErr = scope.Rollback()
		}
		if endErr != nil {
			log.WithError(endErr).Error("Failed to end post-response tenant transaction")
		}
	}
	c.Writer = w.ResponseWriter
}

// scopeCommitWriter ends the request transaction just before the response is
// written.
type scopeCommitWriter struct {
	gin.ResponseWriter
	scope    *db.Scope
	log      *logrus.Logger
	decided  bool
	replaced bool
}

func (w *scopeCommitWriter) decide() {
	w.decided = true
	if w.Status() >= http.StatusBadRequest {
		if err := w.scope.Rollback(); err != nil {
			w.log.WithError(err).Warn("Failed to roll back request transaction")
		}
		return
	}
	if err := w.scope.Commit(); err != nil {
		w.log.WithError(err).Error("Failed to commit request transaction")
		w.replaced = true
		if !w.Written() {
			h := w.ResponseWriter.Header()
			h.Del("Content-Length")
			h.Set("Content-Type", "application/json; charset=utf-8")
			w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
			_, _ = w.ResponseWriter.Write([]byte(commitFailedBody))
		}
	}
}

func (w *scopeCommitWriter) ensureDecided() {
	if !w.decided {
		w.decide()
	}
}

// WriteHeader records the status; the decision waits for the first byte.
func (w *scopeCommitWriter) WriteHeader(code int) {
	if w.replaced {
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *scopeCommitWriter) Write(b []byte) (int, error) {
	w.ensureDecided()
	if w.replaced {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func (w *scopeCommitWriter) WriteString(s string) (int, error) {
	w.ensureDecided()
	if w.replaced {
		return len(s), nil
	}
	return w.ResponseWriter.WriteString(s)
}

func (w *scopeCommitWriter) WriteHeaderNow() {
	w.ensureDecided()
	if w.replaced {
		return
	}
	w.ResponseWriter.WriteHeaderNow()
}

func (w *scopeCommitWriter) Flush() {
	w.ensureDecided()
	w.ResponseWriter.Flush()
}

func (w *scopeCommitWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.ensureDecided()
	return w.ResponseWriter.Hijack()
}
