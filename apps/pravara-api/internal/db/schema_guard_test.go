package db

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

const versionsQuery = `SELECT version FROM schema_migrations`

func newGuard(t *testing.T, required ...string) (*SchemaGuard, sqlmock.Sqlmock, *test.Hook, *time.Time) {
	t.Helper()
	database, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	log, hook := test.NewNullLogger()
	log.SetLevel(logrus.InfoLevel)
	guard := NewSchemaGuard(database, required, log)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	guard.now = func() time.Time { return now }
	return guard, mock, hook, &now
}

func versionRows(versions ...string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"version"})
	for _, v := range versions {
		rows.AddRow(v)
	}
	return rows
}

// The embedded list is what the guard enforces, so it must be exactly the
// *.up.sql files on disk: a file the embed pattern missed would never be
// required.
func TestRequiredMigrationsMatchTheFilesOnDisk(t *testing.T) {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	var onDisk []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			onDisk = append(onDisk, strings.TrimSuffix(e.Name(), ".up.sql"))
		}
	}
	sort.Strings(onDisk)

	got := RequiredMigrations()
	if len(got) == 0 {
		t.Fatal("no migrations embedded")
	}
	if strings.Join(got, ",") != strings.Join(onDisk, ",") {
		t.Fatalf("embedded migrations differ from disk:\n embedded %v\n on disk  %v", got, onDisk)
	}
	if got[0] != "001_genesis" {
		t.Errorf("first migration = %q, want 001_genesis", got[0])
	}
	if !sort.StringsAreSorted(got) {
		t.Error("required migrations are not sorted")
	}
}

func TestSchemaGuardCurrent(t *testing.T) {
	guard, mock, hook, _ := newGuard(t, "001_a", "002_b")
	// The database may be ahead of the build (a code rollback): that is fine.
	mock.ExpectQuery(versionsQuery).WillReturnRows(versionRows("002_b", "001_a", "003_c"))

	status := guard.Status(context.Background())
	if status.Behind() || status.Reason() != "schema_current" {
		t.Fatalf("expected current, got %+v", status)
	}

	// A current answer is kept: no second query.
	if again := guard.Status(context.Background()); again.Behind() {
		t.Fatalf("expected current on the second call, got %+v", again)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(hook.Entries) != 1 || hook.LastEntry().Level != logrus.InfoLevel {
		t.Errorf("expected one info line, got %d entries", len(hook.Entries))
	}
}

func TestSchemaGuardBehindListsWhatIsMissing(t *testing.T) {
	guard, mock, hook, _ := newGuard(t, "033_a", "034_b", "035_c")
	mock.ExpectQuery(versionsQuery).WillReturnRows(versionRows("033_a"))

	status := guard.Status(context.Background())
	if !status.Behind() || status.Reason() != "schema_behind" {
		t.Fatalf("expected behind, got %+v", status)
	}
	if strings.Join(status.Missing, ",") != "034_b,035_c" {
		t.Errorf("missing = %v, want [034_b 035_c]", status.Missing)
	}
	entry := hook.LastEntry()
	if entry == nil || entry.Level != logrus.ErrorLevel {
		t.Fatalf("expected an error line naming the migrations")
	}
	if got, _ := entry.Data["missing_migrations"].([]string); strings.Join(got, ",") != "034_b,035_c" {
		t.Errorf("logged missing_migrations = %v", entry.Data["missing_migrations"])
	}
}

func TestSchemaGuardNoTrackingTableMeansEverythingIsMissing(t *testing.T) {
	guard, mock, _, _ := newGuard(t, "001_a", "002_b")
	mock.ExpectQuery(versionsQuery).WillReturnError(&pq.Error{Code: "42P01", Message: `relation "schema_migrations" does not exist`})

	status := guard.Status(context.Background())
	if status.Err != nil {
		t.Fatalf("a missing table is a schema that is behind, not an unverifiable one: %v", status.Err)
	}
	if strings.Join(status.Missing, ",") != "001_a,002_b" {
		t.Errorf("missing = %v, want every required migration", status.Missing)
	}
}

// An instance that cannot read the table cannot tell, so it must not serve.
func TestSchemaGuardUnreadableTableIsBehind(t *testing.T) {
	for name, cause := range map[string]error{
		"permission denied": &pq.Error{Code: "42501", Message: "permission denied for table schema_migrations"},
		"connection lost":   errors.New("driver: bad connection"),
	} {
		t.Run(name, func(t *testing.T) {
			guard, mock, hook, _ := newGuard(t, "001_a")
			mock.ExpectQuery(versionsQuery).WillReturnError(cause)

			status := guard.Status(context.Background())
			if !status.Behind() || status.Reason() != "schema_unverified" {
				t.Fatalf("expected unverified, got %+v", status)
			}
			if !errors.Is(status.Err, cause) {
				t.Errorf("error does not wrap the cause: %v", status.Err)
			}
			if hook.LastEntry() == nil || hook.LastEntry().Level != logrus.ErrorLevel {
				t.Error("expected an error line")
			}
		})
	}
}

// A behind answer is re-checked, but not on every probe; once the migration
// is applied the instance becomes ready without a restart.
func TestSchemaGuardRechecksAndRecovers(t *testing.T) {
	guard, mock, hook, now := newGuard(t, "001_a", "002_b")
	mock.ExpectQuery(versionsQuery).WillReturnRows(versionRows("001_a"))
	mock.ExpectQuery(versionsQuery).WillReturnRows(versionRows("001_a"))
	mock.ExpectQuery(versionsQuery).WillReturnRows(versionRows("001_a", "002_b"))

	if !guard.Status(context.Background()).Behind() {
		t.Fatal("expected behind at first")
	}

	// Within the interval: the cached answer, no query.
	*now = now.Add(9 * time.Second)
	if !guard.Status(context.Background()).Behind() {
		t.Fatal("expected the cached behind answer")
	}

	// After the interval, still behind: asked again, not logged again.
	*now = now.Add(2 * time.Second)
	if !guard.Status(context.Background()).Behind() {
		t.Fatal("expected behind on the re-check")
	}
	if n := len(hook.Entries); n != 1 {
		t.Errorf("an unchanged state must not be logged again: %d entries", n)
	}

	// The migration is applied.
	*now = now.Add(11 * time.Second)
	if status := guard.Status(context.Background()); status.Behind() {
		t.Fatalf("expected current after the migration, got %+v", status)
	}
	if hook.LastEntry().Level != logrus.InfoLevel {
		t.Error("expected an info line on recovery")
	}

	// Current is kept from here on.
	*now = now.Add(time.Hour)
	if guard.Status(context.Background()).Behind() {
		t.Fatal("expected current to be kept")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaStatusReason(t *testing.T) {
	cases := map[string]SchemaStatus{
		"schema_current":    {},
		"schema_behind":     {Missing: []string{"x"}},
		"schema_unverified": {Err: errors.New("x")},
	}
	for want, status := range cases {
		if got := status.Reason(); got != want {
			t.Errorf("Reason() = %q, want %q", got, want)
		}
		if status.Behind() != (want != "schema_current") {
			t.Errorf("Behind() wrong for %q", want)
		}
	}
}
