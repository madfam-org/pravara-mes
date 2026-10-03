-- 028: one row-level security setting name for every tenant table.
--
-- Every tenant policy now reads the transaction-local setting
-- app.current_tenant_id (set by internal/db with
-- set_config('app.current_tenant_id', $1, true)) through
-- app_current_tenant_id(), with both USING and WITH CHECK. Earlier migrations
-- used three different setting names; 011, 012, 013 and 014 are aligned here.
-- An unset or empty setting matches no rows (fail closed) instead of raising a
-- cast error.
--
-- Tables that hold tenant data only through a parent row (order_items,
-- webhook_deliveries, the 013 visualization children and the 014 printer
-- children) get policies through their parent.
--
-- A separate, READ ONLY "system scope" (app.system_scope = 'on', set only by
-- internal/db RunInSystemScope) may SELECT the queue-like tables a background
-- dispatcher or the API-key lookup must scan across tenants. It grants no
-- INSERT, UPDATE or DELETE.
--
-- Global reference tables stay without RLS: tenants, schema_migrations,
-- health_snapshots, protocol_standards, machine_protocols, machine_firmwares,
-- adapter_compatibility, adapter_metrics, protocol_compliance, machine_models,
-- material_simulations, simulation_cache.

BEGIN;

CREATE OR REPLACE FUNCTION app_current_tenant_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT NULLIF(current_setting('app.current_tenant_id', true), '')::uuid $$;

CREATE OR REPLACE FUNCTION app_system_scope() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT COALESCE(current_setting('app.system_scope', true), '') = 'on' $$;

-- Drop the previous tenant policies (names from 001-023).
DROP POLICY IF EXISTS tenant_isolation_users ON users;
DROP POLICY IF EXISTS tenant_isolation_orders ON orders;
DROP POLICY IF EXISTS tenant_isolation_order_items ON order_items;
DROP POLICY IF EXISTS tenant_isolation_machines ON machines;
DROP POLICY IF EXISTS tenant_isolation_tasks ON tasks;
DROP POLICY IF EXISTS tenant_isolation_telemetry ON telemetry;
DROP POLICY IF EXISTS tenant_isolation_audit ON audit_logs;
DROP POLICY IF EXISTS tenant_isolation_quality_certificates ON quality_certificates;
DROP POLICY IF EXISTS tenant_isolation_inspections ON inspections;
DROP POLICY IF EXISTS tenant_isolation_batch_lots ON batch_lots;
DROP POLICY IF EXISTS tenant_isolation_task_commands ON task_commands;
DROP POLICY IF EXISTS agents_tenant_isolation ON agents;
DROP POLICY IF EXISTS agent_machines_tenant_isolation ON agent_machines;
DROP POLICY IF EXISTS agent_assignments_tenant_isolation ON agent_assignments;
DROP POLICY IF EXISTS agent_notifications_tenant_isolation ON agent_notifications;
DROP POLICY IF EXISTS discovered_machines_tenant_isolation ON discovered_machines;
DROP POLICY IF EXISTS factory_layouts_tenant_isolation ON factory_layouts;
DROP POLICY IF EXISTS cameras_tenant_isolation ON cameras;
DROP POLICY IF EXISTS printer_profiles_tenant_isolation ON printer_profiles;
DROP POLICY IF EXISTS printer_connections_tenant_isolation ON printer_connections;
DROP POLICY IF EXISTS material_profiles_tenant_isolation ON material_profiles;
DROP POLICY IF EXISTS print_jobs_tenant_isolation ON print_jobs;
DROP POLICY IF EXISTS invoices_tenant_isolation ON invoices;
DROP POLICY IF EXISTS tenant_isolation_oee_snapshots ON oee_snapshots;
DROP POLICY IF EXISTS tenant_isolation_maint_schedules ON maintenance_schedules;
DROP POLICY IF EXISTS tenant_isolation_maint_work_orders ON maintenance_work_orders;
DROP POLICY IF EXISTS tenant_isolation_product_defs ON product_definitions;
DROP POLICY IF EXISTS tenant_isolation_bom_items ON bom_items;
DROP POLICY IF EXISTS tenant_isolation_genealogy ON product_genealogy;
DROP POLICY IF EXISTS tenant_isolation_genealogy_material ON genealogy_material_consumption;
DROP POLICY IF EXISTS tenant_isolation_work_instructions ON work_instructions;
DROP POLICY IF EXISTS tenant_isolation_task_work_instructions ON task_work_instructions;
DROP POLICY IF EXISTS tenant_isolation_spc_limits ON spc_control_limits;
DROP POLICY IF EXISTS tenant_isolation_spc_violations ON spc_violations;
DROP POLICY IF EXISTS tenant_isolation_inventory_items ON inventory_items;
DROP POLICY IF EXISTS tenant_isolation_inventory_txn ON inventory_transactions;
DROP POLICY IF EXISTS tenant_isolation_event_outbox ON event_outbox;
DROP POLICY IF EXISTS tenant_isolation_webhook_subscriptions ON webhook_subscriptions;
DROP POLICY IF EXISTS tenant_isolation_webhook_deliveries ON webhook_deliveries;
DROP POLICY IF EXISTS tenant_isolation_api_keys ON api_keys;

