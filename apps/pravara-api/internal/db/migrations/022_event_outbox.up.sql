-- =============================================================================
-- Event Outbox + Webhook Subscriptions (External Data Consumer Readiness)
-- Persists all published events and manages outbound webhook delivery
-- =============================================================================

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

CREATE TYPE webhook_delivery_status AS ENUM (
    'pending',
    'delivered',
    'failed',
    'dead'
);

-- =============================================================================
-- EVENT OUTBOX
-- =============================================================================

CREATE TABLE event_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Event metadata
    event_type VARCHAR(100) NOT NULL,
    channel_namespace VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,

    -- Delivery tracking
    delivered BOOLEAN NOT NULL DEFAULT FALSE,

    -- Audit
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- WEBHOOK SUBSCRIPTIONS
-- =============================================================================

CREATE TABLE webhook_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Subscription config
    name VARCHAR(255) NOT NULL,
    url TEXT NOT NULL,
    secret VARCHAR(255) NOT NULL,
    event_types TEXT[] NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    -- Audit
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- WEBHOOK DELIVERIES
-- =============================================================================

CREATE TABLE webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES event_outbox(id) ON DELETE CASCADE,

    -- Delivery status
    status webhook_delivery_status NOT NULL DEFAULT 'pending',
    http_status INT,
    attempt_count INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    last_error TEXT,

    -- Audit
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- INDEXES
-- =============================================================================

-- Event outbox indexes
CREATE INDEX idx_event_outbox_tenant ON event_outbox(tenant_id);
CREATE INDEX idx_event_outbox_pending ON event_outbox(delivered, created_at)
    WHERE delivered = FALSE;
CREATE INDEX idx_event_outbox_type_time ON event_outbox(event_type, created_at DESC);
CREATE INDEX idx_event_outbox_created ON event_outbox(created_at DESC);

-- Webhook subscription indexes
CREATE INDEX idx_webhook_subscriptions_tenant ON webhook_subscriptions(tenant_id);
CREATE INDEX idx_webhook_subscriptions_active ON webhook_subscriptions(tenant_id, is_active)
    WHERE is_active = TRUE;

-- Webhook delivery indexes
CREATE INDEX idx_webhook_deliveries_subscription ON webhook_deliveries(subscription_id);
CREATE INDEX idx_webhook_deliveries_event ON webhook_deliveries(event_id);
CREATE INDEX idx_webhook_deliveries_pending ON webhook_deliveries(status, next_retry_at)
    WHERE status IN ('pending', 'failed');

-- =============================================================================
-- ROW-LEVEL SECURITY
-- =============================================================================

ALTER TABLE event_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_event_outbox ON event_outbox
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_webhook_subscriptions ON webhook_subscriptions
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- Webhook deliveries RLS through subscription join
CREATE POLICY tenant_isolation_webhook_deliveries ON webhook_deliveries
    FOR ALL
    USING (subscription_id IN (
        SELECT id FROM webhook_subscriptions
        WHERE tenant_id = current_setting('app.current_tenant_id', true)::UUID
    ));

-- =============================================================================
-- TRIGGERS
-- =============================================================================

CREATE TRIGGER update_webhook_subscriptions_updated_at
    BEFORE UPDATE ON webhook_subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_webhook_deliveries_updated_at
    BEFORE UPDATE ON webhook_deliveries
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
