package edge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/brokerauth"
)

func enrollCfg(t *testing.T, url string) Config {
	dir := t.TempDir()
	return Config{GroupID: testGroup, EdgeNodeID: testEdge, EnrollmentURL: url,
		PasswordFile: filepath.Join(dir, "state", "mqtt_password"), StateDir: filepath.Join(dir, "state")}
}

func TestEnrollRotatesOnlyAfterApproval(t *testing.T) {
	api := newFakeEnrollmentAPI(&memCredentials{creds: map[string]*brokerauth.Credential{}})
	defer api.srv.Close()
	cfg := enrollCfg(t, api.srv.URL)
	if err := writeFileAtomic(cfg.PasswordFile, []byte("current-credential-current-credential-0000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- Enroll(context.Background(), cfg, EnrollOptions{Client: api.srv.Client(), Rotate: true, PollInterval: 20 * time.Millisecond})
	}()
	eventually(t, "registration", func() bool { api.mu.Lock(); defer api.mu.Unlock(); return api.posts == 1 })
	if cur, _ := readSecret(cfg.PasswordFile); cur != "current-credential-current-credential-0000" {
		t.Fatal("the current credential was replaced before approval")
	}
	api.approve("BCDF-GH01")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if cur, _ := readSecret(cfg.PasswordFile); cur == "current-credential-current-credential-0000" || len(cur) != 43 {
		t.Fatal("rotated credential not activated")
	}
	if _, err := os.Stat(cfg.PasswordFile + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pending credential left behind")
	}
}

func TestEnrollFailsVisiblyWhenNotApproved(t *testing.T) {
	api := newFakeEnrollmentAPI(&memCredentials{creds: map[string]*brokerauth.Credential{}})
	defer api.srv.Close()
	cfg := enrollCfg(t, api.srv.URL)
	done := make(chan error, 1)
	go func() {
		done <- Enroll(context.Background(), cfg, EnrollOptions{Client: api.srv.Client(), PollInterval: 20 * time.Millisecond})
	}()
	eventually(t, "registration", func() bool { api.mu.Lock(); defer api.mu.Unlock(); return api.posts == 1 })
	api.mu.Lock()
	for id := range api.state {
		api.state[id] = "expired"
	}
	api.mu.Unlock()
	if err := <-done; !errors.Is(err, ErrEnrollmentNotApproved) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, "enrollment.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale enrollment state kept")
	}
}

func TestEnrollRequiresHTTPSAndIdentity(t *testing.T) {
	cfg := enrollCfg(t, "http://pravara.example.test")
	if err := Enroll(context.Background(), cfg, EnrollOptions{}); err == nil {
		t.Fatal("plain http accepted")
	}
	cfg = enrollCfg(t, "https://pravara.example.test")
	cfg.EdgeNodeID = "site/x"
	if err := Enroll(context.Background(), cfg, EnrollOptions{}); err == nil {
		t.Fatal("invalid edge node id accepted")
	}
}

func TestUsernameDefaultsToTheEnrolledName(t *testing.T) {
	cfg := Config{GroupID: testGroup, EdgeNodeID: testEdge}
	cfg.ApplyDefaults()
	if cfg.Username != sparkplug.EdgeNodeUsername(testGroup, testEdge) {
		t.Fatalf("username %q", cfg.Username)
	}
}
