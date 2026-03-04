-- =============================================================================
-- OEE Snapshots (MESA #11 - Performance Analysis)
-- Daily OEE computation per machine from existing telemetry + tasks
-- =============================================================================

CREATE TABLE oee_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    snapshot_date DATE NOT NULL,

    -- Raw input counters
    planned_minutes DECIMAL(10,2) NOT NULL DEFAULT 480, -- 8h default shift
    downtime_minutes DECIMAL(10,2) NOT NULL DEFAULT 0,
    run_minutes DECIMAL(10,2) NOT NULL DEFAULT 0,
    tasks_completed INTEGER NOT NULL DEFAULT 0,
    tasks_failed INTEGER NOT NULL DEFAULT 0,
    tasks_total INTEGER NOT NULL DEFAULT 0,

    -- Computed OEE components (0.0 - 1.0)
    availability DECIMAL(5,4) NOT NULL DEFAULT 0,
    performance DECIMAL(5,4) NOT NULL DEFAULT 0,
    quality DECIMAL(5,4) NOT NULL DEFAULT 0,
    oee DECIMAL(5,4) NOT NULL DEFAULT 0,

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, machine_id, snapshot_date)
);

-- Indexes
CREATE INDEX idx_oee_snapshots_tenant ON oee_snapshots(tenant_id);
CREATE INDEX idx_oee_snapshots_machine ON oee_snapshots(machine_id, snapshot_date DESC);
CREATE INDEX idx_oee_snapshots_date ON oee_snapshots(tenant_id, snapshot_date DESC);

-- Row Level Security
ALTER TABLE oee_snapshots ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_oee_snapshots ON oee_snapshots
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- Updated_at trigger
CREATE TRIGGER update_oee_snapshots_updated_at
    BEFORE UPDATE ON oee_snapshots
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
