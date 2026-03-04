DROP TRIGGER IF EXISTS update_oee_snapshots_updated_at ON oee_snapshots;
DROP POLICY IF EXISTS tenant_isolation_oee_snapshots ON oee_snapshots;
DROP TABLE IF EXISTS oee_snapshots;
