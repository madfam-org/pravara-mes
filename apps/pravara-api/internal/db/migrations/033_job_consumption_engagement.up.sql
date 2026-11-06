-- 033: what each job consumed, and the engagement an order belongs to.
--
--   orders.engagement_slug  the engagement an order is attributed to (one
--                           slug shared across the ecosystem's systems;
--                           lowercase letters, digits and hyphens, 1-63
--                           characters, starting with a letter or digit).
--   job_consumption         append-only quantities per completed job:
--                           material (g, mm, ml, sheet) and machine time
--                           (min), each with its basis (actual or estimate)
--                           and source (printer, operator or slicer).
--                           Quantities only: no cost or money.
--
-- A job is a fabrication dispatch (dispatch_jobs), a print job (print_jobs)
-- or a task without either (job_kind 'task', operator reports). job_id is
-- that row's id and, with kind, basis, source and material_code, keys one
-- fact: recording the same fact again is a no-op.
--
-- Stock relief for a job and material is posted to inventory_transactions
-- (reference_type 'job_consumption', reference_id = the job_consumption row)
-- by the application, never by this migration.
--
-- DEPLOY ORDER: apply this migration (after 028-032) before deploying code
-- that reads orders.engagement_slug: the order repository selects it.
--
-- Tenant scoping follows 032: the 028 policy (USING and WITH CHECK on
-- app_current_tenant_id()), FORCE ROW LEVEL SECURITY (029) and the 028
-- RESTRICTIVE tenant_references policy for its foreign keys.
BEGIN;

ALTER TABLE orders ADD COLUMN IF NOT EXISTS engagement_slug TEXT;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_engagement_slug_format;
ALTER TABLE orders ADD CONSTRAINT orders_engagement_slug_format
    CHECK (engagement_slug ~ '^[a-z0-9][a-z0-9-]{0,62}$');
CREATE INDEX IF NOT EXISTS idx_orders_engagement_slug ON orders (tenant_id, engagement_slug)
    WHERE engagement_slug IS NOT NULL;

COMMENT ON COLUMN orders.engagement_slug IS
    'Engagement the order is attributed to (^[a-z0-9][a-z0-9-]{0,62}$); NULL when unattributed';

