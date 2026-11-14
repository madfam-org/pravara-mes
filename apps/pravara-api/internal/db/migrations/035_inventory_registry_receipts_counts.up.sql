-- 035: quantities you can trust (RFC 0058 wave 1): the material code
-- registry, unit conversions, receipts as a record, batch lots linked to
-- stock, physical counts, and the list of quantities that wait for a
-- conversion. Quantities only: no cost, price or currency column anywhere in
-- this migration (ruling I1: Pravara owns quantity, never money).
--
--   material_registry          one row per material code of the tenant
--                              (ruling I3: Pravara owns the registry; I4:
--                              only registered materials are stock). Codes
--                              are unique per tenant ignoring case.
--   inventory_items.material_id
--                              the registry entry a stock item holds (one
--                              item per material).
--   material_aliases           other spellings of a code, added by a person
--                              (a material class such as a print profile's
--                              material type, a supplier's own code).
--   material_unit_conversions  1 from_unit = factor to_unit for one material
--                              (spool → g, mm → g). kg↔g, l↔ml, m↔mm and
--                              cm↔mm are built in (exact), not stored.
--   inventory_conversion_gaps  a consumption, receipt or count line whose
--                              unit has no conversion to its item's unit:
--                              recorded, never guessed, stock untouched
--                              until a conversion exists.
--   inventory_receipts         what physically arrived, with its invoice
--                              evidence (ruling I7): a CFDI UUID and line
--                              position, or an explicit «no invoice».
--   batch_lots.inventory_item_id
--                              the stock item a lot belongs to.
--   inventory_counts, inventory_count_lines
--                              physical counts; closing one posts its
--                              differences as `adjustment` movements with
--                              reference_type 'inventory_count' (ruling I8),
--                              never folded into a job. One count per tenant
--                              may be the opening count (ruling I7).
--
-- Backfill (deterministic only; anything ambiguous stays unlinked and is
-- listed by GET /v1/inventory/materials/unlinked):
--   - each inventory item whose SKU is a valid code, whose unit is a valid
--     unit, and whose SKU is unique in its tenant ignoring case becomes a
--     registry entry with the SKU as code, and is linked to it;
--   - each batch lot whose product_code is exactly an item's SKU is linked
--     to that item;
--   - consumption codes are resolved at read time (code, then alias, then
--     the SKU of a linked item); none is registered by guessing.
-- The backfill bumps inventory_items.updated_at of the items it links.
--
-- Additive: new tables and nullable columns; no existing row changes except
-- the links above. Tenant scoping follows 033: the 028 policy, FORCE ROW
-- LEVEL SECURITY (029) and the 028 RESTRICTIVE tenant_references policy for
-- foreign keys. The backfill runs per tenant with the tenant set, so it
-- works under FORCE ROW LEVEL SECURITY.
BEGIN;

CREATE TABLE material_registry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    code TEXT NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
    name VARCHAR(255) NOT NULL CHECK (btrim(name) <> ''),
    category VARCHAR(16) NOT NULL CHECK (category IN
        ('filament', 'resin', 'sheet', 'spare_part', 'consumable', 'packaging', 'other')),
    base_unit VARCHAR(16) NOT NULL CHECK (base_unit ~ '^[a-z][a-z0-9_]{0,15}$'),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    notes TEXT,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX ux_material_registry_code ON material_registry (tenant_id, lower(code));
CREATE UNIQUE INDEX ux_material_registry_exact ON material_registry (tenant_id, code);

-- The code is the identity other systems reference (Dhanam, Cotiza Studio,
-- Forgesight): it never changes once created.
CREATE OR REPLACE FUNCTION material_registry_code_immutable() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.code IS DISTINCT FROM OLD.code OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'material_registry.code is immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER trg_material_registry_code_immutable
    BEFORE UPDATE ON material_registry
    FOR EACH ROW EXECUTE FUNCTION material_registry_code_immutable();
CREATE TRIGGER update_material_registry_updated_at
    BEFORE UPDATE ON material_registry
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

ALTER TABLE inventory_items ADD COLUMN IF NOT EXISTS material_id UUID
    REFERENCES material_registry(id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX IF NOT EXISTS ux_inventory_items_material ON inventory_items (material_id)
    WHERE material_id IS NOT NULL;

CREATE TABLE material_aliases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    material_id UUID NOT NULL REFERENCES material_registry(id) ON DELETE CASCADE,
    alias TEXT NOT NULL CHECK (btrim(alias) = alias AND length(alias) BETWEEN 1 AND 100),
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX ux_material_aliases_alias ON material_aliases (tenant_id, alias);
CREATE INDEX idx_material_aliases_material ON material_aliases (material_id);

CREATE TABLE material_unit_conversions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    material_id UUID NOT NULL REFERENCES material_registry(id) ON DELETE CASCADE,
    from_unit VARCHAR(16) NOT NULL CHECK (from_unit ~ '^[a-z][a-z0-9_]{0,15}$'),
    to_unit VARCHAR(16) NOT NULL CHECK (to_unit ~ '^[a-z][a-z0-9_]{0,15}$'),
    -- 1 from_unit = factor to_unit
    factor NUMERIC(20,10) NOT NULL CHECK (factor > 0),
    note TEXT,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (from_unit <> to_unit)
);
CREATE UNIQUE INDEX ux_material_unit_conversions ON material_unit_conversions
    (tenant_id, material_id, from_unit, to_unit);

-- Resolves a consumption material code to its registry entry: the code
-- itself, else an alias, else the SKU of a stock item linked to the
-- registry. NULL when the code is not registered. Runs in the caller's
-- tenant (and its row-level security).
CREATE OR REPLACE FUNCTION material_resolve(p_code TEXT) RETURNS UUID
    LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT id FROM material_registry
          WHERE tenant_id = app_current_tenant_id() AND code = p_code),
        (SELECT material_id FROM material_aliases
          WHERE tenant_id = app_current_tenant_id() AND alias = p_code),
        (SELECT material_id FROM inventory_items
          WHERE tenant_id = app_current_tenant_id() AND sku = p_code AND material_id IS NOT NULL))
