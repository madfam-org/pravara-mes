-- =============================================================================
-- Migration: 010_task_commands (DOWN)
-- =============================================================================

DROP INDEX IF EXISTS idx_tasks_machine_status;
DROP TABLE IF EXISTS task_commands;
