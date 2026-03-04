-- =============================================================================
-- Work Instructions (MESA #4 - Document Control, Simplified)
-- Step-by-step procedures linked to products and tasks
-- =============================================================================

-- =============================================================================
-- WORK INSTRUCTIONS
-- =============================================================================

CREATE TABLE work_instructions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Identity
    title VARCHAR(255) NOT NULL,
    version VARCHAR(50) NOT NULL DEFAULT '1.0',
    category VARCHAR(50) NOT NULL DEFAULT 'operation', -- setup, operation, safety, maintenance
    description TEXT,

    -- Links
    product_definition_id UUID REFERENCES product_definitions(id) ON DELETE SET NULL,
    machine_type VARCHAR(100), -- matches machines.type for auto-attach

    -- Steps (JSONB array, same pattern as inspections checklist)
    -- Each step: { "step_number": 1, "title": "...", "description": "...", "media_url": "...", "warning": "...", "duration_minutes": 5 }
    steps JSONB DEFAULT '[]',

    -- Requirements
    tools_required JSONB DEFAULT '[]',  -- ["wrench", "allen key"]
    ppe_required JSONB DEFAULT '[]',    -- ["safety glasses", "gloves"]

    -- Status
    is_active BOOLEAN NOT NULL DEFAULT true,

    -- Audit
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- TASK-WORK INSTRUCTION JUNCTION
-- =============================================================================

CREATE TABLE task_work_instructions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    work_instruction_id UUID NOT NULL REFERENCES work_instructions(id) ON DELETE CASCADE,

    -- Step acknowledgement tracking
    -- JSONB object: { "1": { "acknowledged_at": "...", "acknowledged_by": "..." }, ... }
    step_acknowledgements JSONB DEFAULT '{}',

    -- Status
    all_acknowledged BOOLEAN NOT NULL DEFAULT false,

    -- Audit
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(task_id, work_instruction_id)
);

-- =============================================================================
-- INDEXES
-- =============================================================================

CREATE INDEX idx_work_instructions_tenant ON work_instructions(tenant_id);
CREATE INDEX idx_work_instructions_product ON work_instructions(product_definition_id);
CREATE INDEX idx_work_instructions_machine_type ON work_instructions(machine_type);
CREATE INDEX idx_work_instructions_category ON work_instructions(tenant_id, category);
CREATE INDEX idx_work_instructions_active ON work_instructions(tenant_id, is_active);

CREATE INDEX idx_task_work_instructions_task ON task_work_instructions(task_id);
CREATE INDEX idx_task_work_instructions_wi ON task_work_instructions(work_instruction_id);

-- =============================================================================
-- ROW-LEVEL SECURITY
-- =============================================================================

ALTER TABLE work_instructions ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_work_instructions ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_work_instructions ON work_instructions
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

CREATE POLICY tenant_isolation_task_work_instructions ON task_work_instructions
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- =============================================================================
-- TRIGGERS
-- =============================================================================

CREATE TRIGGER update_work_instructions_updated_at
    BEFORE UPDATE ON work_instructions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_task_work_instructions_updated_at
    BEFORE UPDATE ON task_work_instructions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