$$;

ALTER TABLE batch_lots ADD COLUMN IF NOT EXISTS inventory_item_id UUID
    REFERENCES inventory_items(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_batch_lots_inventory_item ON batch_lots (inventory_item_id)
    WHERE inventory_item_id IS NOT NULL;

CREATE TABLE inventory_receipts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    material_id UUID NOT NULL REFERENCES material_registry(id) ON DELETE RESTRICT,
    inventory_item_id UUID NOT NULL REFERENCES inventory_items(id) ON DELETE RESTRICT,
    quantity NUMERIC(15,4) NOT NULL CHECK (quantity > 0),
    unit VARCHAR(50) NOT NULL CHECK (unit ~ '^[a-z][a-z0-9_]{0,15}$'),
    received_at TIMESTAMPTZ NOT NULL,
    -- Who received it (the actor id; no users(id) reference, as in 033).
    received_by UUID,
    batch_lot_id UUID REFERENCES batch_lots(id) ON DELETE SET NULL,
    -- Invoice evidence (ruling I7): exactly one of a CFDI line or «no invoice».
    invoice_status VARCHAR(12) NOT NULL CHECK (invoice_status IN ('invoiced', 'no_invoice')),
    cfdi_uuid UUID,
    cfdi_line INTEGER CHECK (cfdi_line > 0),
    -- posted: the receipt movement exists; needs_conversion: the unit has no
    -- conversion to the item's unit, so stock is untouched until one exists.
    status VARCHAR(20) NOT NULL CHECK (status IN ('posted', 'needs_conversion')),
    stock_quantity NUMERIC(15,4),
    transaction_id UUID REFERENCES inventory_transactions(id) ON DELETE SET NULL,
    posted_at TIMESTAMPTZ,
    notes TEXT,
    idempotency_key TEXT CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((invoice_status = 'invoiced') = (cfdi_uuid IS NOT NULL AND cfdi_line IS NOT NULL)),
    CHECK (invoice_status = 'invoiced' OR (cfdi_uuid IS NULL AND cfdi_line IS NULL)),
    CHECK ((status = 'posted') = (posted_at IS NOT NULL AND stock_quantity IS NOT NULL))
);
CREATE INDEX idx_inventory_receipts_tenant_received ON inventory_receipts (tenant_id, received_at DESC, id);
CREATE INDEX idx_inventory_receipts_material ON inventory_receipts (material_id);
CREATE INDEX idx_inventory_receipts_cfdi ON inventory_receipts (tenant_id, cfdi_uuid, cfdi_line)
    WHERE cfdi_uuid IS NOT NULL;
