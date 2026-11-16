package artifactstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BackendR2 is the STORAGE_BACKEND value Enclii writes for a Cloudflare R2
// bucket (`enclii buckets create`).
const BackendR2 = "r2"

// The environment variables Enclii writes into the bucket's Secret. They are
// read under exactly these names.
const (
	EnvBackend         = "STORAGE_BACKEND"
	EnvEndpointURL     = "R2_ENDPOINT_URL"
	EnvBucketName      = "R2_BUCKET_NAME"
	EnvAccessKeyID     = "R2_ACCESS_KEY_ID"
	EnvSecretAccessKey = "R2_SECRET_ACCESS_KEY" // #nosec G101 -- an environment variable name, not a value
)

// metaSHA256 is the user metadata key that records an object's digest.
const metaSHA256 = "sha256"

// Config is an S3-compatible bucket and its bucket-scoped credentials.
type Config struct {
	Backend         string
	EndpointURL     string
	BucketName      string
	AccessKeyID     string
	SecretAccessKey string
	// MaxAttempts caps the SDK's attempts per request (its default when
	// zero). Not read from the environment: tests set 1 so an unreachable
	// endpoint fails at once instead of backing off past their timeout.
	MaxAttempts int
}

// Empty reports whether nothing at all is configured.
func (c Config) Empty() bool {
	return c.Backend == "" && c.EndpointURL == "" && c.BucketName == "" && c.AccessKeyID == "" && c.SecretAccessKey == ""
}

// Missing names the settings that are not set, by environment variable.
func (c Config) Missing() []string {
	var out []string
	for _, f := range []struct{ name, value string }{
		{EnvBackend, c.Backend}, {EnvEndpointURL, c.EndpointURL}, {EnvBucketName, c.BucketName},
		{EnvAccessKeyID, c.AccessKeyID}, {EnvSecretAccessKey, c.SecretAccessKey},
	} {
		if strings.TrimSpace(f.value) == "" {
			out = append(out, f.name)
		}
	}
	return out
}

// Validate checks the configuration is complete and usable. Its errors never
// carry the credentials.
func (c Config) Validate() error {
	if missing := c.Missing(); len(missing) > 0 {
		return fmt.Errorf("artifact store: missing %s", strings.Join(missing, ", "))
	}
	if c.Backend != BackendR2 {
		return fmt.Errorf("artifact store: %s=%q; only %q is supported", EnvBackend, c.Backend, BackendR2)
	}
	u, err := url.Parse(c.EndpointURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("artifact store: %s is not a URL", EnvEndpointURL)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		// A local S3-compatible server in tests; never credentials in clear
		// text over a network.
	default:
		return fmt.Errorf("artifact store: %s must be https", EnvEndpointURL)
	}
	if u.User != nil || u.RawQuery != "" {
		return fmt.Errorf("artifact store: %s must be a plain endpoint URL", EnvEndpointURL)
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DefaultCallTimeout bounds one store call (a Put with its Stat, a Stat, or
// opening an object). A dispatch hop holds a lease while it waits on these.
const DefaultCallTimeout = 2 * time.Minute

// S3 is a Store on an S3-compatible bucket (Cloudflare R2 in production).
type S3 struct {
	client *s3.Client
	bucket string
	// CallTimeout bounds each call (DefaultCallTimeout when zero). Reading
	// an opened object's body is bounded by the caller's context.
	CallTimeout time.Duration
}

func (s *S3) timeout() time.Duration {
	if s.CallTimeout > 0 {
		return s.CallTimeout
	}
	return DefaultCallTimeout
}

// NewS3 creates the store. httpClient may be nil.
func NewS3(c Config, httpClient *http.Client) (*S3, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	client := s3.New(s3.Options{
		Region:           "auto", // R2 ignores the region; SigV4 needs one
		BaseEndpoint:     aws.String(strings.TrimRight(c.EndpointURL, "/")),
		Credentials:      credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""),
		UsePathStyle:     true,
		HTTPClient:       httpClient,
		RetryMaxAttempts: c.MaxAttempts,
		// Only the checksums S3 requires. pravara verifies sha256 itself on
		// both sides, and R2 does not implement every optional checksum the
		// SDK would otherwise add by default.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &S3{client: client, bucket: c.BucketName}, nil
}

// Put implements Store. The write is conditional (If-None-Match: *), so two
// writers racing on one key never replace an object.
func (s *S3) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, digest, mediaType string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	raw, err := hex.DecodeString(digest)
	if err != nil || len(raw) != sha256.Size {
		return false, fmt.Errorf("artifact store: %q is not a hex sha256", digest)
	}
	if existing, err := s.Stat(ctx, key); err == nil {
		if same(existing, size, digest) {
			return false, nil
		}
		return false, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	in := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		Metadata:      map[string]string{metaSHA256: digest},
		IfNoneMatch:   aws.String("*"),
		// The server checks the body against it and refuses other bytes.
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(raw)),
	}
	if mediaType != "" {
		in.ContentType = aws.String(mediaType)
	}
	_, err = s.client.PutObject(ctx, in)
	if status(err) == http.StatusPreconditionFailed {
		// Someone stored it first: fine when it is these bytes.
		existing, serr := s.Stat(ctx, key)
		if serr != nil {
			return false, serr
		}
		if same(existing, size, digest) {
			return false, nil
		}
		return false, ErrConflict
	}
	if err != nil {
		return false, fmt.Errorf("artifact store: put %s: %w", key, err)
	}
	return true, nil
}

// Open implements Store.
// Opening is bounded by CallTimeout; reading the body by ctx, which the
// caller sizes to the object.
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, *Info, error) {
	callCtx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(s.timeout(), cancel)
	out, err := s.client.GetObject(callCtx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		timer.Stop()
		cancel()
		if status(err) == http.StatusNotFound {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("artifact store: get %s: %w", key, err)
	}
	if !timer.Stop() {
		cancel()
		_ = out.Body.Close()
		return nil, nil, fmt.Errorf("artifact store: get %s: %w", key, context.DeadlineExceeded)
	}
	return &cancelOnClose{ReadCloser: out.Body, cancel: cancel}, &Info{Key: key, Size: aws.ToInt64(out.ContentLength), SHA256: out.Metadata[metaSHA256],
		MediaType: aws.ToString(out.ContentType)}, nil
}

// cancelOnClose ends an opened object's context when the body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// Stat implements Store.
func (s *S3) Stat(ctx context.Context, key string) (*Info, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		if status(err) == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("artifact store: head %s: %w", key, err)
	}
	return &Info{Key: key, Size: aws.ToInt64(out.ContentLength), SHA256: out.Metadata[metaSHA256],
		MediaType: aws.ToString(out.ContentType)}, nil
}

// status is the HTTP status of a failed S3 call (0 when there was none).
func status(err error) int {
	var re *awshttp.ResponseError
	if errors.As(err, &re) {
		return re.HTTPStatusCode()
	}
	return 0
}
