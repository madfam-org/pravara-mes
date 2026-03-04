-- =============================================================================
-- API Keys (External Data Consumer Readiness)
-- Enables API key authentication for external consumers (CRM, social, status)
-- =============================================================================

-- =============================================================================
-- API KEYS
-- =============================================================================

CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Key identity
    name VARCHAR(255) NOT NULL,
    key_hash VARCHAR(64) NOT NULL,    -- SHA-256 hex digest
    key_prefix VARCHAR(12) NOT NULL,  -- e.g. "prv_abc123..."

    -- Authorization
    scopes TEXT[] NOT NULL DEFAULT '{}',
    rate_limit INT NOT NULL DEFAULT 1000,  -- requests per minute

    -- Lifecycle
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,

    -- Audit
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- INDEXES
-- =============================================================================

CREATE UNIQUE INDEX idx_api_keys_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix);

-- =============================================================================
-- ROW-LEVEL SECURITY
-- =============================================================================

ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_api_keys ON api_keys
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- =============================================================================
-- TRIGGERS
-- =============================================================================

CREATE TRIGGER update_api_keys_updated_at
    BEFORE UPDATE ON api_keys
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
