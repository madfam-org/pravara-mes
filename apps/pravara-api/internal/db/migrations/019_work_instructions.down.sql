DROP TRIGGER IF EXISTS update_task_work_instructions_updated_at ON task_work_instructions;
DROP TRIGGER IF EXISTS update_work_instructions_updated_at ON work_instructions;
DROP POLICY IF EXISTS tenant_isolation_task_work_instructions ON task_work_instructions;
DROP POLICY IF EXISTS tenant_isolation_work_instructions ON work_instructions;
DROP TABLE IF EXISTS task_work_instructions;
DROP TABLE IF EXISTS work_instructions;
