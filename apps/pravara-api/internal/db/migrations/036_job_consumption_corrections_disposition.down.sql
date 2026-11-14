-- 036 (down): remove corrections and disposition. Corrections are dropped
-- with their table (export them first if they matter); the stock movements
-- they posted stay in inventory_transactions with a reference_id that no
-- longer resolves, and quantity_on_hand is not rewound. Disposition is
-- dropped. job_consumption's fact key was never changed, so nothing else
-- needs restoring.
BEGIN;

DROP VIEW IF EXISTS job_consumption_facts;
DROP TABLE IF EXISTS job_consumption_corrections;
DROP FUNCTION IF EXISTS job_consumption_corrections_check();

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

ALTER TABLE job_consumption DROP CONSTRAINT IF EXISTS job_consumption_disposition_check;
ALTER TABLE job_consumption DROP COLUMN IF EXISTS disposition;

COMMIT;
