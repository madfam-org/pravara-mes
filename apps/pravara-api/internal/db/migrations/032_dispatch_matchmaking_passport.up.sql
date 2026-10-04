-- 032: fabrication dispatch (MES-1 §5-§7).
--
--   dispatch_jobs          one record per dispatched task: requirement snapshot,
--                          match explanation, render bundle, slice job, the
--                          start_job command id, and per-hop state and errors.
--   machine_reservations   a machine held for one dispatch until a TTL, so two
--                          dispatches never pick the same idle machine.
--   manufacturing_records  the append-only record written when a dispatched job
--                          completes (bound to the issuing command and machine).
--   passport_outbox        durable delivery of the instance shell and passport
--                          events to the asset shell service; a failed delivery
--                          never loses the record.
--
-- Every table is tenant-scoped with the 028 policy (USING and WITH CHECK on
-- app_current_tenant_id()), FORCE ROW LEVEL SECURITY (029), and the 028
-- RESTRICTIVE tenant_references policy for its foreign keys.
BEGIN;

CREATE TABLE dispatch_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
    order_item_id UUID REFERENCES order_items(id) ON DELETE SET NULL,
    product_definition_id UUID REFERENCES product_definitions(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'reserved', 'rendered', 'slicing', 'sliced',
                          'enqueuing', 'command_enqueued', 'completed', 'failed', 'cancelled')),
    type_shell_id TEXT,
    part TEXT,
    requirements JSONB,
    match_result JSONB,
    render_bundle JSONB,
    slice_result JSONB,
    command_id UUID UNIQUE,
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner TEXT,
    lease_until TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    error_retryable BOOLEAN,
    -- Who asked: the person's id and the actor id (middleware.ActorUUID).
    -- No users(id) reference: Janua subjects need not have a users row.
    requested_by UUID,
    requested_by_actor UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

-- One live dispatch per task.
CREATE UNIQUE INDEX ux_dispatch_jobs_live_task ON dispatch_jobs (task_id)
    WHERE status NOT IN ('completed', 'failed', 'cancelled');
CREATE INDEX idx_dispatch_jobs_due ON dispatch_jobs (tenant_id, next_attempt_at)
    WHERE status NOT IN ('completed', 'failed', 'cancelled');
CREATE INDEX idx_dispatch_jobs_tenant_created ON dispatch_jobs (tenant_id, created_at DESC);

CREATE TABLE machine_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    dispatch_id UUID NOT NULL REFERENCES dispatch_jobs(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released', 'expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    release_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ
);

-- At most one active reservation per machine. Expired rows are flipped to
-- 'expired' by the acquiring transaction before it inserts.
CREATE UNIQUE INDEX ux_machine_reservations_active ON machine_reservations (machine_id)
    WHERE status = 'active';
CREATE INDEX idx_machine_reservations_dispatch ON machine_reservations (dispatch_id);

CREATE TABLE manufacturing_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    dispatch_id UUID NOT NULL UNIQUE REFERENCES dispatch_jobs(id) ON DELETE RESTRICT,
    command_id UUID NOT NULL UNIQUE,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL,
    genealogy_id UUID REFERENCES product_genealogy(id) ON DELETE SET NULL,
    instance_uuid UUID NOT NULL UNIQUE,
    completion_event_id UUID,
    record JSONB NOT NULL,
    record_sha256 VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The record is a fact: it is never rewritten. Later facts are passport
-- events (passport_outbox rows of kind 'event'). Only the ON DELETE SET NULL
-- links may change, so deleting a task or machine still works.
CREATE OR REPLACE FUNCTION manufacturing_records_append_only() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.record IS DISTINCT FROM OLD.record
       OR NEW.record_sha256 IS DISTINCT FROM OLD.record_sha256
       OR NEW.command_id IS DISTINCT FROM OLD.command_id
       OR NEW.dispatch_id IS DISTINCT FROM OLD.dispatch_id
       OR NEW.instance_uuid IS DISTINCT FROM OLD.instance_uuid
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'manufacturing_records are append-only';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER trg_manufacturing_records_append_only
    BEFORE UPDATE ON manufacturing_records
    FOR EACH ROW EXECUTE FUNCTION manufacturing_records_append_only();

CREATE TABLE passport_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    manufacturing_record_id UUID NOT NULL REFERENCES manufacturing_records(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('instance', 'event')),
    sequence INT NOT NULL,
    event_id UUID UNIQUE,
    payload JSONB NOT NULL,
    payload_sha256 VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT,
    last_status_code INT,
    response JSONB,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (manufacturing_record_id, sequence),
    CHECK ((kind = 'event') = (event_id IS NOT NULL))
);

CREATE INDEX idx_passport_outbox_due ON passport_outbox (tenant_id, next_attempt_at)
    WHERE status = 'pending';

-- Row-level security (028/029 pattern).
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dispatch_jobs', 'machine_reservations', 'manufacturing_records', 'passport_outbox']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I FOR ALL '
            'USING (tenant_id = app_current_tenant_id()) '
            'WITH CHECK (tenant_id = app_current_tenant_id())', t);
    END LOOP;
END $$;

-- References stay inside the tenant (same generator as 028, limited to the
-- tables created here).
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
        WHERE c.contype = 'f'
          AND array_length(c.conkey, 1) = 1
          AND c.conrelid IN ('dispatch_jobs'::regclass, 'machine_reservations'::regclass,
                             'manufacturing_records'::regclass, 'passport_outbox'::regclass)
          AND c.confrelid <> 'tenants'::regclass
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

-- Application role grants (same as 029 / infra/db/roles/pravara_app_grants.sql).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE
            ON dispatch_jobs, machine_reservations, manufacturing_records, passport_outbox
            TO pravara_app;
    ELSE
        RAISE NOTICE '032: role pravara_app does not exist yet; apply infra/db/roles/pravara_app_grants.sql after creating it';
    END IF;
END $$;

COMMIT;
