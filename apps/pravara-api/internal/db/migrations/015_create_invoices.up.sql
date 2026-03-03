-- Invoice records from Dhanam billing webhooks
CREATE TABLE IF NOT EXISTS invoices (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL,
    dhanam_id     TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL DEFAULT 'created',
    amount        NUMERIC(12, 2) NOT NULL DEFAULT 0,
    currency      TEXT NOT NULL DEFAULT 'MXN',
    period_start  TIMESTAMPTZ NOT NULL,
    period_end    TIMESTAMPTZ NOT NULL,
    line_items    JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_payload   JSONB NOT NULL DEFAULT '{}'::jsonb,
    webhook_event TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes
CREATE INDEX idx_invoices_tenant_id ON invoices(tenant_id);
CREATE INDEX idx_invoices_status ON invoices(status);
CREATE INDEX idx_invoices_period ON invoices(period_start, period_end);

-- Row Level Security
ALTER TABLE invoices ENABLE ROW LEVEL SECURITY;

CREATE POLICY invoices_tenant_isolation ON invoices
    USING (tenant_id = current_setting('app.current_tenant_id')::uuid);

-- Updated_at trigger
CREATE TRIGGER set_invoices_updated_at
    BEFORE UPDATE ON invoices
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();
