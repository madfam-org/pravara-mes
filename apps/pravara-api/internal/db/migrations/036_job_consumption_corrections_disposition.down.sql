-- 036 (down): remove corrections and disposition. Correction rows are
-- DELETED (the 033 fact key cannot hold them), and the stock movements they
-- posted stay in inventory_transactions with a reference_id that no longer
-- resolves; quantity_on_hand is not rewound. Export corrections first if
-- they matter. Disposition is dropped.
BEGIN;

DROP TRIGGER IF EXISTS trg_job_consumption_correction_target ON job_consumption;
DROP FUNCTION IF EXISTS job_consumption_correction_target();

ALTER TABLE job_consumption DROP CONSTRAINT IF EXISTS job_consumption_supersedes_id_fkey;

-- One tenant at a time with the tenant set (FORCE ROW LEVEL SECURITY
-- applies to the migration owner too).
DO $$
DECLARE
    tid uuid;
BEGIN
    FOR tid IN SELECT id FROM tenants ORDER BY id
    LOOP
        PERFORM set_config('app.current_tenant_id', tid::text, true);
        DELETE FROM job_consumption WHERE tenant_id = tid AND supersedes_id IS NOT NULL;
    END LOOP;
    PERFORM set_config('app.current_tenant_id', '', true);
END $$;

DROP INDEX IF EXISTS ux_job_consumption_supersedes;
DROP INDEX IF EXISTS ux_job_consumption_fact;
CREATE UNIQUE INDEX ux_job_consumption_fact ON job_consumption
    (tenant_id, job_kind, job_id, kind, basis, source, (COALESCE(material_code, '')));

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

ALTER TABLE job_consumption DROP CONSTRAINT IF EXISTS job_consumption_correction_check;
ALTER TABLE job_consumption DROP CONSTRAINT IF EXISTS job_consumption_disposition_check;
ALTER TABLE job_consumption DROP COLUMN IF EXISTS disposition;
ALTER TABLE job_consumption DROP COLUMN IF EXISTS supersedes_id;

COMMIT;
