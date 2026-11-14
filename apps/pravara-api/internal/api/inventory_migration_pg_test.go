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
