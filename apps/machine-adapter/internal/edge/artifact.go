package edge

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ErrDigestMismatch means the downloaded artifact is not the one the host
// commanded. Nothing is sent to the printer in that case.
var ErrDigestMismatch = errors.New("artifact digest mismatch")

// ArtifactFetcher downloads print artifacts over HTTPS into a scratch
// directory, hashing while it writes.
type ArtifactFetcher struct {
	Client   *http.Client
	Dir      string
	MaxBytes int64
}

// NewArtifactFetcher returns a fetcher whose client refuses non-HTTPS redirects.
func NewArtifactFetcher(dir string, maxBytes int64, timeout time.Duration) *ArtifactFetcher {
	return &ArtifactFetcher{
		Client: &http.Client{
			Timeout:       timeout,
			CheckRedirect: httpsOnlyRedirects,
		},
		Dir:      dir,
		MaxBytes: maxBytes,
	}
}

func httpsOnlyRedirects(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect to a non-https URL")
	}
	if len(via) >= 5 {
		return fmt.Errorf("too many redirects")
	}
	return nil
}

// Fetch downloads rawURL and verifies its SHA-256 against wantSHA256 (hex).
// On success it returns the path of a temporary file the caller must remove.
// On a digest mismatch the file is removed and ErrDigestMismatch returned.
func (f *ArtifactFetcher) Fetch(ctx context.Context, rawURL, wantSHA256 string) (string, int64, error) {
	if !strings.HasPrefix(rawURL, "https://") {
		return "", 0, fmt.Errorf("artifact URL must be https")
	}
	want, err := hex.DecodeString(strings.ToLower(wantSHA256))
	if err != nil || len(want) != sha256.Size {
		return "", 0, fmt.Errorf("artifact sha256 must be 64 hex characters")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("artifact request: %w", err)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		// Report the cause without the URL: artifact URLs are signed.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return "", 0, fmt.Errorf("artifact download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("artifact download: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > f.MaxBytes {
		return "", 0, fmt.Errorf("artifact is %d bytes, limit %d", resp.ContentLength, f.MaxBytes)
	}

	if f.Dir != "" {
		if err := os.MkdirAll(f.Dir, 0o700); err != nil {
			return "", 0, fmt.Errorf("artifact scratch dir: %w", err)
		}
	}
	tmp, err := os.CreateTemp(f.Dir, "artifact-*")
	if err != nil {
		return "", 0, fmt.Errorf("artifact scratch file: %w", err)
	}
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmp.Name())
		}
	}()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, f.MaxBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("artifact download: %w", err)
	}
	if n > f.MaxBytes {
		return "", 0, fmt.Errorf("artifact exceeds the %d byte limit", f.MaxBytes)
	}
	if subtle.ConstantTimeCompare(h.Sum(nil), want) != 1 {
		return "", 0, ErrDigestMismatch
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, fmt.Errorf("artifact scratch file: %w", err)
	}
	keep = true
	return tmp.Name(), n, nil
}
