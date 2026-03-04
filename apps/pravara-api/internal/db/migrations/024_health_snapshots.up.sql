-- =============================================================================
-- Health Snapshots (External Data Consumer Readiness - Status Page)
-- Tracks component health for status.madfam.io integration
-- =============================================================================

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

CREATE TYPE component_status AS ENUM (
    'operational',
    'degraded',
    'outage'
);

-- =============================================================================
-- HEALTH SNAPSHOTS
-- =============================================================================

CREATE TABLE health_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Component identity
    component VARCHAR(100) NOT NULL,
    status component_status NOT NULL DEFAULT 'operational',
    details JSONB DEFAULT '{}',

    -- Audit
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- No RLS (system-level table)

-- =============================================================================
-- INDEXES
-- =============================================================================

CREATE INDEX idx_health_snapshots_component ON health_snapshots(component, checked_at DESC);
CREATE INDEX idx_health_snapshots_checked ON health_snapshots(checked_at DESC);
CREATE INDEX idx_health_snapshots_status ON health_snapshots(status, checked_at DESC)
    WHERE status != 'operational';
