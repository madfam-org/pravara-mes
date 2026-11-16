// Package artifactstore keeps the manufacturing bytes fabrication dispatch
// sent to be made: the rendered geometry and its GOC-1 sidecar, as they were
// when pravara checked their digests (RFC 0059 S12).
//
// Objects are content-addressed and namespaced by tenant:
//
//	tenants/<tenant_id>/sha256/<hex digest>
//
// The store is write-once by design. The interface has no delete, a Put of an
// object already present writes nothing, and a Put never replaces an object
// with other bytes. What is kept is the manufacturing record; nothing in
// pravara removes it.
package artifactstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/aws/smithy-go"
	"github.com/google/uuid"
)

var (
	// ErrNotFound: no object at the key.
	ErrNotFound = errors.New("artifactstore: object not found")
	// ErrConflict: an object is already at the key and it is not the one
	// being put (another size or digest). A content-addressed key holding
	// other bytes is a damaged record; it is reported, never overwritten.
	ErrConflict = errors.New("artifactstore: the object at this key differs from the one being stored")
)

// Info describes a stored object.
type Info struct {
	Key  string `json:"key"`
	Size int64  `json:"size"`
	// SHA256 is the hex digest recorded with the object when it was stored
	// ("" when the backend has none to report).
	SHA256    string `json:"sha256,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

// Store is where kept bytes live.
type Store interface {
	// Put stores size bytes read from body under key. sha256Hex is the
	// caller-verified digest of those bytes, recorded with the object. When
	// an object with the same size (and digest, when recorded) is already at
	// key, nothing is written and stored is false. Another object at key is
	// ErrConflict.
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64, sha256Hex, mediaType string) (stored bool, err error)
	// Open streams the object at key (ErrNotFound when absent). The caller
	// closes the reader and verifies the bytes against what it recorded.
	Open(ctx context.Context, key string) (io.ReadCloser, *Info, error)
	// Stat describes the object at key (ErrNotFound when absent).
	Stat(ctx context.Context, key string) (*Info, error)
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Key is the object key of a tenant's bytes with the given sha256.
func Key(tenantID uuid.UUID, digest string) (string, error) {
	if tenantID == uuid.Nil {
		return "", errors.New("artifactstore: no tenant")
	}
	if !sha256Hex.MatchString(digest) {
		return "", fmt.Errorf("artifactstore: %q is not a lowercase hex sha256", digest)
	}
	return "tenants/" + tenantID.String() + "/sha256/" + digest, nil
}

// same reports whether an existing object is the one being put. An object
// without a recorded digest is never "the same": pravara cannot tell what it
// holds, so it is a conflict, not something already stored.
func same(existing *Info, size int64, digest string) bool {
	return existing.Size == size && existing.SHA256 != "" && existing.SHA256 == digest
}

// Describe is a store error in words that are safe to store where tenants can
// read them (a dispatch's error message): what went wrong and the HTTP status
// or S3 error code, never the endpoint, bucket or key. Log the full error
// server-side.
func Describe(err error) string {
	var api smithy.APIError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrConflict):
		return "the object already stored at this content address differs"
	case errors.Is(err, ErrNotFound):
		return "object not found"
	case errors.Is(err, context.DeadlineExceeded):
		return "the artifact store did not answer in time"
	case errors.Is(err, context.Canceled):
		return "the request was cancelled"
	case errors.As(err, &api):
		if code := status(err); code != 0 {
			return fmt.Sprintf("the artifact store answered HTTP %d (%s)", code, api.ErrorCode())
		}
		return "the artifact store answered " + api.ErrorCode()
	case status(err) != 0:
		return fmt.Sprintf("the artifact store answered HTTP %d", status(err))
	}
	return "the artifact store could not be reached"
}
