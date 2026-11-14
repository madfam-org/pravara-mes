package api

// Migration 035 on PostgreSQL: the backfill registers and links only what
// is deterministic (a valid, case-unique SKU becomes a registry code; a lot
// whose product_code is exactly a SKU is linked), leaves the rest unlinked,
// and the down migration removes everything it added. Opt-in with
// PRAVARA_TEST_DATABASE_URL (a throwaway database is created and dropped).

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration035BackfillIsDeterministic(t *testing.T) {
	base := os.Getenv("PRAVARA_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("PRAVARA_TEST_DATABASE_URL not set; skipping PostgreSQL migration test")
	}
	admin, err := sql.Open("postgres", base)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("pravara_mig035_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE DATABASE " + pq.QuoteIdentifier(name))
	require.NoError(t, err)
	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	scratch, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = scratch.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + pq.QuoteIdentifier(name) + " WITH (FORCE)")
	})

	files, err := filepath.Glob("../db/migrations/*.up.sql")
	require.NoError(t, err)
	sort.Strings(files)
	run := func(f string) {
		body, err := os.ReadFile(f)
		require.NoError(t, err)
		_, err = scratch.Exec(string(body))
		require.NoError(t, err, "migration %s", filepath.Base(f))
	}
	_, err = scratch.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	var later []string
	for _, f := range files {
		if filepath.Base(f) >= "035" {
			later = append(later, f)
			continue
		}
		run(f)
	}

	t1, t2 := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	_, err = scratch.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Synthetic one', 'syn-one'), ($2, 'Synthetic two', 'syn-two')`, t1, t2)
	require.NoError(t, err)
	_, err = scratch.Exec(`INSERT INTO inventory_items (tenant_id, sku, name, category, unit) VALUES
		($1, 'FIL-PLA-BLK', 'PLA black', 'filament', 'g'),
		($1, 'bad sku', 'not a code', 'material', 'pcs'),
		($1, 'DUP-A', 'case twin 1', 'material', 'pcs'),
		($1, 'dup-a', 'case twin 2', 'material', 'pcs'),
		($1, 'SPOOL-X', 'odd unit case', 'material', 'Spool'),
		($2, 'FIL-PLA-BLK', 'same code, other tenant', 'filament', 'kg')`, t1, t2)
	require.NoError(t, err)
	_, err = scratch.Exec(`INSERT INTO batch_lots (tenant_id, lot_number, product_name, product_code, quantity, unit) VALUES
		($1, 'LOT-1', 'PLA', 'FIL-PLA-BLK', 1000, 'g'), ($1, 'LOT-2', 'unknown', 'NOPE', 1, 'g')`, t1)
	require.NoError(t, err)

	run(later[0])
	require.True(t, strings.Contains(filepath.Base(later[0]), "035_"), later[0])

	codes := map[string]string{}
	rows, err := scratch.Query(`SELECT tenant_id::text || '/' || code, category || ' ' || base_unit FROM material_registry`)
	require.NoError(t, err)
	for rows.Next() {
		var k, v string
		require.NoError(t, rows.Scan(&k, &v))
		codes[k] = v
	}
	require.NoError(t, rows.Close())
	assert.Equal(t, map[string]string{
		t1 + "/FIL-PLA-BLK": "filament g",
		t1 + "/SPOOL-X":     "other spool",
		t2 + "/FIL-PLA-BLK": "filament kg",
	}, codes, "only valid, case-unique SKUs; invalid and case twins stay unlinked")

	var linked, unlinked, lotLinked int
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FILTER (WHERE material_id IS NOT NULL), count(*) FILTER (WHERE material_id IS NULL)
		FROM inventory_items`).Scan(&linked, &unlinked))
	assert.Equal(t, 3, linked)
	assert.Equal(t, 3, unlinked)
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FROM batch_lots WHERE inventory_item_id IS NOT NULL`).Scan(&lotLinked))
	assert.Equal(t, 1, lotLinked, "only the lot whose product_code is exactly a SKU")

	down, err := os.ReadFile(strings.Replace(later[0], ".up.sql", ".down.sql", 1))
	require.NoError(t, err)
	_, err = scratch.Exec(string(down))
	require.NoError(t, err)
	var cols int
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FROM information_schema.columns
		WHERE (table_name = 'inventory_items' AND column_name = 'material_id')
		   OR (table_name = 'batch_lots' AND column_name = 'inventory_item_id')`).Scan(&cols))
	assert.Zero(t, cols)
	var tables int
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name IN
		('material_registry', 'material_aliases', 'material_unit_conversions', 'inventory_receipts', 'inventory_counts',
		 'inventory_count_lines', 'inventory_conversion_gaps')`).Scan(&tables))
	assert.Zero(t, tables)

	for _, f := range later {
		run(f) // up again, then any later migration
	}
}

