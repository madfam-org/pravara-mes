-- 035 (down): remove the material registry, conversions, receipts, counts
-- and conversion gaps. Receipts and counts are dropped with their tables
-- (export them first if a downstream ledger has not read them). Stock
-- movements already posted stay in inventory_transactions with
-- reference_type 'inventory_receipt' or 'inventory_count' and a
-- reference_id that no longer resolves; quantity_on_hand is not rewound.
BEGIN;

DROP POLICY IF EXISTS tenant_references_inventory_item ON batch_lots;
DROP POLICY IF EXISTS tenant_references_material ON inventory_items;

DROP TABLE IF EXISTS inventory_conversion_gaps;
DROP TABLE IF EXISTS inventory_count_lines;
DROP TABLE IF EXISTS inventory_counts;
DROP TABLE IF EXISTS inventory_receipts;
DROP FUNCTION IF EXISTS inventory_receipts_append_only();
DROP INDEX IF EXISTS idx_inventory_txn_receipt;
DROP INDEX IF EXISTS idx_inventory_txn_count;

DROP INDEX IF EXISTS idx_batch_lots_inventory_item;
ALTER TABLE batch_lots DROP COLUMN IF EXISTS inventory_item_id;

DROP FUNCTION IF EXISTS material_resolve(TEXT);
DROP TABLE IF EXISTS material_unit_conversions;
DROP TABLE IF EXISTS material_aliases;

DROP INDEX IF EXISTS ux_inventory_items_material;
ALTER TABLE inventory_items DROP COLUMN IF EXISTS material_id;

DROP TABLE IF EXISTS material_registry;
DROP FUNCTION IF EXISTS material_registry_code_immutable();

COMMIT;
