-- =============================================================================
-- PravaraMES Genesis Schema Rollback
-- =============================================================================

-- Drop triggers
DROP TRIGGER IF EXISTS update_tasks_updated_at ON tasks;
DROP TRIGGER IF EXISTS update_machines_updated_at ON machines;
DROP TRIGGER IF EXISTS update_orders_updated_at ON orders;
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
DROP TRIGGER IF EXISTS update_tenants_updated_at ON tenants;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop RLS policies
DROP POLICY IF EXISTS tenant_isolation_audit ON audit_logs;
DROP POLICY IF EXISTS tenant_isolation_telemetry ON telemetry;
DROP POLICY IF EXISTS tenant_isolation_tasks ON tasks;
DROP POLICY IF EXISTS tenant_isolation_machines ON machines;
DROP POLICY IF EXISTS tenant_isolation_order_items ON order_items;
DROP POLICY IF EXISTS tenant_isolation_orders ON orders;
DROP POLICY IF EXISTS tenant_isolation_users ON users;

-- Drop tables (in dependency order)
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS telemetry;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS machines;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;

-- Drop enum types
DROP TYPE IF EXISTS machine_status;
DROP TYPE IF EXISTS task_status;
DROP TYPE IF EXISTS order_status;
