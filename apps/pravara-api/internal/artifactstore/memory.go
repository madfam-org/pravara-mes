package artifactstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
)

// Memory is an in-memory Store for tests. It behaves like the S3 store
// (write-once, no delete) and can be told to fail.
type Memory struct {
	mu      sync.Mutex
	objects map[string]memoryObject
	// PutErr, when set, is returned by every Put (a store outage).
	PutErr error
	// Writes counts the objects actually written (a no-op Put is not one).
	Writes int
}

type memoryObject struct {
	data      []byte
	sha256    string
	mediaType string
}

// NewMemory returns an empty store.
func NewMemory() *Memory { return &Memory{objects: map[string]memoryObject{}} }

// Put implements Store.
func (m *Memory) Put(_ context.Context, key string, body io.ReadSeeker, size int64, digest, mediaType string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.PutErr != nil {
		return false, m.PutErr
	}
	if o, ok := m.objects[key]; ok {
		if same(&Info{Size: int64(len(o.data)), SHA256: o.sha256}, size, digest) {
			return false, nil
		}
		return false, ErrConflict
	}
	data, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil {
		return false, err
	}
	if int64(len(data)) != size {
		return false, io.ErrUnexpectedEOF
	}
	// What S3 does with x-amz-checksum-sha256: refuse bytes that are not
	// the declared digest.
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != digest {
		return false, errors.New("artifactstore: the body does not match its declared sha256")
	}
	m.objects[key] = memoryObject{data: data, sha256: digest, mediaType: mediaType}
	m.Writes++
	return true, nil
}

// Open implements Store.
func (m *Memory) Open(_ context.Context, key string) (io.ReadCloser, *Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), o.data...))),
		&Info{Key: key, Size: int64(len(o.data)), SHA256: o.sha256, MediaType: o.mediaType}, nil
}

// Stat implements Store.
func (m *Memory) Stat(_ context.Context, key string) (*Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return &Info{Key: key, Size: int64(len(o.data)), SHA256: o.sha256, MediaType: o.mediaType}, nil
}

// Bytes returns a copy of the object at key (tests).
func (m *Memory) Bytes(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	return append([]byte(nil), o.data...), ok
}

// Keys returns the stored keys (tests).
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objects))
	for k := range m.objects {
		keys = append(keys, k)
	}
	return keys
}

// Tamper replaces the bytes at key without touching the recorded digest, the
// way a damaged object would read back (tests of the read-side check).
func (m *Memory) Tamper(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.objects[key]
	o.data = append([]byte(nil), data...)
	m.objects[key] = o
}

// PutRaw stores data at key with the given recorded digest ("" for none),
// bypassing every check: an object some other writer left (tests).
func (m *Memory) PutRaw(key string, data []byte, digest string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memoryObject{data: append([]byte(nil), data...), sha256: digest}
}
