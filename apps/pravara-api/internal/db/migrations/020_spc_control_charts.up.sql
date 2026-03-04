-- =============================================================================
-- SPC Control Charts (MESA #7 Enhanced - Statistical Process Control)
-- Control limits per machine/metric + violation detection
-- =============================================================================

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

CREATE TYPE spc_violation_type AS ENUM (
    'above_ucl',
    'below_lcl',
    'run_of_7',
    'trend'
);

-- =============================================================================
-- SPC CONTROL LIMITS
-- =============================================================================

CREATE TABLE spc_control_limits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,

    -- Metric identification
    metric_type VARCHAR(100) NOT NULL,

    -- Statistical limits
    mean DOUBLE PRECISION NOT NULL,
    stddev DOUBLE PRECISION NOT NULL,
    ucl DOUBLE PRECISION NOT NULL, -- Upper Control Limit (mean + 3*stddev)
    lcl DOUBLE PRECISION NOT NULL, -- Lower Control Limit (mean - 3*stddev)

    -- Optional specification limits (customer-defined)
    usl DOUBLE PRECISION,  -- Upper Specification Limit
    lsl DOUBLE PRECISION,  -- Lower Specification Limit

    -- Sample metadata
    sample_count INTEGER NOT NULL,
    sample_start TIMESTAMPTZ NOT NULL,
    sample_end TIMESTAMPTZ NOT NULL,

    -- Status
    is_active BOOLEAN NOT NULL DEFAULT true,

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, machine_id, metric_type)
);

-- =============================================================================
-- SPC VIOLATIONS
-- =============================================================================

CREATE TABLE spc_violations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    control_limit_id UUID NOT NULL REFERENCES spc_control_limits(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,

    -- Violation details
    violation_type spc_violation_type NOT NULL,
    metric_type VARCHAR(100) NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    limit_value DOUBLE PRECISION, -- UCL or LCL that was breached
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Acknowledgement
    acknowledged BOOLEAN NOT NULL DEFAULT false,
    acknowledged_by UUID REFERENCES users(id) ON DELETE SET NULL,
    acknowledged_at TIMESTAMPTZ,
    notes TEXT,

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- INDEXES
-- =============================================================================

CREATE INDEX idx_spc_limits_tenant ON spc_control_limits(tenant_id);
CREATE INDEX idx_spc_limits_machine ON spc_control_limits(machine_id, metric_type);
CREATE INDEX idx_spc_limits_active ON spc_control_limits(tenant_id, is_active);

CREATE INDEX idx_spc_violations_tenant ON spc_violations(tenant_id);
CREATE INDEX idx_spc_violations_machine ON spc_violations(machine_id);
CREATE INDEX idx_spc_violations_limit ON spc_violations(control_limit_id);
CREATE INDEX idx_spc_violations_unacked ON spc_violations(tenant_id, acknowledged) WHERE NOT acknowledged;
CREATE INDEX idx_spc_violations_detected ON spc_violations(detected_at DESC);

-- =============================================================================
-- ROW-LEVEL SECURITY
-- =============================================================================

ALTER TABLE spc_control_limits ENABLE ROW LEVEL SECURITY;
ALTER TABLE spc_violations ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_spc_limits ON spc_control_limits
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_spc_violations ON spc_violations
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- =============================================================================
-- TRIGGERS
-- =============================================================================

CREATE TRIGGER update_spc_limits_updated_at
    BEFORE UPDATE ON spc_control_limits
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