CREATE UNIQUE INDEX ux_inventory_receipts_idempotency ON inventory_receipts (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_inventory_txn_receipt ON inventory_transactions (reference_id)
    WHERE reference_type = 'inventory_receipt';

-- A receipt is a record: what arrived and its evidence never change. Only
-- posting (needs_conversion → posted) and the ON DELETE SET NULL links may.
CREATE OR REPLACE FUNCTION inventory_receipts_append_only() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.material_id IS DISTINCT FROM OLD.material_id
       OR NEW.inventory_item_id IS DISTINCT FROM OLD.inventory_item_id
       OR NEW.quantity IS DISTINCT FROM OLD.quantity
       OR NEW.unit IS DISTINCT FROM OLD.unit
       OR NEW.received_at IS DISTINCT FROM OLD.received_at
       OR NEW.received_by IS DISTINCT FROM OLD.received_by
       OR NEW.invoice_status IS DISTINCT FROM OLD.invoice_status
       OR NEW.cfdi_uuid IS DISTINCT FROM OLD.cfdi_uuid
       OR NEW.cfdi_line IS DISTINCT FROM OLD.cfdi_line
       OR NEW.notes IS DISTINCT FROM OLD.notes
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR (NEW.batch_lot_id IS NOT NULL AND NEW.batch_lot_id IS DISTINCT FROM OLD.batch_lot_id)
       OR (OLD.status = 'posted' AND (NEW.status IS DISTINCT FROM OLD.status
                                      OR NEW.stock_quantity IS DISTINCT FROM OLD.stock_quantity
                                      OR NEW.posted_at IS DISTINCT FROM OLD.posted_at
                                      OR (NEW.transaction_id IS NOT NULL
                                          AND NEW.transaction_id IS DISTINCT FROM OLD.transaction_id))) THEN
        RAISE EXCEPTION 'inventory_receipts is append-only';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER trg_inventory_receipts_append_only
    BEFORE UPDATE ON inventory_receipts
    FOR EACH ROW EXECUTE FUNCTION inventory_receipts_append_only();

CREATE TABLE inventory_counts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    status VARCHAR(8) NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    is_opening BOOLEAN NOT NULL DEFAULT FALSE,
    notes TEXT,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_by UUID,
    closed_at TIMESTAMPTZ,
    CHECK ((status = 'closed') = (closed_at IS NOT NULL))
);
-- One opening count per tenant (ruling I7: one physical count date).
CREATE UNIQUE INDEX ux_inventory_counts_opening ON inventory_counts (tenant_id) WHERE is_opening;
CREATE INDEX idx_inventory_counts_tenant ON inventory_counts (tenant_id, created_at DESC, id);

