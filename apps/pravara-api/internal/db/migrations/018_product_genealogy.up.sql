-- =============================================================================
-- Product Genealogy & Flat BOM (MESA #10 - Product Tracking)
-- Product definitions, BOM, genealogy records, material consumption
-- =============================================================================

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

CREATE TYPE product_category AS ENUM (
    '3d_print',
    'cnc_part',
    'laser_cut',
    'assembly',
    'other'
);

CREATE TYPE genealogy_status AS ENUM (
    'draft',
    'in_progress',
    'completed',
    'sealed'
);

-- =============================================================================
-- PRODUCT DEFINITIONS
-- =============================================================================

CREATE TABLE product_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Product identity
    sku VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    version VARCHAR(50) NOT NULL DEFAULT '1.0',
    category product_category NOT NULL DEFAULT 'other',
    description TEXT,

    -- Technical specs
    cad_file_url TEXT,
    parametric_specs JSONB DEFAULT '{}',

    -- Status
    is_active BOOLEAN NOT NULL DEFAULT true,

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, sku, version)
);

-- =============================================================================
-- BOM ITEMS (Flat one-level)
-- =============================================================================

CREATE TABLE bom_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    product_definition_id UUID NOT NULL REFERENCES product_definitions(id) ON DELETE CASCADE,

    -- Material
    material_name VARCHAR(255) NOT NULL,
    material_code VARCHAR(100),
    quantity DECIMAL(15,4) NOT NULL,
    unit VARCHAR(50) NOT NULL DEFAULT 'pcs',

    -- Cost
    estimated_cost DECIMAL(12,2),
    currency VARCHAR(3) DEFAULT 'MXN',

    -- Supplier
    supplier VARCHAR(255),

    -- Sort order
    sort_order INTEGER DEFAULT 0,

    -- Audit
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- PRODUCT GENEALOGY (Birth certificate records)
-- =============================================================================

CREATE TABLE product_genealogy (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Product identity
    product_definition_id UUID REFERENCES product_definitions(id) ON DELETE SET NULL,
    serial_number VARCHAR(100),
    lot_number VARCHAR(100),

    -- Traceability links
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
    order_item_id UUID REFERENCES order_items(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL,

    -- Quality links
    inspection_id UUID REFERENCES inspections(id) ON DELETE SET NULL,
    certificate_id UUID REFERENCES quality_certificates(id) ON DELETE SET NULL,

    -- Status & seal
    status genealogy_status NOT NULL DEFAULT 'draft',
    sealed_at TIMESTAMPTZ,
    sealed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    seal_hash VARCHAR(64),  -- SHA-256 hash
    birth_cert_url TEXT,    -- R2 URL for sealed JSON-LD document

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- GENEALOGY MATERIAL CONSUMPTION
-- =============================================================================

CREATE TABLE genealogy_material_consumption (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    genealogy_id UUID NOT NULL REFERENCES product_genealogy(id) ON DELETE CASCADE,

    -- Material consumed
    batch_lot_id UUID REFERENCES batch_lots(id) ON DELETE SET NULL,
    material_name VARCHAR(255) NOT NULL,
    material_code VARCHAR(100),
    quantity_consumed DECIMAL(15,4) NOT NULL,
    unit VARCHAR(50) NOT NULL DEFAULT 'pcs',

    -- Audit
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- IMMUTABILITY TRIGGER (Sealed genealogy cannot be updated)
-- =============================================================================

CREATE OR REPLACE FUNCTION prevent_sealed_genealogy_update()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'sealed' AND NEW.status != OLD.status THEN
        RAISE EXCEPTION 'Cannot modify a sealed genealogy record';
    END IF;
    IF OLD.status = 'sealed' THEN
        RAISE EXCEPTION 'Cannot modify a sealed genealogy record';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER check_genealogy_sealed
    BEFORE UPDATE ON product_genealogy
    FOR EACH ROW EXECUTE FUNCTION prevent_sealed_genealogy_update();

-- =============================================================================
-- INDEXES
-- =============================================================================

CREATE INDEX idx_product_defs_tenant ON product_definitions(tenant_id);
CREATE INDEX idx_product_defs_sku ON product_definitions(tenant_id, sku);
CREATE INDEX idx_product_defs_category ON product_definitions(tenant_id, category);

CREATE INDEX idx_bom_items_product ON bom_items(product_definition_id);
CREATE INDEX idx_bom_items_tenant ON bom_items(tenant_id);

CREATE INDEX idx_genealogy_tenant ON product_genealogy(tenant_id);
CREATE INDEX idx_genealogy_product ON product_genealogy(product_definition_id);
CREATE INDEX idx_genealogy_order ON product_genealogy(order_id);
CREATE INDEX idx_genealogy_task ON product_genealogy(task_id);
CREATE INDEX idx_genealogy_serial ON product_genealogy(tenant_id, serial_number);
CREATE INDEX idx_genealogy_lot ON product_genealogy(tenant_id, lot_number);
CREATE INDEX idx_genealogy_status ON product_genealogy(tenant_id, status);

CREATE INDEX idx_genealogy_material_genealogy ON genealogy_material_consumption(genealogy_id);
CREATE INDEX idx_genealogy_material_batch ON genealogy_material_consumption(batch_lot_id);

-- =============================================================================
-- ROW-LEVEL SECURITY
-- =============================================================================

ALTER TABLE product_definitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE bom_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_genealogy ENABLE ROW LEVEL SECURITY;
ALTER TABLE genealogy_material_consumption ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_product_defs ON product_definitions
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_bom_items ON bom_items
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_genealogy ON product_genealogy
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_genealogy_material ON genealogy_material_consumption
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- =============================================================================
-- TRIGGERS
-- =============================================================================

CREATE TRIGGER update_product_defs_updated_at
    BEFORE UPDATE ON product_definitions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_bom_items_updated_at
    BEFORE UPDATE ON bom_items
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_genealogy_updated_at
    BEFORE UPDATE ON product_genealogy
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
