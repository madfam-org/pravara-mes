-- =============================================================================
-- Migration: 010_task_commands
-- Description: Task-Machine command tracking for Kanban automation
-- =============================================================================

-- Track commands dispatched from tasks to machines
CREATE TABLE task_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    command_id UUID NOT NULL UNIQUE,
    command_type VARCHAR(50) NOT NULL,
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'acknowledged', 'failed', 'completed')),
    parameters JSONB DEFAULT '{}',
    issued_by UUID REFERENCES users(id) ON DELETE SET NULL,
    issued_at TIMESTAMPTZ DEFAULT NOW(),
    acked_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for common queries
CREATE INDEX idx_task_commands_task ON task_commands(task_id);
CREATE INDEX idx_task_commands_machine ON task_commands(machine_id);
CREATE INDEX idx_task_commands_command_id ON task_commands(command_id);
CREATE INDEX idx_task_commands_tenant_status ON task_commands(tenant_id, status);
CREATE INDEX idx_task_commands_active ON task_commands(task_id, status)
    WHERE status IN ('pending', 'sent', 'acknowledged');

-- Enable RLS
ALTER TABLE task_commands ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_task_commands ON task_commands
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant_id', true)::UUID);

-- Trigger for updated_at
CREATE TRIGGER update_task_commands_updated_at
    BEFORE UPDATE ON task_commands
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Add index for machine tasks lookup (used by automation service)
CREATE INDEX idx_tasks_machine_status ON tasks(machine_id, status)
    WHERE machine_id IS NOT NULL;
