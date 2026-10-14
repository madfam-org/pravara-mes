-- 031: Sparkplug B registry, edge-node credentials, discovery quarantine and
-- live machine state (MES-1 §1–§3).
--
--   * machines.sparkplug_edge_id: the site edge node a machine is attached
--     to. The Sparkplug device_id is the machine code, so a device is
--     (tenant, edge node, code).
--   * edge_nodes: one MQTT credential per site edge node. Only a password
--     hash is stored. disabled_at revokes the credential.
--   * edge_enrollments: pending enrollments. A site box generates its own
--     credential, registers the hash under a short non-secret user code, and
--     a tenant admin approves it. Rows of other tenants are invisible to the
--     approving admin (row-level security).
--   * discovered_machines: DBIRTHs for devices that are not registered land
--     here (quarantine). They are never trusted automatically.
--   * machine_live_state: the latest Sparkplug state per registered device,
--     written by the telemetry worker's primary host application.
--
-- Every new table follows 028/029: tenant_isolation (USING + WITH CHECK on
-- app_current_tenant_id()), FORCE ROW LEVEL SECURITY, tenant_references on
-- tenant foreign keys, and DML grants for pravara_app. edge_nodes and
-- edge_enrollments also get the read-only system_scope_read policy: the
-- broker credential lookup and the enrollment status poll know a username or
-- an enrollment id, not a tenant.

BEGIN;

-- ---------------------------------------------------------------------------
-- machines: Sparkplug edge node binding
-- ---------------------------------------------------------------------------
ALTER TABLE machines ADD COLUMN IF NOT EXISTS sparkplug_edge_id TEXT;
ALTER TABLE machines ADD CONSTRAINT machines_sparkplug_edge_id_check
    CHECK (sparkplug_edge_id IS NULL OR (sparkplug_edge_id <> '' AND sparkplug_edge_id !~ '[+/#]'));
ALTER TABLE machines ADD CONSTRAINT machines_sparkplug_device_key
    UNIQUE (tenant_id, sparkplug_edge_id, code);

-- ---------------------------------------------------------------------------
-- edge_nodes: per-site MQTT credentials
-- ---------------------------------------------------------------------------
CREATE TABLE edge_nodes (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    edge_node_id          TEXT NOT NULL CHECK (edge_node_id <> '' AND edge_node_id !~ '[+/#]'),
    mqtt_username         TEXT NOT NULL UNIQUE CHECK (mqtt_username <> ''),
    password_hash         TEXT NOT NULL CHECK (password_hash <> ''),
    credential_rotated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Audit identity of the approving person ("user:<id>"); not a users(id)
    -- reference because Janua subjects need not have a users row.
    approved_by           TEXT,
    disabled_at           TIMESTAMPTZ,
    online                BOOLEAN NOT NULL DEFAULT FALSE,
    last_bdseq            BIGINT CHECK (last_bdseq BETWEEN 0 AND 255),
    last_birth_at         TIMESTAMPTZ,
    last_death_at         TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, edge_node_id)
);