CREATE TABLE inventory_count_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    count_id UUID NOT NULL REFERENCES inventory_counts(id) ON DELETE CASCADE,
    material_id UUID NOT NULL REFERENCES material_registry(id) ON DELETE RESTRICT,
    counted_quantity NUMERIC(15,4) NOT NULL CHECK (counted_quantity >= 0),
    unit VARCHAR(50) NOT NULL CHECK (unit ~ '^[a-z][a-z0-9_]{0,15}$'),
    -- When the material was counted: the expected quantity is the stock on
    -- hand at that moment (movements after it are not the count's business).
    counted_at TIMESTAMPTZ NOT NULL,
    counted_by UUID,
    -- Set when the count closes.
    result VARCHAR(20) CHECK (result IN ('matched', 'adjusted', 'needs_conversion', 'not_stocked')),
    inventory_item_id UUID REFERENCES inventory_items(id) ON DELETE SET NULL,
    expected_quantity NUMERIC(15,4),
    counted_stock_quantity NUMERIC(15,4),
    difference NUMERIC(15,4),
    transaction_id UUID REFERENCES inventory_transactions(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (count_id, material_id)
);
CREATE INDEX idx_inventory_count_lines_count ON inventory_count_lines (count_id);
CREATE INDEX IF NOT EXISTS idx_inventory_txn_count ON inventory_transactions (reference_id)
    WHERE reference_type = 'inventory_count';

CREATE TABLE inventory_conversion_gaps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    inventory_item_id UUID NOT NULL REFERENCES inventory_items(id) ON DELETE CASCADE,
    material_id UUID REFERENCES material_registry(id) ON DELETE SET NULL,
    source_type VARCHAR(24) NOT NULL CHECK (source_type IN
        ('job_consumption', 'inventory_receipt', 'inventory_count_line')),
    -- The job_consumption row, receipt or count line that waits.
    source_id UUID NOT NULL,
    -- job consumption only: the job whose stock relief waits.
    job_kind VARCHAR(16) CHECK (job_kind IN ('dispatch', 'print_job', 'task')),
    job_id UUID,
    quantity NUMERIC(15,4) NOT NULL,
    from_unit VARCHAR(50) NOT NULL,
    to_unit VARCHAR(50) NOT NULL,
    -- open: waits for a conversion; resolved: posted once one existed;
    -- superseded: a newer fact of the same job replaced it.
    status VARCHAR(12) NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'superseded')),
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((status = 'open') = (resolved_at IS NULL)),
    CHECK ((source_type = 'job_consumption') = (job_kind IS NOT NULL AND job_id IS NOT NULL))
);
CREATE UNIQUE INDEX ux_inventory_conversion_gaps_open ON inventory_conversion_gaps
    (tenant_id, source_type, source_id) WHERE status = 'open';
CREATE INDEX idx_inventory_conversion_gaps_tenant ON inventory_conversion_gaps (tenant_id, status, created_at, id);
CREATE INDEX idx_inventory_conversion_gaps_item ON inventory_conversion_gaps (inventory_item_id) WHERE status = 'open';

-- Row-level security (028/029 pattern) and tenant_references (033 generator)
-- for every new table.
DO $$
DECLARE
    t text;
    checks text;
BEGIN
    FOREACH t IN ARRAY ARRAY['material_registry', 'material_aliases', 'material_unit_conversions',
        'inventory_receipts', 'inventory_counts', 'inventory_count_lines', 'inventory_conversion_gaps']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I FOR ALL '
            'USING (tenant_id = app_current_tenant_id()) '
            'WITH CHECK (tenant_id = app_current_tenant_id())', t);

        SELECT string_agg(
                   format('(%1$I IS NULL OR EXISTS (SELECT 1 FROM %2$s p WHERE p.%3$I = %4$I.%1$I '
                          'AND p.tenant_id = app_current_tenant_id()))',
                          a.attname, c.confrelid::regclass, pa.attname, t),
                   ' AND ' ORDER BY a.attname)
          INTO checks
        FROM pg_constraint c
        JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
        JOIN pg_attribute pa ON pa.attrelid = c.confrelid AND pa.attnum = c.confkey[1]
        WHERE c.contype = 'f'
          AND array_length(c.conkey, 1) = 1
          AND c.conrelid = t::regclass
          AND c.confrelid <> 'tenants'::regclass
          AND EXISTS (SELECT 1 FROM pg_attribute x
                      WHERE x.attrelid = c.confrelid AND x.attname = 'tenant_id' AND NOT x.attisdropped);
        IF checks IS NOT NULL THEN
            EXECUTE format(
                'CREATE POLICY tenant_references ON %I AS RESTRICTIVE FOR ALL USING (true) WITH CHECK (%s)',
                t, checks);
        END IF;
    END LOOP;
