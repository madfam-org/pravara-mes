-- 028 down: restore the policies exactly as migrations 001-023 created them.

BEGIN;

DROP POLICY IF EXISTS system_scope_read ON event_outbox;
DROP POLICY IF EXISTS system_scope_read ON webhook_subscriptions;
DROP POLICY IF EXISTS system_scope_read ON webhook_deliveries;
DROP POLICY IF EXISTS system_scope_read ON api_keys;

DO $$
DECLARE
    t text;
BEGIN
    FOR t IN
        SELECT tablename FROM pg_policies
        WHERE schemaname = 'public' AND policyname = 'tenant_isolation'
    LOOP
        EXECUTE format('DROP POLICY tenant_isolation ON %I', t);
    END LOOP;

    -- Tables that had no RLS before 028.
    FOREACH t IN ARRAY ARRAY[
        'machine_model_associations', 'digital_twin_calibrations', 'machine_positions',
        'printer_connection_logs', 'printer_maintenance', 'video_recordings', 'gcode_simulations'
    ]
    LOOP
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
    END LOOP;
    FOR t IN
        SELECT c.relname FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        JOIN pg_class p ON p.oid = i.inhparent
        WHERE p.relname = 'machine_positions'
    LOOP
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;

-- 001
CREATE POLICY tenant_isolation_users ON users FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_orders ON orders FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_order_items ON order_items FOR ALL
    USING (order_id IN (
        SELECT id FROM orders
        WHERE tenant_id = current_setting('app.current_tenant_id', true)::UUID
    ));
CREATE POLICY tenant_isolation_machines ON machines FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_tasks ON tasks FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_telemetry ON telemetry FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_audit ON audit_logs FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 009
CREATE POLICY tenant_isolation_quality_certificates ON quality_certificates FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_inspections ON inspections FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_batch_lots ON batch_lots FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 010
CREATE POLICY tenant_isolation_task_commands ON task_commands FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 011
CREATE POLICY agents_tenant_isolation ON agents
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
CREATE POLICY agent_machines_tenant_isolation ON agent_machines
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
CREATE POLICY agent_assignments_tenant_isolation ON agent_assignments
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
CREATE POLICY agent_notifications_tenant_isolation ON agent_notifications
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
-- 012
CREATE POLICY discovered_machines_tenant_isolation ON discovered_machines
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
-- 013
CREATE POLICY factory_layouts_tenant_isolation ON factory_layouts
    FOR ALL USING (tenant_id = current_setting('app.current_tenant')::UUID);
CREATE POLICY cameras_tenant_isolation ON cameras
    FOR ALL USING (tenant_id = current_setting('app.current_tenant')::UUID);
-- 014
CREATE POLICY printer_profiles_tenant_isolation ON printer_profiles
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
CREATE POLICY printer_connections_tenant_isolation ON printer_connections
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
CREATE POLICY material_profiles_tenant_isolation ON material_profiles
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
CREATE POLICY print_jobs_tenant_isolation ON print_jobs
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
-- 015
CREATE POLICY invoices_tenant_isolation ON invoices
    USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
-- 016
CREATE POLICY tenant_isolation_oee_snapshots ON oee_snapshots FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 017
CREATE POLICY tenant_isolation_maint_schedules ON maintenance_schedules FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_maint_work_orders ON maintenance_work_orders FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 018
CREATE POLICY tenant_isolation_product_defs ON product_definitions FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_bom_items ON bom_items FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_genealogy ON product_genealogy FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_genealogy_material ON genealogy_material_consumption FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 019
CREATE POLICY tenant_isolation_work_instructions ON work_instructions FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_task_work_instructions ON task_work_instructions FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 020
CREATE POLICY tenant_isolation_spc_limits ON spc_control_limits FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_spc_violations ON spc_violations FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 021
CREATE POLICY tenant_isolation_inventory_items ON inventory_items FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_inventory_txn ON inventory_transactions FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
-- 022
CREATE POLICY tenant_isolation_event_outbox ON event_outbox FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_webhook_subscriptions ON webhook_subscriptions FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);
CREATE POLICY tenant_isolation_webhook_deliveries ON webhook_deliveries FOR ALL
    USING (subscription_id IN (
        SELECT id FROM webhook_subscriptions
        WHERE tenant_id = current_setting('app.current_tenant_id', true)::UUID
    ));
-- 023
CREATE POLICY tenant_isolation_api_keys ON api_keys FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

DROP FUNCTION IF EXISTS app_system_scope();
DROP FUNCTION IF EXISTS app_current_tenant_id();

COMMIT;