-- Tables with their own tenant_id column.
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'users', 'orders', 'machines', 'tasks', 'telemetry', 'audit_logs',
        'quality_certificates', 'inspections', 'batch_lots', 'task_commands',
        'agents', 'agent_machines', 'agent_assignments', 'agent_notifications',
        'discovered_machines', 'factory_layouts', 'cameras',
        'printer_profiles', 'printer_connections', 'material_profiles', 'print_jobs',
        'invoices', 'oee_snapshots', 'maintenance_schedules', 'maintenance_work_orders',
        'product_definitions', 'bom_items', 'product_genealogy', 'genealogy_material_consumption',
        'work_instructions', 'task_work_instructions', 'spc_control_limits', 'spc_violations',
        'inventory_items', 'inventory_transactions', 'event_outbox', 'webhook_subscriptions',
        'api_keys'
    ]
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I FOR ALL '
            'USING (tenant_id = app_current_tenant_id()) '
            'WITH CHECK (tenant_id = app_current_tenant_id())', t);
    END LOOP;
END $$;

-- Tables scoped through a parent row.
CREATE POLICY tenant_isolation ON order_items FOR ALL
    USING (EXISTS (SELECT 1 FROM orders p WHERE p.id = order_items.order_id AND p.tenant_id = app_current_tenant_id()))
    WITH CHECK (EXISTS (SELECT 1 FROM orders p WHERE p.id = order_items.order_id AND p.tenant_id = app_current_tenant_id()));

CREATE POLICY tenant_isolation ON webhook_deliveries FOR ALL
    USING (EXISTS (SELECT 1 FROM webhook_subscriptions p WHERE p.id = webhook_deliveries.subscription_id AND p.tenant_id = app_current_tenant_id()))
    WITH CHECK (EXISTS (SELECT 1 FROM webhook_subscriptions p WHERE p.id = webhook_deliveries.subscription_id AND p.tenant_id = app_current_tenant_id()));

DO $$
DECLARE
    t text;
BEGIN
    -- children of machines (machine_id NOT NULL)
    FOREACH t IN ARRAY ARRAY['machine_model_associations', 'digital_twin_calibrations', 'machine_positions']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %1$I FOR ALL '
            'USING (EXISTS (SELECT 1 FROM machines p WHERE p.id = %1$I.machine_id AND p.tenant_id = app_current_tenant_id())) '
            'WITH CHECK (EXISTS (SELECT 1 FROM machines p WHERE p.id = %1$I.machine_id AND p.tenant_id = app_current_tenant_id()))', t);
    END LOOP;

    -- children of printer_connections
    FOREACH t IN ARRAY ARRAY['printer_connection_logs', 'printer_maintenance']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %1$I FOR ALL '
            'USING (EXISTS (SELECT 1 FROM printer_connections p WHERE p.id = %1$I.connection_id AND p.tenant_id = app_current_tenant_id())) '
            'WITH CHECK (EXISTS (SELECT 1 FROM printer_connections p WHERE p.id = %1$I.connection_id AND p.tenant_id = app_current_tenant_id()))', t);
    END LOOP;

    -- existing partitions of machine_positions (queries through the parent
    -- use the parent's policy; direct partition access uses these)
    FOR t IN
        SELECT c.relname FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        JOIN pg_class p ON p.oid = i.inhparent
        WHERE p.relname = 'machine_positions'
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %1$I FOR ALL '
            'USING (EXISTS (SELECT 1 FROM machines p WHERE p.id = %1$I.machine_id AND p.tenant_id = app_current_tenant_id())) '
            'WITH CHECK (EXISTS (SELECT 1 FROM machines p WHERE p.id = %1$I.machine_id AND p.tenant_id = app_current_tenant_id()))', t);
    END LOOP;
