-- =============================================================================
-- Rollback Quality Management System
-- =============================================================================

-- Drop foreign key constraint
ALTER TABLE quality_certificates DROP CONSTRAINT IF EXISTS fk_quality_certificates_batch_lot;

-- Drop triggers
DROP TRIGGER IF EXISTS update_batch_lots_updated_at ON batch_lots;
DROP TRIGGER IF EXISTS update_inspections_updated_at ON inspections;
DROP TRIGGER IF EXISTS update_quality_certificates_updated_at ON quality_certificates;

-- Drop policies
DROP POLICY IF EXISTS tenant_isolation_batch_lots ON batch_lots;
DROP POLICY IF EXISTS tenant_isolation_inspections ON inspections;
DROP POLICY IF EXISTS tenant_isolation_quality_certificates ON quality_certificates;

-- Drop tables
DROP TABLE IF EXISTS batch_lots CASCADE;
DROP TABLE IF EXISTS inspections CASCADE;
DROP TABLE IF EXISTS quality_certificates CASCADE;

-- Drop enums
DROP TYPE IF EXISTS inspection_result;
DROP TYPE IF EXISTS quality_cert_status;
DROP TYPE IF EXISTS quality_cert_type;
