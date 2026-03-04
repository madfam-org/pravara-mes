DROP TRIGGER IF EXISTS update_inventory_items_updated_at ON inventory_items;
DROP POLICY IF EXISTS tenant_isolation_inventory_txn ON inventory_transactions;
DROP POLICY IF EXISTS tenant_isolation_inventory_items ON inventory_items;
DROP TABLE IF EXISTS inventory_transactions;
DROP TABLE IF EXISTS inventory_items;
DROP TYPE IF EXISTS inventory_transaction_type;
