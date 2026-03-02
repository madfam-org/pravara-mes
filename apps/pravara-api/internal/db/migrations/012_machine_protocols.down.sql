-- Drop triggers
DROP TRIGGER IF EXISTS protocol_standards_updated_at ON protocol_standards;
DROP TRIGGER IF EXISTS machine_protocols_updated_at ON machine_protocols;
DROP TRIGGER IF EXISTS machine_firmwares_updated_at ON machine_firmwares;
DROP TRIGGER IF EXISTS adapter_compatibility_updated_at ON adapter_compatibility;
DROP TRIGGER IF EXISTS protocol_compliance_updated_at ON protocol_compliance;
DROP TRIGGER IF EXISTS discovered_machines_updated_at ON discovered_machines;

-- Drop policies
DROP POLICY IF EXISTS discovered_machines_tenant_isolation ON discovered_machines;

-- Drop indexes
DROP INDEX IF EXISTS idx_machine_protocols_standard;
DROP INDEX IF EXISTS idx_machine_firmwares_protocol;
DROP INDEX IF EXISTS idx_machine_firmwares_type;
DROP INDEX IF EXISTS idx_adapter_compatibility_adapter;
DROP INDEX IF EXISTS idx_adapter_compatibility_firmware;
DROP INDEX IF EXISTS idx_discovered_machines_tenant;
DROP INDEX IF EXISTS idx_discovered_machines_status;
DROP INDEX IF EXISTS idx_adapter_metrics_adapter;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS adapter_metrics;
DROP TABLE IF EXISTS discovered_machines;
DROP TABLE IF EXISTS protocol_compliance;
DROP TABLE IF EXISTS adapter_compatibility;
DROP TABLE IF EXISTS machine_firmwares;
DROP TABLE IF EXISTS machine_protocols;
DROP TABLE IF EXISTS protocol_standards;