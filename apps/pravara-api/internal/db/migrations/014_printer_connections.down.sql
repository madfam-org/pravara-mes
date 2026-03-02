-- Drop printer connections schema

-- Drop foreign key constraint first
ALTER TABLE printer_connections
    DROP CONSTRAINT IF EXISTS fk_current_job;

-- Drop indexes
DROP INDEX IF EXISTS idx_printer_profiles_tenant;
DROP INDEX IF EXISTS idx_printer_connections_tenant;
DROP INDEX IF EXISTS idx_printer_connections_machine;
DROP INDEX IF EXISTS idx_printer_connections_state;
DROP INDEX IF EXISTS idx_material_profiles_tenant;
DROP INDEX IF EXISTS idx_print_jobs_tenant;
DROP INDEX IF EXISTS idx_print_jobs_connection;
DROP INDEX IF EXISTS idx_print_jobs_status;
DROP INDEX IF EXISTS idx_print_jobs_started;
DROP INDEX IF EXISTS idx_connection_logs_connection;
DROP INDEX IF EXISTS idx_connection_logs_created;
DROP INDEX IF EXISTS idx_connection_logs_event;
DROP INDEX IF EXISTS idx_maintenance_connection;

-- Drop tables in reverse order of dependencies
DROP TABLE IF EXISTS printer_maintenance;
DROP TABLE IF EXISTS printer_connection_logs;
DROP TABLE IF EXISTS print_jobs;
DROP TABLE IF EXISTS material_profiles;
DROP TABLE IF EXISTS printer_connections;
DROP TABLE IF EXISTS printer_profiles;