END $$;

ALTER TABLE video_recordings ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON video_recordings FOR ALL
    USING (EXISTS (SELECT 1 FROM cameras p WHERE p.id = video_recordings.camera_id AND p.tenant_id = app_current_tenant_id()))
    WITH CHECK (EXISTS (SELECT 1 FROM cameras p WHERE p.id = video_recordings.camera_id AND p.tenant_id = app_current_tenant_id()));

-- gcode_simulations: machine_id and task_id are both optional; every parent
-- that is set must belong to the current tenant, and at least one must be set.
ALTER TABLE gcode_simulations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON gcode_simulations FOR ALL
    USING (
        (machine_id IS NOT NULL OR task_id IS NOT NULL)
        AND (machine_id IS NULL OR EXISTS (SELECT 1 FROM machines p WHERE p.id = gcode_simulations.machine_id AND p.tenant_id = app_current_tenant_id()))
        AND (task_id IS NULL OR EXISTS (SELECT 1 FROM tasks p WHERE p.id = gcode_simulations.task_id AND p.tenant_id = app_current_tenant_id()))
    )
    WITH CHECK (
        (machine_id IS NOT NULL OR task_id IS NOT NULL)
        AND (machine_id IS NULL OR EXISTS (SELECT 1 FROM machines p WHERE p.id = gcode_simulations.machine_id AND p.tenant_id = app_current_tenant_id()))
        AND (task_id IS NULL OR EXISTS (SELECT 1 FROM tasks p WHERE p.id = gcode_simulations.task_id AND p.tenant_id = app_current_tenant_id()))
    );

-- References must stay inside the tenant. Foreign-key checks ignore RLS, so
-- without this a tenant could create its own rows pointing at another
-- tenant's parent rows (and the parent's ON DELETE CASCADE would then reach
-- across tenants). For every single-column foreign key from a tenant table
-- to a tenant table, a RESTRICTIVE policy requires new and updated rows to
-- reference a parent of the current tenant. Parents without their own
-- tenant_id (order_items) are checked through their order. Reads are not
-- affected (USING true).
DO $$
DECLARE
    child regclass;
    checks text;
BEGIN
    FOR child, checks IN
        SELECT c.conrelid::regclass,
               string_agg(
                   CASE
                       WHEN c.confrelid = 'order_items'::regclass THEN format(
                           '(%1$I IS NULL OR EXISTS (SELECT 1 FROM order_items p JOIN orders o ON o.id = p.order_id '
                           'WHERE p.id = %2$s.%1$I AND o.tenant_id = app_current_tenant_id()))',
                           a.attname, c.conrelid::regclass)
                       ELSE format(
                           '(%1$I IS NULL OR EXISTS (SELECT 1 FROM %3$s p WHERE p.%4$I = %2$s.%1$I '
                           'AND p.tenant_id = app_current_tenant_id()))',
                           a.attname, c.conrelid::regclass, c.confrelid::regclass, pa.attname)
                   END,
                   ' AND ' ORDER BY a.attname)
        FROM pg_constraint c
        JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
        JOIN pg_attribute pa ON pa.attrelid = c.confrelid AND pa.attnum = c.confkey[1]
        JOIN pg_class cc ON cc.oid = c.conrelid
        WHERE c.contype = 'f'
          AND c.connamespace = 'public'::regnamespace
          AND array_length(c.conkey, 1) = 1
          AND cc.relrowsecurity
          AND NOT cc.relispartition
          AND (c.confrelid = 'order_items'::regclass
               OR EXISTS (SELECT 1 FROM pg_attribute t
                          WHERE t.attrelid = c.confrelid AND t.attname = 'tenant_id' AND NOT t.attisdropped))
        GROUP BY c.conrelid
    LOOP
        EXECUTE format(
            'CREATE POLICY tenant_references ON %s AS RESTRICTIVE FOR ALL USING (true) WITH CHECK (%s)',
            child, checks);
    END LOOP;
END $$;

-- Read-only system scope for cross-tenant discovery.
CREATE POLICY system_scope_read ON event_outbox FOR SELECT USING (app_system_scope());
CREATE POLICY system_scope_read ON webhook_subscriptions FOR SELECT USING (app_system_scope());
CREATE POLICY system_scope_read ON webhook_deliveries FOR SELECT USING (app_system_scope());
CREATE POLICY system_scope_read ON api_keys FOR SELECT USING (app_system_scope());

COMMIT;
