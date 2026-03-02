-- =============================================================================
-- Quality Management System Migration
-- COC/COA certificates, inspections, batch traceability
-- =============================================================================

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

-- Quality certificate types (COC, COA, inspection report, etc.)
CREATE TYPE quality_cert_type AS ENUM (
    'coc',
    'coa',
    'inspection',
    'test_report',
    'calibration'
);

CREATE TYPE quality_cert_status AS ENUM (
    'draft',
    'pending_review',
    'approved',
    'rejected',
    'expired'
);

CREATE TYPE inspection_result AS ENUM (
    'pass',
    'fail',
    'conditional',
    'pending'
);

-- =============================================================================
-- QUALITY CERTIFICATES
-- =============================================================================

CREATE TABLE quality_certificates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    certificate_number VARCHAR(100) NOT NULL,
    type quality_cert_type NOT NULL,
    status quality_cert_status NOT NULL DEFAULT 'draft',

    -- References (polymorphic - can be for order, task, machine, or batch)
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL,
    batch_lot_id UUID, -- Will reference batch_lots when created

    -- Certificate details
    title VARCHAR(255) NOT NULL,
    description TEXT,
    issued_date TIMESTAMPTZ,
    expiry_date TIMESTAMPTZ,
    issued_by UUID REFERENCES users(id) ON DELETE SET NULL,
    approved_by UUID REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,

    -- Document storage
    document_url VARCHAR(500),

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, certificate_number)
);

-- =============================================================================
-- INSPECTIONS
-- =============================================================================

CREATE TABLE inspections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    inspection_number VARCHAR(100) NOT NULL,

    -- What's being inspected
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL,

    -- Inspection details
    type VARCHAR(100) NOT NULL, -- 'incoming', 'in_process', 'final', 'periodic'
    scheduled_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    inspector_id UUID REFERENCES users(id) ON DELETE SET NULL,
    result inspection_result NOT NULL DEFAULT 'pending',
    notes TEXT,

    -- Checklist items stored as JSONB
    checklist JSONB DEFAULT '[]',

    -- Generated certificate
    certificate_id UUID REFERENCES quality_certificates(id) ON DELETE SET NULL,

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, inspection_number)
);

-- =============================================================================
-- BATCH LOTS (Traceability)
-- =============================================================================

CREATE TABLE batch_lots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    lot_number VARCHAR(100) NOT NULL,

    -- Product/material info
    product_name VARCHAR(255) NOT NULL,
    product_code VARCHAR(100),
    quantity DECIMAL(15,4) NOT NULL,
    unit VARCHAR(50) NOT NULL,

    -- Dates
    manufactured_date TIMESTAMPTZ,
    expiry_date TIMESTAMPTZ,
    received_date TIMESTAMPTZ,

    -- Source tracking
    supplier_name VARCHAR(255),
    supplier_lot_number VARCHAR(100),
    purchase_order VARCHAR(100),

    -- Status
    status VARCHAR(50) NOT NULL DEFAULT 'active', -- active, consumed, expired, quarantine, rejected

    -- Related entities
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, lot_number)
);

-- =============================================================================
-- INDEXES
-- =============================================================================

-- Quality certificates indexes
CREATE INDEX idx_quality_certs_tenant ON quality_certificates(tenant_id);
CREATE INDEX idx_quality_certs_order ON quality_certificates(order_id);
CREATE INDEX idx_quality_certs_task ON quality_certificates(task_id);
CREATE INDEX idx_quality_certs_machine ON quality_certificates(machine_id);
CREATE INDEX idx_quality_certs_batch ON quality_certificates(batch_lot_id);
CREATE INDEX idx_quality_certs_status ON quality_certificates(status);
CREATE INDEX idx_quality_certs_type ON quality_certificates(type);
CREATE INDEX idx_quality_certs_issued ON quality_certificates(issued_date DESC);

-- Inspections indexes
CREATE INDEX idx_inspections_tenant ON inspections(tenant_id);
CREATE INDEX idx_inspections_order ON inspections(order_id);
CREATE INDEX idx_inspections_task ON inspections(task_id);
CREATE INDEX idx_inspections_machine ON inspections(machine_id);
CREATE INDEX idx_inspections_result ON inspections(result);
CREATE INDEX idx_inspections_type ON inspections(type);
CREATE INDEX idx_inspections_scheduled ON inspections(scheduled_at);

-- Batch lots indexes
CREATE INDEX idx_batch_lots_tenant ON batch_lots(tenant_id);
CREATE INDEX idx_batch_lots_order ON batch_lots(order_id);
CREATE INDEX idx_batch_lots_status ON batch_lots(status);
CREATE INDEX idx_batch_lots_product ON batch_lots(product_code);
CREATE INDEX idx_batch_lots_expiry ON batch_lots(expiry_date);

-- =============================================================================
-- ROW-LEVEL SECURITY
-- =============================================================================

ALTER TABLE quality_certificates ENABLE ROW LEVEL SECURITY;
ALTER TABLE inspections ENABLE ROW LEVEL SECURITY;
ALTER TABLE batch_lots ENABLE ROW LEVEL SECURITY;

-- Tenant isolation policies
CREATE POLICY tenant_isolation_quality_certificates ON quality_certificates
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_inspections ON inspections
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_batch_lots ON batch_lots
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- =============================================================================
-- TRIGGERS
-- =============================================================================

-- Automatically update updated_at timestamp
CREATE TRIGGER update_quality_certificates_updated_at
    BEFORE UPDATE ON quality_certificates
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_inspections_updated_at
    BEFORE UPDATE ON inspections
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_batch_lots_updated_at
    BEFORE UPDATE ON batch_lots
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- =============================================================================
-- FOREIGN KEY FOR BATCH LOTS IN CERTIFICATES
-- =============================================================================

-- Add foreign key constraint now that batch_lots table exists
ALTER TABLE quality_certificates
    ADD CONSTRAINT fk_quality_certificates_batch_lot
    FOREIGN KEY (batch_lot_id) REFERENCES batch_lots(id) ON DELETE SET NULL;
