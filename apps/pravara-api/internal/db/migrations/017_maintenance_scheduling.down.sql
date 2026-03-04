DROP TRIGGER IF EXISTS update_maint_work_orders_updated_at ON maintenance_work_orders;
DROP TRIGGER IF EXISTS update_maint_schedules_updated_at ON maintenance_schedules;
DROP POLICY IF EXISTS tenant_isolation_maint_work_orders ON maintenance_work_orders;
DROP POLICY IF EXISTS tenant_isolation_maint_schedules ON maintenance_schedules;
DROP TABLE IF EXISTS maintenance_work_orders;
DROP TABLE IF EXISTS maintenance_schedules;
DROP TYPE IF EXISTS work_order_status;
DROP TYPE IF EXISTS maintenance_trigger_type;