// Migration 036 down drops the corrections and the disposition column; up
// again restores them. A correction supersedes its own fact or a correction
// of the same fact, keeping the fact's unit family.
func TestMigration036CorrectionsDownAndUp(t *testing.T) {
	scratch := newScratchDatabase(t)
	tid, job := "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"
	_, err := scratch.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Synthetic three', 'syn-three')`, tid)
	require.NoError(t, err)
	var orig, other string
	require.NoError(t, scratch.QueryRow(`INSERT INTO job_consumption (tenant_id, job_kind, job_id, kind, material_code, quantity, unit,
		basis, source, occurred_at) VALUES ($1, 'task', $2, 'material', 'FIL-X', 5, 'g', 'actual', 'operator', NOW()) RETURNING id`,
		tid, job).Scan(&orig))
	require.NoError(t, scratch.QueryRow(`INSERT INTO job_consumption (tenant_id, job_kind, job_id, kind, material_code, quantity, unit,
		basis, source, occurred_at) VALUES ($1, 'task', $2, 'material', 'FIL-Y', 5, 'g', 'actual', 'operator', NOW()) RETURNING id`,
		tid, job).Scan(&other))
	correct := func(supersedes, unit string) error {
		_, err := scratch.Exec(`INSERT INTO job_consumption_corrections (tenant_id, consumption_id, supersedes_id, quantity, unit, disposition)
			VALUES ($1, $2, $3, 6, $4, 'scrap')`, tid, orig, supersedes, unit)
		return err
	}
	require.ErrorContains(t, correct(other, "g"), "supersedes the corrected fact")
	require.ErrorContains(t, correct(orig, "min"), "unit family")
	require.NoError(t, correct(orig, "g"))
	var n int
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FROM job_consumption_facts WHERE job_id = $1`, job).Scan(&n))
	assert.Equal(t, 3, n, "two facts and one correction through the view")
	_, err = scratch.Exec(`UPDATE job_consumption_corrections SET quantity = 7`)
	require.ErrorContains(t, err, "append-only")

	files, err := filepath.Glob("../db/migrations/036_*.sql")
	require.NoError(t, err)
	require.Len(t, files, 2)
	sort.Strings(files) // down, up
	exec := func(f string) {
		body, err := os.ReadFile(f)
		require.NoError(t, err)
		_, err = scratch.Exec(string(body))
		require.NoError(t, err, filepath.Base(f))
	}
	exec(files[0])
	var cols, tables int
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FROM job_consumption`).Scan(&n))
	assert.Equal(t, 2, n, "recorded facts stay")
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name = 'job_consumption'
		AND column_name = 'disposition'`).Scan(&cols))
	assert.Zero(t, cols)
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FROM information_schema.tables
		WHERE table_name IN ('job_consumption_corrections', 'job_consumption_facts')`).Scan(&tables))
	assert.Zero(t, tables)
	exec(files[1])
	var disposition string
	require.NoError(t, scratch.QueryRow(`SELECT disposition FROM job_consumption WHERE id = $1`, orig).Scan(&disposition))
	assert.Equal(t, "good", disposition)
}

// Migration 036 must not break the code it is applied under: production
// migrations are applied by hand, so 036 may run before this code deploys,
// or the code may roll back after it. The insert of PR A / main (an ON
// CONFLICT on the 033 fact key, no predicate) must keep working.
func TestMigration036KeepsTheOlderInsertWorking(t *testing.T) {
	scratch := newScratchDatabase(t)
	tid, job := "55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666"
	_, err := scratch.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Synthetic five', 'syn-five')`, tid)
	require.NoError(t, err)
	olderInsert := `
		INSERT INTO job_consumption (id, tenant_id, job_kind, job_id, dispatch_id, print_job_id, task_id,
			order_id, machine_id, kind, material_code, material_lot, quantity, unit, basis, source, note,
			recorded_by, occurred_at)
		VALUES (gen_random_uuid(), $1, 'task', $2, NULL, NULL, NULL, NULL, NULL, 'material', 'FIL-X', NULL, 5, 'g',
			'actual', 'operator', NULL, NULL, NOW())
		ON CONFLICT (tenant_id, job_kind, job_id, kind, basis, source, (COALESCE(material_code, ''))) DO NOTHING
		RETURNING created_at`
	var created time.Time
	require.NoError(t, scratch.QueryRow(olderInsert, tid, job).Scan(&created), "the older insert under 036")
	assert.ErrorIs(t, scratch.QueryRow(olderInsert, tid, job).Scan(&created), sql.ErrNoRows, "a replay is still a no-op")
	var n int
	require.NoError(t, scratch.QueryRow(`SELECT count(*) FROM job_consumption WHERE job_id = $1`, job).Scan(&n))
	assert.Equal(t, 1, n)
}
