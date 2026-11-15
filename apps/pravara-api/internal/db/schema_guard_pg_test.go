package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// The schema guard against a real PostgreSQL: the error codes it branches on
// come from the server, so a mock cannot vouch for them.
//
// Opt-in: set PRAVARA_TEST_DATABASE_URL to a server where the role may CREATE
// DATABASE and CREATE ROLE. The test creates a scratch database and drops it.

func scratchDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	base := os.Getenv("PRAVARA_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("PRAVARA_TEST_DATABASE_URL not set; skipping PostgreSQL schema guard test")
	}
	admin, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	name := "schema_guard_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.Path = "/" + name
	scratch, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatalf("open scratch database: %v", err)
	}
	t.Cleanup(func() {
		_ = scratch.Close()
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
		_ = admin.Close()
	})
	return scratch, u.String()
}

func quietGuard(database *sql.DB, required []string) *SchemaGuard {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	guard := NewSchemaGuard(database, required, log)
	guard.recheckEvery = 0 // ask the database every time
	return guard
}

func TestSchemaGuardOnPostgres(t *testing.T) {
	scratch, _ := scratchDatabase(t)
	required := RequiredMigrations()
	guard := quietGuard(scratch, required)
	ctx := context.Background()

	// No tracking table at all.
	status := guard.Status(ctx)
	if status.Err != nil {
		t.Fatalf("a missing table must read as behind, not unverifiable: %v", status.Err)
	}
	if len(status.Missing) != len(required) {
		t.Fatalf("missing %d of %d, want all", len(status.Missing), len(required))
	}

	// The table as infra/db/migrate.sh creates it, with all but the last two.
	if _, err := scratch.Exec(`CREATE TABLE schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		t.Fatalf("create tracking table: %v", err)
	}
	held := required[len(required)-2:]
	for _, version := range required[:len(required)-2] {
		if _, err := scratch.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			t.Fatalf("record %s: %v", version, err)
		}
	}
	status = guard.Status(ctx)
	if strings.Join(status.Missing, ",") != strings.Join(held, ",") {
		t.Fatalf("missing = %v, want %v", status.Missing, held)
	}

	// The operator applies them: the same instance becomes current.
	for _, version := range held {
		if _, err := scratch.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			t.Fatalf("record %s: %v", version, err)
		}
	}
	if status = guard.Status(ctx); status.Behind() {
		t.Fatalf("expected current after recording every migration, got %+v", status)
	}
}

// The application role holds nothing on schema_migrations unless it is
// granted SELECT (infra/db/roles/pravara_app_grants.sql). Without the grant
// the guard cannot tell and must refuse; with it, it works.
func TestSchemaGuardAsARoleWithoutAndWithTheGrant(t *testing.T) {
	scratch, scratchURL := scratchDatabase(t)
	role := "guard_app_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if _, err := scratch.Exec(fmt.Sprintf(`CREATE ROLE %s LOGIN NOSUPERUSER NOBYPASSRLS`, role)); err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = scratch.Exec(fmt.Sprintf(`DROP OWNED BY %s`, role))
		_, _ = scratch.Exec(fmt.Sprintf(`DROP ROLE IF EXISTS %s`, role))
	})
	if _, err := scratch.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		INSERT INTO schema_migrations (version) VALUES ('001_a')`); err != nil {
		t.Fatalf("seed tracking table: %v", err)
	}
	if _, err := scratch.Exec(fmt.Sprintf(`REVOKE ALL ON schema_migrations FROM %s`, role)); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	u, _ := url.Parse(scratchURL)
	u.User = url.User(role)
	asRole, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatalf("open as role: %v", err)
	}
	defer asRole.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := asRole.PingContext(ctx); err != nil {
		t.Skipf("cannot connect as a password-less role (server needs trust auth): %v", err)
	}

	guard := quietGuard(asRole, []string{"001_a"})
	status := guard.Status(ctx)
	if status.Reason() != "schema_unverified" {
		t.Fatalf("without the grant the guard must be unverified, got %+v", status)
	}

	if _, err := scratch.Exec(fmt.Sprintf(`GRANT SELECT ON schema_migrations TO %s`, role)); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if status = guard.Status(ctx); status.Behind() {
		t.Fatalf("with the grant the guard must be current, got %+v", status)
	}

	// SELECT only: the application role still cannot record a migration.
	if _, err := asRole.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ('999_forged')`); err == nil {
		t.Fatal("the application role must not be able to write schema_migrations")
	}
}
