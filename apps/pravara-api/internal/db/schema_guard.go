package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

// The migrations this binary was built with. Production migrations are
// applied by hand (docs/operations/database-app-role.md) while a merge to
// main rolls the new image out by itself, so the code can arrive before its
// schema. The schema guard compares this list with the database's
// schema_migrations table and keeps the instance out of service until every
// one of them is recorded there.
//
//go:embed migrations/*.up.sql
var migrationFiles embed.FS

// RequiredMigrations lists the versions of the embedded *.up.sql files, in
// order, named as infra/db/migrate.sh records them (the file name without
// ".up.sql").
func RequiredMigrations() []string {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		// The directory is embedded at build time; it cannot be missing.
		panic(fmt.Sprintf("db: embedded migrations unreadable: %v", err))
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		versions = append(versions, strings.TrimSuffix(name, ".up.sql"))
	}
	sort.Strings(versions)
	return versions
}

// SchemaStatus is the result of one comparison.
type SchemaStatus struct {
	// Missing lists the required migrations the database has not recorded.
	Missing []string
	// Err is set when the comparison itself could not be made (the table
	// could not be read). The schema is then treated as behind: an instance
	// that cannot tell must not serve.
	Err error
	// CheckedAt is when the database was last asked.
	CheckedAt time.Time
}

// Behind reports whether the instance must stay out of service.
func (s SchemaStatus) Behind() bool {
	return s.Err != nil || len(s.Missing) > 0
}

// Reason is a short, stable word for the state, for probes and logs.
func (s SchemaStatus) Reason() string {
	switch {
	case s.Err != nil:
		return "schema_unverified"
	case len(s.Missing) > 0:
		return "schema_behind"
	default:
		return "schema_current"
	}
}

// SchemaGuard answers whether the database has every migration this binary
// requires. A current answer is kept: migrations are not un-applied under a
// running instance. A "behind" answer is re-checked after recheckEvery, so
// the instance becomes ready on its own once the migration is applied.
type SchemaGuard struct {
	db           *sql.DB
	required     []string
	log          *logrus.Logger
	recheckEvery time.Duration
	queryTimeout time.Duration
	now          func() time.Time

	mu      sync.Mutex
	last    SchemaStatus
	checked bool
}

// NewSchemaGuard builds a guard for required versions (RequiredMigrations in
// production).
func NewSchemaGuard(database *sql.DB, required []string, log *logrus.Logger) *SchemaGuard {
	return &SchemaGuard{
		db:           database,
		required:     append([]string(nil), required...),
		log:          log,
		recheckEvery: 10 * time.Second,
		queryTimeout: 5 * time.Second,
		now:          time.Now,
	}
}

// Status returns the current comparison, asking the database when there is
// no answer yet or the last one was "behind" and is older than recheckEvery.
func (g *SchemaGuard) Status(ctx context.Context) SchemaStatus {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.checked {
		if !g.last.Behind() {
			return g.last
		}
		if g.now().Sub(g.last.CheckedAt) < g.recheckEvery {
			return g.last
		}
	}

	next := g.compare(ctx)
	g.report(next)
	g.last, g.checked = next, true
	return next
}

// compare reads schema_migrations and lists what is missing.
func (g *SchemaGuard) compare(ctx context.Context) SchemaStatus {
	status := SchemaStatus{CheckedAt: g.now()}

	ctx, cancel := context.WithTimeout(ctx, g.queryTimeout)
	defer cancel()

	rows, err := g.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "42P01" {
			// No tracking table: nothing was ever applied with the runner.
			status.Missing = append([]string(nil), g.required...)
			return status
		}
		status.Err = fmt.Errorf("read schema_migrations: %w", err)
		return status
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			status.Err = fmt.Errorf("read schema_migrations: %w", err)
			return status
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		status.Err = fmt.Errorf("read schema_migrations: %w", err)
		return status
	}

	for _, version := range g.required {
		if !applied[version] {
			status.Missing = append(status.Missing, version)
		}
	}
	return status
}

// report logs a change of state, not every probe.
func (g *SchemaGuard) report(next SchemaStatus) {
	if g.log == nil {
		return
	}
	if g.checked && sameSchemaState(g.last, next) {
		return
	}
	switch {
	case next.Err != nil:
		g.log.WithError(next.Err).Error("Schema guard: cannot verify the database schema; not accepting traffic")
	case len(next.Missing) > 0:
		g.log.WithField("missing_migrations", next.Missing).
			Error("Schema guard: the database is behind this build; not accepting traffic until these migrations are applied")
	default:
		g.log.WithField("migrations", len(g.required)).Info("Schema guard: the database has every migration this build requires")
	}
}

func sameSchemaState(a, b SchemaStatus) bool {
	if (a.Err == nil) != (b.Err == nil) {
		return false
	}
	if a.Err != nil {
		return a.Err.Error() == b.Err.Error()
	}
	return strings.Join(a.Missing, ",") == strings.Join(b.Missing, ",")
}
