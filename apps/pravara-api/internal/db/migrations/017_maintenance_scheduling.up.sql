-- =============================================================================
-- Maintenance Scheduling & CMMS (MESA #9 - Maintenance Management)
-- Recurring schedules with multiple trigger types + work order lifecycle
-- =============================================================================

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

CREATE TYPE maintenance_trigger_type AS ENUM (
    'calendar',
    'runtime_hours',
    'cycle_count',
    'condition'
);

CREATE TYPE work_order_status AS ENUM (
    'scheduled',
    'overdue',
    'in_progress',
    'completed',
    'cancelled'
);

-- =============================================================================
-- MAINTENANCE SCHEDULES
-- =============================================================================

CREATE TABLE maintenance_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,

    -- Schedule definition
    name VARCHAR(255) NOT NULL,
    description TEXT,
    trigger_type maintenance_trigger_type NOT NULL DEFAULT 'calendar',
    priority INTEGER DEFAULT 5 CHECK (priority >= 1 AND priority <= 10),

    -- Interval parameters (varies by trigger_type)
    interval_days INTEGER,            -- calendar: days between maintenance
    interval_hours DECIMAL(10,2),     -- runtime_hours: machine hours between maintenance
    interval_cycles INTEGER,          -- cycle_count: cycles between maintenance
    condition_metric VARCHAR(100),    -- condition: telemetry metric to watch
    condition_threshold DOUBLE PRECISION, -- condition: threshold value

    -- Tracking
    last_done_at TIMESTAMPTZ,
    last_done_hours DECIMAL(10,2),
    last_done_cycles INTEGER,
    next_due_at TIMESTAMPTZ,
    next_due_hours DECIMAL(10,2),
    next_due_cycles INTEGER,

    -- Assignment
    assigned_to UUID REFERENCES users(id) ON DELETE SET NULL,

    -- Status
    is_active BOOLEAN NOT NULL DEFAULT true,

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- MAINTENANCE WORK ORDERS
-- =============================================================================

CREATE TABLE maintenance_work_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    schedule_id UUID REFERENCES maintenance_schedules(id) ON DELETE SET NULL,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,

    -- Work order details
    work_order_number VARCHAR(100) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    status work_order_status NOT NULL DEFAULT 'scheduled',
    priority INTEGER DEFAULT 5 CHECK (priority >= 1 AND priority <= 10),

    -- Assignment
    assigned_to UUID REFERENCES users(id) ON DELETE SET NULL,

    -- Checklist (same pattern as inspections)
    checklist JSONB DEFAULT '[]',

    -- Time tracking
    scheduled_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    due_at TIMESTAMPTZ,

    -- Notes
    notes TEXT,
    parts_used JSONB DEFAULT '[]',

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, work_order_number)
);

-- =============================================================================
-- INDEXES
-- =============================================================================

CREATE INDEX idx_maint_schedules_tenant ON maintenance_schedules(tenant_id);
CREATE INDEX idx_maint_schedules_machine ON maintenance_schedules(machine_id);
CREATE INDEX idx_maint_schedules_next_due ON maintenance_schedules(next_due_at);
CREATE INDEX idx_maint_schedules_active ON maintenance_schedules(tenant_id, is_active);

CREATE INDEX idx_maint_work_orders_tenant ON maintenance_work_orders(tenant_id);
CREATE INDEX idx_maint_work_orders_machine ON maintenance_work_orders(machine_id);
CREATE INDEX idx_maint_work_orders_schedule ON maintenance_work_orders(schedule_id);
CREATE INDEX idx_maint_work_orders_status ON maintenance_work_orders(tenant_id, status);
CREATE INDEX idx_maint_work_orders_assigned ON maintenance_work_orders(assigned_to);

-- =============================================================================
-- ROW-LEVEL SECURITY
-- =============================================================================

ALTER TABLE maintenance_schedules ENABLE ROW LEVEL SECURITY;
ALTER TABLE maintenance_work_orders ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_maint_schedules ON maintenance_schedules
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_maint_work_orders ON maintenance_work_orders
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- =============================================================================
-- TRIGGERS
-- =============================================================================

CREATE TRIGGER update_maint_schedules_updated_at
    BEFORE UPDATE ON maintenance_schedules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_maint_work_orders_updated_at
    BEFORE UPDATE ON maintenance_work_orders
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