-- ---------------------------------------------------------------------------
-- edge_enrollments: credential registrations awaiting approval
-- ---------------------------------------------------------------------------
CREATE TABLE edge_enrollments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    edge_node_id  TEXT NOT NULL CHECK (edge_node_id <> '' AND edge_node_id !~ '[+/#]'),
    mqtt_username TEXT NOT NULL CHECK (mqtt_username <> ''),
    -- Cleared ('') once the enrollment is decided or expired.
    password_hash TEXT NOT NULL,
    user_code     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'superseded')),
    expires_at    TIMESTAMPTZ NOT NULL,
    decided_at    TIMESTAMPTZ,
    decided_by    TEXT,
    edge_node_ref UUID REFERENCES edge_nodes(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A user code identifies at most one pending enrollment.
CREATE UNIQUE INDEX uq_edge_enrollments_pending_code
    ON edge_enrollments (user_code) WHERE status = 'pending';
CREATE INDEX idx_edge_enrollments_tenant_status
    ON edge_enrollments (tenant_id, status, expires_at);

-- ---------------------------------------------------------------------------
-- discovered_machines: Sparkplug quarantine columns
-- ---------------------------------------------------------------------------
ALTER TABLE discovered_machines DROP CONSTRAINT IF EXISTS discovered_machines_discovery_method_check;
ALTER TABLE discovered_machines ADD CONSTRAINT discovered_machines_discovery_method_check
    CHECK (discovery_method IN ('mdns', 'ssdp', 'usb', 'network_scan', 'bluetooth', 'manual', 'sparkplug'));
ALTER TABLE discovered_machines ADD COLUMN IF NOT EXISTS sparkplug_edge_id TEXT;
ALTER TABLE discovered_machines ADD COLUMN IF NOT EXISTS device_id TEXT;
ALTER TABLE discovered_machines ADD COLUMN IF NOT EXISTS birth_payload JSONB;
ALTER TABLE discovered_machines ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ;
ALTER TABLE discovered_machines ADD COLUMN IF NOT EXISTS birth_count INT NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX uq_discovered_machines_sparkplug_device
    ON discovered_machines (tenant_id, sparkplug_edge_id, device_id)
    WHERE sparkplug_edge_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- machine_live_state: latest Sparkplug state per registered device
-- ---------------------------------------------------------------------------
CREATE TABLE machine_live_state (
    machine_id      UUID PRIMARY KEY REFERENCES machines(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    edge_node_id    TEXT NOT NULL,
    online          BOOLEAN NOT NULL DEFAULT FALSE,
    -- State/Status (MES-1 §1)
    state_status    TEXT CHECK (state_status IN ('idle', 'printing', 'paused', 'error', 'offline')),
    -- State/Progress, 0..100
    progress        DOUBLE PRECISION CHECK (progress BETWEEN 0 AND 100),
    hotend_temp_c   DOUBLE PRECISION,
    bed_temp_c      DOUBLE PRECISION,
    -- [{"slot": 1, "class": "pla", "loaded": true}, ...]; class null = unknown
    material_slots  JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- {"<fabrication-capabilities key>": value, ...} from Capabilities/*
    capabilities    JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- {"model": ..., "firmware": ..., "connectivity": ...} from Properties/*
    properties      JSONB NOT NULL DEFAULT '{}'::jsonb,
    job_id          TEXT,
    job_status      TEXT CHECK (job_status IN ('queued', 'printing', 'complete', 'failed', 'cancelled')),
    command_last_id TEXT,
    command_status  TEXT CHECK (command_status IN ('accepted', 'running', 'done', 'failed')),
    command_error   TEXT,
    bdseq           BIGINT,
    born_at         TIMESTAMPTZ,
    died_at         TIMESTAMPTZ,
    -- payload timestamp reported by the edge node for the latest update
    reported_at     TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_machine_live_state_tenant ON machine_live_state (tenant_id, online, state_status);

-- ---------------------------------------------------------------------------
-- Row-level security (028/029 pattern)
-- ---------------------------------------------------------------------------
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['edge_nodes', 'edge_enrollments', 'machine_live_state']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I FOR ALL '
            'USING (tenant_id = app_current_tenant_id()) '
            'WITH CHECK (tenant_id = app_current_tenant_id())', t);
    END LOOP;
END $$;

-- References must stay inside the tenant (028 tenant_references).
CREATE POLICY tenant_references ON edge_enrollments AS RESTRICTIVE FOR ALL USING (true)
    WITH CHECK (edge_node_ref IS NULL OR EXISTS (
        SELECT 1 FROM edge_nodes p WHERE p.id = edge_enrollments.edge_node_ref AND p.tenant_id = app_current_tenant_id()));
CREATE POLICY tenant_references ON machine_live_state AS RESTRICTIVE FOR ALL USING (true)
    WITH CHECK (machine_id IS NULL OR EXISTS (
        SELECT 1 FROM machines p WHERE p.id = machine_live_state.machine_id AND p.tenant_id = app_current_tenant_id()));

-- Read-only system scope: broker credential lookup by username and the
-- enrollment status poll by id.
CREATE POLICY system_scope_read ON edge_nodes FOR SELECT USING (app_system_scope());
CREATE POLICY system_scope_read ON edge_enrollments FOR SELECT USING (app_system_scope());

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON edge_nodes, edge_enrollments, machine_live_state TO pravara_app;
    ELSE
        RAISE NOTICE '031: role pravara_app does not exist yet; apply infra/db/roles/pravara_app_grants.sql after creating it';
    END IF;
END $$;

COMMIT;
