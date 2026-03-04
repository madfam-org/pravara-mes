-- =============================================================================
-- Inventory Management (MESA #1 - Resource/Inventory, ForgeSight Activation)
-- Inventory items, transactions, and running balance
-- =============================================================================

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

CREATE TYPE inventory_transaction_type AS ENUM (
    'receipt',
    'consumption',
    'adjustment',
    'reservation',
    'release'
);

-- =============================================================================
-- INVENTORY ITEMS
-- =============================================================================

CREATE TABLE inventory_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Item identity
    sku VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    category VARCHAR(100),
    description TEXT,
    unit VARCHAR(50) NOT NULL DEFAULT 'pcs',

    -- Quantities
    quantity_on_hand DECIMAL(15,4) NOT NULL DEFAULT 0,
    quantity_reserved DECIMAL(15,4) NOT NULL DEFAULT 0,
    quantity_available DECIMAL(15,4) GENERATED ALWAYS AS (quantity_on_hand - quantity_reserved) STORED,

    -- Reorder
    reorder_point DECIMAL(15,4) DEFAULT 0,
    reorder_quantity DECIMAL(15,4) DEFAULT 0,

    -- External reference
    forgesight_id VARCHAR(100),

    -- Cost
    unit_cost DECIMAL(12,2),
    currency VARCHAR(3) DEFAULT 'MXN',

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, sku)
);

-- =============================================================================
-- INVENTORY TRANSACTIONS
-- =============================================================================

CREATE TABLE inventory_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    inventory_item_id UUID NOT NULL REFERENCES inventory_items(id) ON DELETE CASCADE,

    -- Transaction details
    transaction_type inventory_transaction_type NOT NULL,
    quantity DECIMAL(15,4) NOT NULL, -- positive for receipt, negative for consumption
    running_balance DECIMAL(15,4) NOT NULL,

    -- Reference
    reference_type VARCHAR(50),  -- 'genealogy', 'work_order', 'forgesight', 'manual'
    reference_id UUID,

    -- Notes
    notes TEXT,

    -- Audit
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- INDEXES
-- =============================================================================

CREATE INDEX idx_inventory_items_tenant ON inventory_items(tenant_id);
CREATE INDEX idx_inventory_items_sku ON inventory_items(tenant_id, sku);
CREATE INDEX idx_inventory_items_category ON inventory_items(tenant_id, category);
CREATE INDEX idx_inventory_items_forgesight ON inventory_items(forgesight_id);
CREATE INDEX idx_inventory_items_low_stock ON inventory_items(tenant_id)
    WHERE quantity_on_hand - quantity_reserved <= reorder_point;

CREATE INDEX idx_inventory_txn_item ON inventory_transactions(inventory_item_id);
CREATE INDEX idx_inventory_txn_tenant ON inventory_transactions(tenant_id);
CREATE INDEX idx_inventory_txn_type ON inventory_transactions(transaction_type);
CREATE INDEX idx_inventory_txn_created ON inventory_transactions(created_at DESC);

-- =============================================================================
-- ROW-LEVEL SECURITY
-- =============================================================================

ALTER TABLE inventory_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE inventory_transactions ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_inventory_items ON inventory_items
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_inventory_txn ON inventory_transactions
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- =============================================================================
-- TRIGGERS
-- =============================================================================

CREATE TRIGGER update_inventory_items_updated_at
    BEFORE UPDATE ON inventory_items
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
