package artifactstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestKey(t *testing.T) {
	tenant := uuid.MustParse("6f1c2c1e-6d0b-4a39-9c43-7c0d1c0e0001")
	d := digest([]byte("x"))
	key, err := Key(tenant, d)
	require.NoError(t, err)
	assert.Equal(t, "tenants/6f1c2c1e-6d0b-4a39-9c43-7c0d1c0e0001/sha256/"+d, key)

	_, err = Key(uuid.Nil, d)
	assert.Error(t, err, "no tenant, no key")
	for _, bad := range []string{"", "abc", strings.ToUpper(d), d + "0", "../" + d[3:]} {
		_, err := Key(tenant, bad)
		assert.Error(t, err, bad)
	}
}

func TestConfigValidate(t *testing.T) {
	full := Config{Backend: "r2", EndpointURL: "https://account.r2.example.test", BucketName: "b",
		AccessKeyID: "placeholder-id", SecretAccessKey: "placeholder-secret-value"}
	require.NoError(t, full.Validate())
	assert.Empty(t, full.Missing())
	assert.True(t, Config{}.Empty())
	assert.Equal(t, []string{EnvBackend, EnvEndpointURL, EnvBucketName, EnvAccessKeyID, EnvSecretAccessKey}, Config{}.Missing())

	cases := map[string]func(c *Config){
		"no secret":            func(c *Config) { c.SecretAccessKey = "" },
		"another backend":      func(c *Config) { c.Backend = "s3" },
		"clear text":           func(c *Config) { c.EndpointURL = "http://account.r2.example.test" },
		"not a URL":            func(c *Config) { c.EndpointURL = "account.r2.example.test" },
		"credentials in URL":   func(c *Config) { c.EndpointURL = "https://user:pw@account.r2.example.test" },
		"query in URL":         func(c *Config) { c.EndpointURL = "https://account.r2.example.test/?x=1" },
		"whitespace bucket id": func(c *Config) { c.BucketName = "  " },
	}
	for name, mutate := range cases {
		c := full
		mutate(&c)
		err := c.Validate()
		require.Error(t, err, name)
		assert.NotContains(t, err.Error(), "placeholder-secret-value", "errors never carry the secret")
	}
	local := full
	local.EndpointURL = "http://127.0.0.1:9000"
	assert.NoError(t, local.Validate(), "a loopback test server may be plain http")
}

// storeContract is what every Store must do: write-once, content-checked,
// no-op on the same bytes, conflict on other bytes, not-found reported.
func storeContract(t *testing.T, s Store) {
	ctx := context.Background()
	tenant := uuid.New()
	data := []byte("PK\x03\x04 kept geometry " + uuid.NewString())
	d := digest(data)
	key, err := Key(tenant, d)
	require.NoError(t, err)

	_, err = s.Stat(ctx, key)
	assert.ErrorIs(t, err, ErrNotFound)
	_, _, err = s.Open(ctx, key)
	assert.ErrorIs(t, err, ErrNotFound)

	stored, err := s.Put(ctx, key, bytes.NewReader(data), int64(len(data)), d, "model/3mf")
	require.NoError(t, err)
	assert.True(t, stored)

	info, err := s.Stat(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), info.Size)
	assert.Equal(t, d, info.SHA256, "the digest is recorded with the object")

	body, info, err := s.Open(ctx, key)
	require.NoError(t, err)
	got, err := io.ReadAll(body)
	require.NoError(t, body.Close())
	require.NoError(t, err)
	assert.Equal(t, data, got)
	assert.Equal(t, "model/3mf", info.MediaType)

	stored, err = s.Put(ctx, key, bytes.NewReader(data), int64(len(data)), d, "model/3mf")
	require.NoError(t, err)
	assert.False(t, stored, "the same bytes again write nothing")

	other := append([]byte(nil), data...)
	other = append(other, '!')
	_, err = s.Put(ctx, key, bytes.NewReader(other), int64(len(other)), digest(other), "model/3mf")
	assert.ErrorIs(t, err, ErrConflict, "a content-addressed key never takes other bytes")
	body, _, err = s.Open(ctx, key)
	require.NoError(t, err)
	got, _ = io.ReadAll(body)
	_ = body.Close()
	assert.Equal(t, data, got, "the first object is untouched")
}

// putChecks is what every Store must refuse besides the contract: bytes that
// are not their declared digest.
func putChecks(t *testing.T, s Store) {
	ctx := context.Background()
	data := []byte("declared " + uuid.NewString())
	wrong := digest([]byte("something else"))
	key, _ := Key(uuid.New(), wrong)
	_, err := s.Put(ctx, key, bytes.NewReader(data), int64(len(data)), wrong, "")
	require.Error(t, err, "a body that is not its declared sha256 is refused")
	t.Logf("refused: %s", Describe(err))
	_, err = s.Stat(ctx, key)
	assert.ErrorIs(t, err, ErrNotFound, "nothing was stored")
}