END $$;

-- The two links added to existing tables stay inside the tenant as well
-- (028 generated those tables' tenant_references before these columns
-- existed).
CREATE POLICY tenant_references_material ON inventory_items AS RESTRICTIVE FOR ALL USING (true)
    WITH CHECK (material_id IS NULL OR EXISTS (SELECT 1 FROM material_registry p
                WHERE p.id = inventory_items.material_id AND p.tenant_id = app_current_tenant_id()));
CREATE POLICY tenant_references_inventory_item ON batch_lots AS RESTRICTIVE FOR ALL USING (true)
    WITH CHECK (inventory_item_id IS NULL OR EXISTS (SELECT 1 FROM inventory_items p
                WHERE p.id = batch_lots.inventory_item_id AND p.tenant_id = app_current_tenant_id()));

-- Backfill, one tenant at a time with the tenant set (FORCE ROW LEVEL
-- SECURITY applies to the migration owner too).
DO $$
DECLARE
    tid uuid;
BEGIN
    FOR tid IN SELECT id FROM tenants ORDER BY id
    LOOP
        PERFORM set_config('app.current_tenant_id', tid::text, true);

        INSERT INTO material_registry (tenant_id, code, name, category, base_unit)
        SELECT i.tenant_id, i.sku, i.name,
               CASE WHEN lower(btrim(COALESCE(i.category, ''))) IN
                         ('filament', 'resin', 'sheet', 'spare_part', 'consumable', 'packaging')
                    THEN lower(btrim(i.category)) ELSE 'other' END,
               lower(btrim(i.unit))
        FROM inventory_items i
        WHERE i.tenant_id = tid
          AND i.material_id IS NULL
          AND i.sku ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'
          AND lower(btrim(i.unit)) ~ '^[a-z][a-z0-9_]{0,15}$'
          AND btrim(i.name) <> ''
          AND NOT EXISTS (SELECT 1 FROM inventory_items o
                          WHERE o.tenant_id = tid AND o.id <> i.id AND lower(o.sku) = lower(i.sku))
          AND NOT EXISTS (SELECT 1 FROM material_registry m
                          WHERE m.tenant_id = tid AND lower(m.code) = lower(i.sku));

        UPDATE inventory_items i SET material_id = m.id
        FROM material_registry m
        WHERE i.tenant_id = tid AND m.tenant_id = tid AND m.code = i.sku AND i.material_id IS NULL
          AND NOT EXISTS (SELECT 1 FROM inventory_items x WHERE x.material_id = m.id);

        UPDATE batch_lots b SET inventory_item_id = i.id
        FROM inventory_items i
        WHERE b.tenant_id = tid AND i.tenant_id = tid AND b.product_code = i.sku
          AND b.inventory_item_id IS NULL;
    END LOOP;
    PERFORM set_config('app.current_tenant_id', '', true);
END $$;

-- Application role grants (same as 029 / infra/db/roles/pravara_app_grants.sql).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON material_registry, material_aliases, material_unit_conversions,
            inventory_receipts, inventory_counts, inventory_count_lines, inventory_conversion_gaps TO pravara_app;
    ELSE
        RAISE NOTICE '035: role pravara_app does not exist yet; apply infra/db/roles/pravara_app_grants.sql after creating it';
    END IF;
END $$;

COMMIT;
