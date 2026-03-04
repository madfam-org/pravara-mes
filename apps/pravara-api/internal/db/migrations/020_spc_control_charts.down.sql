DROP TRIGGER IF EXISTS update_spc_limits_updated_at ON spc_control_limits;
DROP POLICY IF EXISTS tenant_isolation_spc_violations ON spc_violations;
DROP POLICY IF EXISTS tenant_isolation_spc_limits ON spc_control_limits;
DROP TABLE IF EXISTS spc_violations;
DROP TABLE IF EXISTS spc_control_limits;
DROP TYPE IF EXISTS spc_violation_type;
