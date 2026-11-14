-- 036: operator corrections and disposition of job consumption (RFC 0058
-- wave 1). Quantities only.
--
--   job_consumption.supersedes_id  a correction is a NEW row that supersedes
--                                  an earlier one (append-only: history is
--                                  never updated). A correction is always
--                                  an operator actual; one row is superseded
--                                  at most once, so corrections form a
--                                  chain whose last row is the effective
--                                  one. Stock relief moves by the
--                                  difference.
--   job_consumption.disposition    good, scrap or failed_print (ruling I5:
--                                  scrap and failed prints are
--                                  distinguishable). Set by the operator at
--                                  report, correction or print-job
--                                  completion; 'good' for every existing row.
--
-- The fact key (job, kind, basis, source, material) stays unique among
-- original facts only; correction rows are keyed by what they supersede.
-- Dhanam's cost.event contract v1.1 has no field for disposition, so the
-- emitter does not send it (it stays in Pravara).
--
-- Additive: two columns (existing rows get disposition 'good' and no
-- supersedes_id), a narrower unique index plus one new index, and the
-- append-only trigger extended to the new columns.
BEGIN;

-- NO ACTION (checked at the end of the statement), so deleting a tenant,
-- which cascades to all its rows at once, still works.
ALTER TABLE job_consumption ADD COLUMN IF NOT EXISTS supersedes_id UUID
    CONSTRAINT job_consumption_supersedes_id_fkey REFERENCES job_consumption(id);
ALTER TABLE job_consumption ADD COLUMN IF NOT EXISTS disposition VARCHAR(16) NOT NULL DEFAULT 'good';
ALTER TABLE job_consumption DROP CONSTRAINT IF EXISTS job_consumption_disposition_check;
ALTER TABLE job_consumption ADD CONSTRAINT job_consumption_disposition_check
    CHECK (disposition IN ('good', 'scrap', 'failed_print'));
ALTER TABLE job_consumption DROP CONSTRAINT IF EXISTS job_consumption_correction_check;
ALTER TABLE job_consumption ADD CONSTRAINT job_consumption_correction_check
    CHECK (supersedes_id IS NULL OR (basis = 'actual' AND source = 'operator' AND supersedes_id <> id));

DROP INDEX IF EXISTS ux_job_consumption_fact;
CREATE UNIQUE INDEX ux_job_consumption_fact ON job_consumption
    (tenant_id, job_kind, job_id, kind, basis, source, (COALESCE(material_code, '')))
    WHERE supersedes_id IS NULL;
CREATE UNIQUE INDEX ux_job_consumption_supersedes ON job_consumption (supersedes_id)
    WHERE supersedes_id IS NOT NULL;

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
       OR NEW.supersedes_id IS DISTINCT FROM OLD.supersedes_id
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

-- A correction supersedes a row of the same tenant, job, kind and material
-- (a trigger, not a policy: a policy on job_consumption that reads
-- job_consumption would recurse).
CREATE OR REPLACE FUNCTION job_consumption_correction_target() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.supersedes_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM job_consumption p
        WHERE p.id = NEW.supersedes_id AND p.tenant_id = NEW.tenant_id
          AND p.job_kind = NEW.job_kind AND p.job_id = NEW.job_id AND p.kind = NEW.kind
          AND COALESCE(p.material_code, '') = COALESCE(NEW.material_code, '')) THEN
        RAISE EXCEPTION 'a correction supersedes a row of the same tenant, job, kind and material';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER trg_job_consumption_correction_target
    BEFORE INSERT ON job_consumption
    FOR EACH ROW EXECUTE FUNCTION job_consumption_correction_target();

COMMIT;