CREATE TABLE job_consumption (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    job_kind VARCHAR(16) NOT NULL CHECK (job_kind IN ('dispatch', 'print_job', 'task')),
    job_id UUID NOT NULL,
    dispatch_id UUID REFERENCES dispatch_jobs(id) ON DELETE SET NULL,
    print_job_id UUID REFERENCES print_jobs(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('material', 'machine_time')),
    -- material only: what was consumed (the inventory SKU when the material
    -- maps to an inventory item, else the material class) and its lot.
    material_code TEXT,
    material_lot TEXT,
    quantity NUMERIC(15,4) NOT NULL CHECK (quantity >= 0),
    unit VARCHAR(8) NOT NULL CHECK (unit IN ('g', 'mm', 'ml', 'sheet', 'min')),
    basis VARCHAR(8) NOT NULL CHECK (basis IN ('actual', 'estimate')),
    source VARCHAR(16) NOT NULL CHECK (source IN ('printer', 'operator', 'slicer')),
    -- How the quantity was derived (e.g. "kept in mm: the material profile
    -- has no diameter or density").
    note TEXT,
    -- Who reported it (operator reports); no users(id) reference, as in 032.
    recorded_by UUID,
    -- When the consumption happened (the job's completion), for period cut-off.
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((kind = 'machine_time') = (unit = 'min')),
    CHECK (kind = 'material' OR (material_code IS NULL AND material_lot IS NULL)),
    CHECK (kind = 'machine_time' OR (material_code IS NOT NULL AND material_code <> '')),
    -- The job's own link is job_id itself (until ON DELETE SET NULL clears it).
    CHECK (job_kind = 'dispatch' OR dispatch_id IS NULL),
    CHECK (job_kind = 'print_job' OR print_job_id IS NULL),
    CHECK (job_kind <> 'dispatch' OR dispatch_id IS NULL OR dispatch_id = job_id),
    CHECK (job_kind <> 'print_job' OR print_job_id IS NULL OR print_job_id = job_id),
    CHECK (job_kind <> 'task' OR task_id IS NULL OR task_id = job_id)
);

-- One fact per job, kind, basis, source and material.
CREATE UNIQUE INDEX ux_job_consumption_fact ON job_consumption
    (tenant_id, job_kind, job_id, kind, basis, source, (COALESCE(material_code, '')));
CREATE INDEX idx_job_consumption_tenant_occurred ON job_consumption (tenant_id, occurred_at, id);
CREATE INDEX idx_job_consumption_task ON job_consumption (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX idx_job_consumption_order ON job_consumption (order_id) WHERE order_id IS NOT NULL;

-- Stock movements posted for job consumption, found by their consumption row.
CREATE INDEX IF NOT EXISTS idx_inventory_txn_job_consumption ON inventory_transactions (reference_id)
    WHERE reference_type = 'job_consumption';

-- Facts are never rewritten. Only the ON DELETE SET NULL links may change,
-- so deleting a task, order, machine or job still works (032 pattern).
CREATE OR REPLACE FUNCTION job_consumption_append_only() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.job_kind IS DISTINCT FROM OLD.job_kind
       OR NEW.job_id IS DISTINCT FROM OLD.job_id
       OR NEW.kind IS DISTINCT FROM OLD.kind
       OR NEW.material_code IS DISTINCT FROM OLD.material_code
       OR NEW.material_lot IS DISTINCT FROM OLD.material_lot
       OR NEW.quantity IS DISTINCT FROM OLD.quantity
       OR NEW.unit IS DISTINCT FROM OLD.unit
       OR NEW.basis IS DISTINCT FROM OLD.basis
       OR NEW.source IS DISTINCT FROM OLD.source
       OR NEW.note IS DISTINCT FROM OLD.note
       OR NEW.recorded_by IS DISTINCT FROM OLD.recorded_by
       OR NEW.occurred_at IS DISTINCT FROM OLD.occurred_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR (NEW.dispatch_id IS NOT NULL AND NEW.dispatch_id IS DISTINCT FROM OLD.dispatch_id)
       OR (NEW.print_job_id IS NOT NULL AND NEW.print_job_id IS DISTINCT FROM OLD.print_job_id)
       OR (NEW.task_id IS NOT NULL AND NEW.task_id IS DISTINCT FROM OLD.task_id)
       OR (NEW.order_id IS NOT NULL AND NEW.order_id IS DISTINCT FROM OLD.order_id)
       OR (NEW.machine_id IS NOT NULL AND NEW.machine_id IS DISTINCT FROM OLD.machine_id) THEN
        RAISE EXCEPTION 'job_consumption is append-only';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER trg_job_consumption_append_only
    BEFORE UPDATE ON job_consumption
    FOR EACH ROW EXECUTE FUNCTION job_consumption_append_only();

-- Row-level security (028/029 pattern).
ALTER TABLE job_consumption ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_consumption FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_consumption FOR ALL
    USING (tenant_id = app_current_tenant_id())
    WITH CHECK (tenant_id = app_current_tenant_id());

-- References stay inside the tenant (same generator as 028/032, limited to
-- job_consumption).
DO $$
DECLARE
    checks text;
BEGIN
    SELECT string_agg(
               format('(%1$I IS NULL OR EXISTS (SELECT 1 FROM %2$s p WHERE p.%3$I = job_consumption.%1$I '
                      'AND p.tenant_id = app_current_tenant_id()))',
                      a.attname, c.confrelid::regclass, pa.attname),
               ' AND ' ORDER BY a.attname)
      INTO checks
    FROM pg_constraint c
    JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
    JOIN pg_attribute pa ON pa.attrelid = c.confrelid AND pa.attnum = c.confkey[1]
    WHERE c.contype = 'f'
      AND array_length(c.conkey, 1) = 1
      AND c.conrelid = 'job_consumption'::regclass
      AND c.confrelid <> 'tenants'::regclass
      AND EXISTS (SELECT 1 FROM pg_attribute t
                  WHERE t.attrelid = c.confrelid AND t.attname = 'tenant_id' AND NOT t.attisdropped);
    EXECUTE format(
        'CREATE POLICY tenant_references ON job_consumption AS RESTRICTIVE FOR ALL USING (true) WITH CHECK (%s)',
        checks);
END $$;

-- Application role grants (same as 029 / infra/db/roles/pravara_app_grants.sql).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON job_consumption TO pravara_app;
    ELSE
        RAISE NOTICE '033: role pravara_app does not exist yet; apply infra/db/roles/pravara_app_grants.sql after creating it';
    END IF;
END $$;

COMMIT;
