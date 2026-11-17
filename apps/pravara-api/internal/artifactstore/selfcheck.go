package artifactstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

// SelfCheckPrefix is where the self-check writes. It is outside tenants/, so
// removing its probe never touches a kept manufacturing record.
const SelfCheckPrefix = "selfcheck/"

// SelfCheck proves, against the real bucket, the behaviours this store relies
// on and that only the server can confirm:
//
//  1. a PutObject carrying ChecksumSHA256, the sha256 metadata and
//     If-None-Match: * is accepted;
//  2. a second conditional PutObject on that key is refused with 412 (write
//     once is enforced by the server, not only by the HEAD before it);
//  3. HEAD returns the sha256 metadata, so an identical second Put is
//     recognised as already stored rather than a conflict;
//  4. the object reads back with the same digest;
//  5. Put of the same bytes again reports "already stored".
//
// The probe object is removed afterwards; a failed removal is returned only
// when every check passed (it changes nothing about the store's behaviour).
// Each step is bounded by CallTimeout.
func (s *S3) SelfCheck(ctx context.Context) (err error) {
	data := []byte("pravara artifact store self-check " + time.Now().UTC().Format(time.RFC3339Nano) + "\n")
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	key := SelfCheckPrefix + uuid.NewString()

	step := func(name string, f func(context.Context) error) error {
		cctx, cancel := context.WithTimeout(ctx, s.timeout())
		defer cancel()
		if err := f(cctx); err != nil {
			return fmt.Errorf("artifact store self-check, %s: %w", name, err)
		}
		return nil
	}
	put := func(cctx context.Context) error {
		_, err := s.client.PutObject(cctx, &s3.PutObjectInput{
			Bucket:         aws.String(s.bucket),
			Key:            aws.String(key),
			Body:           bytes.NewReader(data),
			ContentLength:  aws.Int64(int64(len(data))),
			Metadata:       map[string]string{metaSHA256: hexSum},
			IfNoneMatch:    aws.String("*"),
			ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(sum[:])),
		})
		return err
	}

	if err := step("conditional put with a sha256 checksum", put); err != nil {
		return err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), s.timeout())
		defer cancel()
		_, derr := s.client.DeleteObject(cctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
		if err == nil && derr != nil {
			err = fmt.Errorf("artifact store self-check passed but removing its probe failed: %w", derr)
		}
	}()
	if err := step("second conditional put is refused", func(cctx context.Context) error {
		perr := put(cctx)
		if perr == nil {
			return fmt.Errorf("the server accepted it: If-None-Match is not enforced")
		}
		if status(perr) != http.StatusPreconditionFailed {
			return fmt.Errorf("expected 412, got: %w", perr)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := step("HEAD reports the sha256 metadata", func(cctx context.Context) error {
		info, serr := s.Stat(cctx, key)
		if serr != nil {
			return serr
		}
		if info.SHA256 != hexSum || info.Size != int64(len(data)) {
			return fmt.Errorf("got size %d and sha256 %q", info.Size, info.SHA256)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := step("the object reads back intact", func(cctx context.Context) error {
		rc, _, oerr := s.Open(cctx, key)
		if oerr != nil {
			return oerr
		}
		defer rc.Close()
		h := sha256.New()
		if _, cerr := io.Copy(h, rc); cerr != nil {
			return cerr
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != hexSum {
			return fmt.Errorf("read sha256 %s, stored %s", got, hexSum)
		}
		return nil
	}); err != nil {
		return err
	}
	return step("the same bytes again are already stored", func(cctx context.Context) error {
		stored, perr := s.Put(cctx, key, bytes.NewReader(data), int64(len(data)), hexSum, "")
		if perr != nil {
			return perr
		}
		if stored {
			return fmt.Errorf("Put wrote again instead of recognising the object")
		}
		return nil
	})
}
