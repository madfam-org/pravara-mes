-- Drop triggers
DROP TRIGGER IF EXISTS agents_updated_at ON agents;
DROP TRIGGER IF EXISTS agent_machines_updated_at ON agent_machines;
DROP TRIGGER IF EXISTS agent_assignments_updated_at ON agent_assignments;

-- Drop policies
DROP POLICY IF EXISTS agents_tenant_isolation ON agents;
DROP POLICY IF EXISTS agent_machines_tenant_isolation ON agent_machines;
DROP POLICY IF EXISTS agent_assignments_tenant_isolation ON agent_assignments;
DROP POLICY IF EXISTS agent_notifications_tenant_isolation ON agent_notifications;

-- Drop indexes
DROP INDEX IF EXISTS idx_agents_tenant_status;
DROP INDEX IF EXISTS idx_agents_skills;
DROP INDEX IF EXISTS idx_agent_machines_agent;
DROP INDEX IF EXISTS idx_agent_machines_machine;
DROP INDEX IF EXISTS idx_agent_assignments_task;
DROP INDEX IF EXISTS idx_agent_assignments_agent_status;
DROP INDEX IF EXISTS idx_agent_notifications_agent_unread;

-- Drop tables
DROP TABLE IF EXISTS agent_notifications;
DROP TABLE IF EXISTS agent_assignments;
DROP TABLE IF EXISTS agent_machines;
DROP TABLE IF EXISTS agents;