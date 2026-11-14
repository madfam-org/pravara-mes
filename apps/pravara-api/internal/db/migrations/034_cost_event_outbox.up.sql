-- 034: durable outbox for the cost events Pravara sends to Dhanam.
--
-- Pravara sends each completed job's material consumption to Dhanam's cost
-- ledger as a quantity-only cost.event.v1 `consumption` event (RFC 0050,
-- owner ruling C12: Pravara never values consumption; Dhanam does). One row
-- is one event: one job and material code, keyed by the event's
-- deterministic id. The emitter (internal/costevents) rebuilds a row's body
-- from job_consumption and resends it only when the body changes; Dhanam is
-- idempotent on (source, id), so a correction updates the same event.
--
--   body / body_sha256   the event as sent (quantities only: the event type
--                        has no money fields) and the hash of its bytes
--   status               pending   due for sending (again)
--                        delivered Dhanam stored this exact body
--                        rejected  the contract refuses this body (locally
--                                  or Dhanam's 400); waits for a new body
--   attempts, next_attempt_at, last_error
--                        retry with backoff; never dropped. last_error holds
--                        reason codes only, never a payload or a secret
--   leased_until         a POST is in flight until then: no other sender
--                        (replica) claims the row, even if its body changed
--   delivered_sha256, delivered_at, dhanam_outcome
--                        what Dhanam last stored (created/updated/unchanged)
--
-- Nothing reads or writes this table unless PRAVARA_COST_EVENTS_ENABLED is
-- on. Apply it before the owner turns the switch on; the deploy does not
-- apply migrations (docs/operations/cost-events.md).
--
-- Forward-only and additive: a new table, no existing row changes. Tenant
-- scoping follows 033: the 028 policy, FORCE ROW LEVEL SECURITY (029). Its
-- only foreign key is tenants, so it needs no tenant_references policy.
BEGIN;

CREATE TABLE cost_event_outbox (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL CHECK (event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    job_kind VARCHAR(16) NOT NULL CHECK (job_kind IN ('dispatch', 'print_job', 'task')),
    job_id UUID NOT NULL,
    material_code TEXT NOT NULL,
    body JSONB NOT NULL,
    body_sha256 CHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'rejected')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    leased_until TIMESTAMPTZ,
    last_error TEXT,
    delivered_sha256 CHAR(64),
    delivered_at TIMESTAMPTZ,
    dhanam_outcome VARCHAR(16),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, event_id),
    -- Quantities only, enforced by the database too (C12).
    CHECK (NOT (body ?| ARRAY['amount_minor', 'unit_value_minor', 'fx', 'taxes']))
);

CREATE INDEX idx_cost_event_outbox_due ON cost_event_outbox (tenant_id, next_attempt_at, event_id)
    WHERE status = 'pending';
CREATE INDEX idx_cost_event_outbox_job ON cost_event_outbox (tenant_id, job_kind, job_id);

-- Row-level security (028/029 pattern).
ALTER TABLE cost_event_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE cost_event_outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cost_event_outbox FOR ALL
    USING (tenant_id = app_current_tenant_id())
    WITH CHECK (tenant_id = app_current_tenant_id());

-- Application role grants (same as 029 / infra/db/roles/pravara_app_grants.sql).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON cost_event_outbox TO pravara_app;
    ELSE
        RAISE NOTICE '034: role pravara_app does not exist yet; apply infra/db/roles/pravara_app_grants.sql after creating it';
    END IF;
END $$;

COMMIT;
