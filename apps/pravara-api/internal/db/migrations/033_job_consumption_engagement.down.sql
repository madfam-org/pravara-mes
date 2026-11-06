-- 033 (down): remove job consumption and order engagement attribution.
-- Recorded consumption facts are dropped with the table (export them first if
-- a downstream ledger has not read them). Stock movements already posted to
-- inventory_transactions stay; they keep reference_type 'job_consumption'
-- with a reference_id that no longer resolves.
BEGIN;

DROP TABLE IF EXISTS job_consumption;
DROP FUNCTION IF EXISTS job_consumption_append_only();
DROP INDEX IF EXISTS idx_inventory_txn_job_consumption;

DROP INDEX IF EXISTS idx_orders_engagement_slug;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_engagement_slug_format;
ALTER TABLE orders DROP COLUMN IF EXISTS engagement_slug;

COMMIT;