func TestMemoryStoreContract(t *testing.T) {
	m := NewMemory()
	storeContract(t, m)
	assert.Equal(t, 1, m.Writes)
	putChecks(t, m)

	// An object without a recorded digest is not "already stored": pravara
	// cannot tell what it holds.
	data := []byte("left by someone else")
	key, _ := Key(uuid.New(), digest(data))
	m.PutRaw(key, data, "")
	_, err := m.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), digest(data), "")
	assert.ErrorIs(t, err, ErrConflict)

	m.PutErr = errors.New("bucket unavailable")
	_, err = m.Put(context.Background(), "k", bytes.NewReader(nil), 0, digest(nil), "")
	assert.Error(t, err)
}

// TestS3StoreContractAgainstS3CompatibleServer runs the contract against a
// real S3-compatible server (MinIO, SeaweedFS). Opt-in: start a throwaway
// one on a loopback port and set PRAVARA_TEST_S3_ENDPOINT (a loopback http
// URL), PRAVARA_TEST_S3_ACCESS_KEY and PRAVARA_TEST_S3_SECRET_KEY. CI does
// not run it, and it never runs against R2.
func TestS3StoreContractAgainstS3CompatibleServer(t *testing.T) {
	endpoint := os.Getenv("PRAVARA_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("PRAVARA_TEST_S3_ENDPOINT not set; skipping the S3-compatibility test")
	}
	bucket := "pravara-test-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	store, err := NewS3(Config{Backend: BackendR2, EndpointURL: endpoint, BucketName: bucket,
		AccessKeyID: os.Getenv("PRAVARA_TEST_S3_ACCESS_KEY"), SecretAccessKey: os.Getenv("PRAVARA_TEST_S3_SECRET_KEY")}, nil)
	require.NoError(t, err)
	_, err = store.client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	storeContract(t, store)
	putChecks(t, store)
	// The start-up self-check passes on a conforming server and leaves no probe.
	require.NoError(t, store.SelfCheck(context.Background()))
	left, err := store.client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(SelfCheckPrefix)})
	require.NoError(t, err)
	assert.Empty(t, left.Contents, "the self-check removes its probe")

	// An object without the sha256 metadata is a conflict, never "stored".
	bare := []byte("no metadata " + uuid.NewString())
	bareKey, _ := Key(uuid.New(), digest(bare))
	_, err = store.client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(bareKey),
		Body: bytes.NewReader(bare)})
	require.NoError(t, err)
	_, err = store.Put(context.Background(), bareKey, bytes.NewReader(bare), int64(len(bare)), digest(bare), "")
	assert.ErrorIs(t, err, ErrConflict)

	// An object another writer stored first is found and left alone.
	data := []byte("written first " + uuid.NewString())
	key, _ := Key(uuid.New(), digest(data))
	_, err = store.client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key),
		Body: bytes.NewReader(data), Metadata: map[string]string{metaSHA256: digest(data)}})
	require.NoError(t, err)
	stored, err := store.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), digest(data), "")
	require.NoError(t, err)
	assert.False(t, stored)

	// Put relies on the server refusing a conditional write over an
	// existing object (If-None-Match: *) with 412: a racing writer never
	// replaces bytes.
	_, err = store.client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key),
		Body: bytes.NewReader([]byte("other")), IfNoneMatch: aws.String("*")})
	require.Error(t, err)
	assert.Equal(t, 412, status(err))
}

// Store errors reach a dispatch's error message, which tenants read: the
// description never carries the endpoint, bucket or key.
func TestDescribeHidesTheEndpoint(t *testing.T) {
	store, err := NewS3(Config{Backend: BackendR2, EndpointURL: "http://127.0.0.1:1", BucketName: "secret-bucket-name",
		AccessKeyID: "placeholder-id", SecretAccessKey: "placeholder-secret", MaxAttempts: 1}, nil)
	require.NoError(t, err)
	store.CallTimeout = 30 * time.Second
	data := []byte("x")
	key, _ := Key(uuid.New(), digest(data))
	_, err = store.Put(context.Background(), key, bytes.NewReader(data), 1, digest(data), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1", "the full error (logged server-side) names the endpoint")
	safe := Describe(err)
	for _, leak := range []string{"127.0.0.1", "secret-bucket-name", key, "tenants/"} {
		assert.NotContains(t, safe, leak)
	}
	assert.Equal(t, "the artifact store could not be reached", safe)
	assert.Equal(t, "the artifact store did not answer in time", Describe(context.DeadlineExceeded))
	assert.Equal(t, "the object already stored at this content address differs", Describe(ErrConflict))
}
