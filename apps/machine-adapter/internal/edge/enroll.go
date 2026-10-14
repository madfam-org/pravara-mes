package edge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// Edge enrollment (MES-1 §3, no secret held by a person):
//
//  1. The box generates its own MQTT password (32 random bytes) and writes it
//     to password_file (mode 0600). Nobody reads or copies it.
//  2. It registers the password with pravara (POST /v1/edge/enrollments);
//     pravara keeps only a bcrypt hash and answers with a short, non-secret
//     user code.
//  3. A tenant admin approves that code in pravara (people only, SSO).
//  4. The box polls the enrollment until it is approved, then the edge node
//     can connect to the broker.
//
// With Rotate, a new password is generated next to the current one and
// replaces it only after the approval, so the box keeps working meanwhile.

// EnrollOptions tunes Enroll.
type EnrollOptions struct {
	// Client is the HTTP client for the pravara API (default: 30s timeout).
	Client *http.Client
	// Out receives the operator-facing progress lines (never the password).
	Out io.Writer
	// Rotate generates a new credential even if one exists.
	Rotate bool
	// PollInterval overrides the interval the API suggests.
	PollInterval time.Duration
}

// enrollmentState is persisted in state_dir so an interrupted enrollment
// resumes polling instead of registering again.
type enrollmentState struct {
	EnrollmentID string    `json:"enrollment_id"`
	UserCode     string    `json:"user_code"`
	ExpiresAt    time.Time `json:"expires_at"`
	PasswordFile string    `json:"password_file"`
}

type enrollmentResponse struct {
	EnrollmentID string    `json:"enrollment_id"`
	Status       string    `json:"status"`
	UserCode     string    `json:"user_code"`
	ExpiresAt    time.Time `json:"expires_at"`
	PollInterval int       `json:"poll_interval_seconds"`
	Error        string    `json:"error"`
	Message      string    `json:"message"`
}

// ErrEnrollmentNotApproved is returned when the enrollment ends without approval.
var ErrEnrollmentNotApproved = errors.New("edge enrollment was not approved")

// Enroll registers this edge node's self-generated broker credential with
// pravara and waits until a person approves it.
func Enroll(ctx context.Context, cfg Config, opts EnrollOptions) error {
	cfg.ApplyDefaults()
	if _, err := sparkplug.NodeTopic(cfg.GroupID, sparkplug.NBIRTH, cfg.EdgeNodeID); err != nil {
		return fmt.Errorf("edge identity: %w", err)
	}
	base, err := url.Parse(strings.TrimRight(cfg.EnrollmentURL, "/"))
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return fmt.Errorf("enrollment_url must be an https URL")
	}
	if cfg.PasswordFile == "" || cfg.StateDir == "" {
		return fmt.Errorf("password_file and state_dir are required for enrollment")
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	statePath := filepath.Join(cfg.StateDir, "enrollment.json")

	st, resumed := loadEnrollmentState(statePath)
	if resumed && time.Now().After(st.ExpiresAt) {
		resumed = false
	}
	if !resumed {
		target := cfg.PasswordFile
		password, err := readSecret(cfg.PasswordFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read password file: %w", err)
		}
		if opts.Rotate || password == "" {
			if password != "" {
				target = cfg.PasswordFile + ".pending"
			}
			if password, err = generatePassword(); err != nil {
				return err
			}
			if err := writeFileAtomic(target, []byte(password+"\n"), 0o600); err != nil {
				return fmt.Errorf("write credential: %w", err)
			}
		}
		resp, err := postEnrollment(ctx, opts.Client, base, cfg, password)
		if err != nil {
			return err
		}
		st = enrollmentState{EnrollmentID: resp.EnrollmentID, UserCode: resp.UserCode, ExpiresAt: resp.ExpiresAt, PasswordFile: target}
		if b, err := json.Marshal(st); err == nil {
			_ = writeFileAtomic(statePath, b, 0o600)
		}
		if opts.PollInterval <= 0 && resp.PollInterval > 0 {
			opts.PollInterval = time.Duration(resp.PollInterval) * time.Second
		}
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 5 * time.Second
	}
	_, _ = fmt.Fprintf(opts.Out, "Enrollment pending for edge node %s (group %s).\nApprove user code %s in pravara before %s.\n",
		cfg.EdgeNodeID, cfg.GroupID, st.UserCode, st.ExpiresAt.Local().Format(time.RFC1123))

	for {
		status, err := pollEnrollment(ctx, opts.Client, base, st.EnrollmentID)
		if err != nil {
			return err
		}
		switch status {
		case "approved":
			if st.PasswordFile != cfg.PasswordFile {
				if err := os.Rename(st.PasswordFile, cfg.PasswordFile); err != nil {
					return fmt.Errorf("activate rotated credential: %w", err)
				}
			}
			_ = os.Remove(statePath)
			_, _ = fmt.Fprintf(opts.Out, "Enrollment approved. The edge node can connect as %s.\n", cfg.Username)
			return nil
		case "pending":
		default:
			_ = os.Remove(statePath)
			if st.PasswordFile != cfg.PasswordFile {
				_ = os.Remove(st.PasswordFile)
			}
			return fmt.Errorf("%w: status %s; run the enrollment again", ErrEnrollmentNotApproved, status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.PollInterval):
		}
	}
}

func postEnrollment(ctx context.Context, client *http.Client, base *url.URL, cfg Config, password string) (*enrollmentResponse, error) {
	body, _ := json.Marshal(map[string]string{"group_id": cfg.GroupID, "edge_node_id": cfg.EdgeNodeID, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String()+"/v1/edge/enrollments", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register enrollment: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	var out enrollmentResponse
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out)
	if res.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("register enrollment: HTTP %d %s %s", res.StatusCode, out.Error, out.Message)
	}
	if out.EnrollmentID == "" || out.UserCode == "" {
		return nil, fmt.Errorf("register enrollment: incomplete response")
	}
	return &out, nil
}

func pollEnrollment(ctx context.Context, client *http.Client, base *url.URL, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/v1/edge/enrollments/"+url.PathEscape(id), nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("poll enrollment: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return "unknown", nil
	}
	var out enrollmentResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out); err != nil || res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poll enrollment: HTTP %d", res.StatusCode)
	}
	return out.Status, nil
}

func loadEnrollmentState(path string) (enrollmentState, bool) {
	var st enrollmentState
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &st) != nil || st.EnrollmentID == "" {
		return enrollmentState{}, false
	}
	return st, true
}

// generatePassword returns 32 random bytes in unpadded base64url (43 chars).
func generatePassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
