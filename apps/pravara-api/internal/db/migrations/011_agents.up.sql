-- Agents table: Human operators and automated agents
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL, -- Link to user account if human
    code VARCHAR(50) NOT NULL, -- Unique agent identifier (e.g., "OP-001", "BOT-42")
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('human', 'bot', 'hybrid')),
    status VARCHAR(50) NOT NULL DEFAULT 'offline' CHECK (status IN ('offline', 'available', 'busy', 'break', 'maintenance')),

    -- Skills and certifications
    skills JSONB DEFAULT '[]'::jsonb, -- ["cnc_operation", "quality_inspection", "3d_printing"]
    certifications JSONB DEFAULT '[]'::jsonb, -- [{"name": "ISO 9001", "expiry": "2025-12-31"}]
    experience_level VARCHAR(50) CHECK (experience_level IN ('trainee', 'junior', 'mid', 'senior', 'expert')),

    -- Availability and scheduling
    shift_pattern VARCHAR(50), -- "day", "night", "swing", "flexible"
    available_from TIMESTAMP WITH TIME ZONE,
    available_until TIMESTAMP WITH TIME ZONE,
    max_concurrent_tasks INT DEFAULT 1,
    current_task_count INT DEFAULT 0,

    -- Performance metrics
    tasks_completed INT DEFAULT 0,
    tasks_failed INT DEFAULT 0,
    avg_task_duration_minutes INT,
    quality_score DECIMAL(3,2), -- 0.00 to 1.00
    reliability_score DECIMAL(3,2), -- 0.00 to 1.00

    -- Preferences and constraints
    preferred_machines JSONB DEFAULT '[]'::jsonb, -- Array of machine IDs
    blocked_machines JSONB DEFAULT '[]'::jsonb, -- Machines they can't/won't operate
    notification_preferences JSONB DEFAULT '{}'::jsonb, -- {"email": true, "sms": false, "push": true}

    -- Metadata
    metadata JSONB DEFAULT '{}'::jsonb,
    last_activity_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT agents_tenant_code_unique UNIQUE (tenant_id, code)
);

-- Agent-Machine authorization matrix
CREATE TABLE agent_machines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,

    -- Authorization levels
    can_operate BOOLEAN DEFAULT false,
    can_maintain BOOLEAN DEFAULT false,
    can_configure BOOLEAN DEFAULT false,
    can_emergency_stop BOOLEAN DEFAULT true,

    -- Proficiency and training
    proficiency_level VARCHAR(50) CHECK (proficiency_level IN ('learning', 'basic', 'intermediate', 'advanced', 'expert')),
    training_completed_at TIMESTAMP WITH TIME ZONE,
    certification_expires_at TIMESTAMP WITH TIME ZONE,
    hours_operated INT DEFAULT 0,

    -- Restrictions
    max_operation_hours_per_day INT,
    requires_supervisor BOOLEAN DEFAULT false,
    restricted_operations JSONB DEFAULT '[]'::jsonb, -- Operations they can't perform

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT agent_machines_unique UNIQUE (agent_id, machine_id)
);

-- Agent task assignments
CREATE TABLE agent_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,

    -- Assignment details
    assigned_by UUID REFERENCES users(id),
    assigned_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    accepted_at TIMESTAMP WITH TIME ZONE,
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,

    -- Assignment status
    status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending',      -- Waiting for agent response
        'accepted',     -- Agent accepted the task
        'rejected',     -- Agent rejected the task
        'in_progress',  -- Agent is working on it
        'paused',       -- Temporarily paused
        'completed',    -- Successfully completed
        'failed',       -- Failed to complete
        'reassigned'    -- Reassigned to another agent
    )),

    -- Response and feedback
    response_time_seconds INT, -- Time to accept/reject
    rejection_reason TEXT,
    completion_notes TEXT,
    quality_check_result VARCHAR(50),

    -- Escalation
    escalation_level INT DEFAULT 0,
    escalated_to UUID REFERENCES agents(id),
    escalation_reason TEXT,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Agent notifications
CREATE TABLE agent_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,

    -- Notification details
    type VARCHAR(50) NOT NULL CHECK (type IN (
        'task_assigned',
        'task_reminder',
        'task_escalated',
        'machine_alert',
        'quality_issue',
        'schedule_change',
        'system_message'
    )),
    priority VARCHAR(20) NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),

    -- Content
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    data JSONB DEFAULT '{}'::jsonb, -- Additional context data
    action_url TEXT, -- Deep link to relevant screen

    -- Delivery status
    channels JSONB NOT NULL DEFAULT '["in_app"]'::jsonb, -- ["in_app", "email", "sms", "push"]
    sent_at TIMESTAMP WITH TIME ZONE,
    delivered_at TIMESTAMP WITH TIME ZONE,
    read_at TIMESTAMP WITH TIME ZONE,
    acknowledged_at TIMESTAMP WITH TIME ZONE,

    -- Expiry and persistence
    expires_at TIMESTAMP WITH TIME ZONE,
    requires_acknowledgment BOOLEAN DEFAULT false,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX idx_agents_tenant_status ON agents(tenant_id, status);
CREATE INDEX idx_agents_skills ON agents USING gin(skills);
CREATE INDEX idx_agent_machines_agent ON agent_machines(agent_id);
CREATE INDEX idx_agent_machines_machine ON agent_machines(machine_id);
CREATE INDEX idx_agent_assignments_task ON agent_assignments(task_id);
CREATE INDEX idx_agent_assignments_agent_status ON agent_assignments(agent_id, status);
CREATE INDEX idx_agent_notifications_agent_unread ON agent_notifications(agent_id, read_at) WHERE read_at IS NULL;

-- RLS policies
ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_machines ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_notifications ENABLE ROW LEVEL SECURITY;

-- Agents policies
CREATE POLICY agents_tenant_isolation ON agents
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

CREATE POLICY agent_machines_tenant_isolation ON agent_machines
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

CREATE POLICY agent_assignments_tenant_isolation ON agent_assignments
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

CREATE POLICY agent_notifications_tenant_isolation ON agent_notifications
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

-- Triggers for updated_at
CREATE TRIGGER agents_updated_at BEFORE UPDATE ON agents
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER agent_machines_updated_at BEFORE UPDATE ON agent_machines
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER agent_assignments_updated_at BEFORE UPDATE ON agent_assignments
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();