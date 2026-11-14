-- 036: operator corrections and disposition of job consumption (RFC 0058
-- wave 1). Quantities only.
--
--   job_consumption.disposition    good, scrap or failed_print (ruling I5:
--                                  scrap and failed prints are
--                                  distinguishable). 'good' for every
--                                  existing row and for any insert that does
--                                  not name it.
--   job_consumption_corrections    an operator correction is a NEW row that
--                                  supersedes an earlier one (append-only:
--                                  history is never updated). consumption_id
--                                  is the recorded fact the chain corrects;
--                                  supersedes_id is that fact or the
--                                  previous correction. One correction per
--                                  row, so the chain's last row is the
--                                  effective one. Stock relief moves by the
--                                  difference.
--   job_consumption_facts (view)   recorded facts and corrections as one set
--                                  of rows (a correction: basis actual,
--                                  source operator, the corrected fact's job,
--                                  kind, material and occurred_at). The
--                                  application reads facts through it.
--
-- COMPATIBLE WITH THE CODE BEFORE IT. job_consumption's fact key and its
-- unique index (033) are untouched, so the insert of the previous release
-- (ON CONFLICT on that key) keeps working whether this migration is applied
-- before the new code deploys or the code rolls back after it. The
-- previous release reads job_consumption only: it sees no corrections and
-- settles stock from the recorded facts, as it always did.
--
-- Dhanam's cost.event contract v1.1 has no field for disposition, so the
-- emitter does not send it (it stays in Pravara).
--
-- Tenant scoping follows 033: the 028 policy, FORCE ROW LEVEL SECURITY (029)
-- and the RESTRICTIVE tenant_references policy for the foreign key. The view
-- runs with the caller's rights (security_invoker), so every row-level
-- policy above applies through it.
BEGIN;

ALTER TABLE job_consumption ADD COLUMN IF NOT EXISTS disposition VARCHAR(16) NOT NULL DEFAULT 'good';
ALTER TABLE job_consumption DROP CONSTRAINT IF EXISTS job_consumption_disposition_check;
ALTER TABLE job_consumption ADD CONSTRAINT job_consumption_disposition_check
    CHECK (disposition IN ('good', 'scrap', 'failed_print'));

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
       OR NEW.disposition IS DISTINCT FROM OLD.disposition
       OR (NEW.dispatch_id IS NOT NULL AND NEW.dispatch_id IS DISTINCT FROM OLD.dispatch_id)
       OR (NEW.print_job_id IS NOT NULL AND NEW.print_job_id IS DISTINCT FROM OLD.print_job_id)
       OR (NEW.task_id IS NOT NULL AND NEW.task_id IS DISTINCT FROM OLD.task_id)
       OR (NEW.order_id IS NOT NULL AND NEW.order_id IS DISTINCT FROM OLD.order_id)
       OR (NEW.machine_id IS NOT NULL AND NEW.machine_id IS DISTINCT FROM OLD.machine_id) THEN
        RAISE EXCEPTION 'job_consumption is append-only';
    END IF;
    RETURN NEW;
END $$;

CREATE TABLE job_consumption_corrections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    -- The recorded fact this chain corrects (its job, kind and material).
    consumption_id UUID NOT NULL REFERENCES job_consumption(id) ON DELETE CASCADE,
    -- The row this correction replaces: consumption_id itself, or the
    -- previous correction of the same chain (checked by a trigger).
    supersedes_id UUID NOT NULL,
    quantity NUMERIC(15,4) NOT NULL CHECK (quantity >= 0),
    unit VARCHAR(8) NOT NULL CHECK (unit IN ('g', 'mm', 'ml', 'sheet', 'min')),
    disposition VARCHAR(16) NOT NULL DEFAULT 'good' CHECK (disposition IN ('good', 'scrap', 'failed_print')),
    note TEXT,
    -- Who corrected it (the actor id; no users(id) reference, as in 033).
    recorded_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- One correction per row: the chain is linear.
CREATE UNIQUE INDEX ux_job_consumption_corrections_supersedes ON job_consumption_corrections (supersedes_id);
CREATE INDEX idx_job_consumption_corrections_consumption ON job_consumption_corrections (consumption_id);

CREATE OR REPLACE FUNCTION job_consumption_corrections_check() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    fact_kind text;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'job_consumption_corrections is append-only';
    END IF;
    SELECT kind INTO fact_kind FROM job_consumption
     WHERE id = NEW.consumption_id AND tenant_id = NEW.tenant_id;
    IF fact_kind IS NULL THEN
        RAISE EXCEPTION 'a correction corrects a recorded fact of the same tenant';
    END IF;
    IF NEW.supersedes_id <> NEW.consumption_id AND NOT EXISTS (
        SELECT 1 FROM job_consumption_corrections p
        WHERE p.id = NEW.supersedes_id AND p.consumption_id = NEW.consumption_id AND p.tenant_id = NEW.tenant_id) THEN
        RAISE EXCEPTION 'a correction supersedes the corrected fact or a correction of the same fact';
    END IF;
    IF (fact_kind = 'machine_time') <> (NEW.unit = 'min') THEN
        RAISE EXCEPTION 'a correction keeps the unit family of its fact (min for machine time)';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER trg_job_consumption_corrections_check
    BEFORE INSERT OR UPDATE ON job_consumption_corrections
    FOR EACH ROW EXECUTE FUNCTION job_consumption_corrections_check();

ALTER TABLE job_consumption_corrections ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_consumption_corrections FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_consumption_corrections FOR ALL
    USING (tenant_id = app_current_tenant_id())
    WITH CHECK (tenant_id = app_current_tenant_id());
CREATE POLICY tenant_references ON job_consumption_corrections AS RESTRICTIVE FOR ALL USING (true)
    WITH CHECK (EXISTS (SELECT 1 FROM job_consumption p
                WHERE p.id = job_consumption_corrections.consumption_id AND p.tenant_id = app_current_tenant_id()));

-- Stock movements posted for a correction reference it (reference_type
-- 'job_consumption', reference_id = the correction).
CREATE VIEW job_consumption_facts WITH (security_invoker = true) AS
    SELECT c.id, c.tenant_id, c.job_kind, c.job_id, c.dispatch_id, c.print_job_id, c.task_id, c.order_id,
           c.machine_id, c.kind, c.material_code, c.material_lot, c.quantity, c.unit, c.basis, c.source, c.note,
           c.recorded_by, c.occurred_at, c.created_at, c.disposition, NULL::uuid AS supersedes_id, c.id AS root_id
    FROM job_consumption c
    UNION ALL
    SELECT k.id, k.tenant_id, c.job_kind, c.job_id, c.dispatch_id, c.print_job_id, c.task_id, c.order_id,
           c.machine_id, c.kind, c.material_code, c.material_lot, k.quantity, k.unit, 'actual', 'operator', k.note,
           k.recorded_by, c.occurred_at, k.created_at, k.disposition, k.supersedes_id, k.consumption_id
    FROM job_consumption_corrections k
    JOIN job_consumption c ON c.id = k.consumption_id AND c.tenant_id = k.tenant_id;

-- Application role grants (same as 029 / infra/db/roles/pravara_app_grants.sql).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON job_consumption_corrections TO pravara_app;
        GRANT SELECT ON job_consumption_facts TO pravara_app;
    ELSE
        RAISE NOTICE '036: role pravara_app does not exist yet; apply infra/db/roles/pravara_app_grants.sql after creating it';
    END IF;
END $$;

COMMIT;
