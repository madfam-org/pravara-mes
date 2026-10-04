-- 030 (down): revert command delivery tracking.
--
-- Direct machine commands (task_id IS NULL) cannot exist under the 010
-- schema, so they are removed; 'timeout' rows are folded into 'failed'.
BEGIN;

DROP INDEX IF EXISTS idx_task_commands_open_deadline;

ALTER TABLE task_commands DROP COLUMN IF EXISTS deadline_at;
ALTER TABLE task_commands DROP COLUMN IF EXISTS sent_at;
ALTER TABLE task_commands DROP COLUMN IF EXISTS attempts;

UPDATE task_commands SET status = 'failed' WHERE status = 'timeout';
ALTER TABLE task_commands DROP CONSTRAINT IF EXISTS task_commands_status_check;
ALTER TABLE task_commands ADD CONSTRAINT task_commands_status_check
    CHECK (status IN ('pending', 'sent', 'acknowledged', 'failed', 'completed'));

DELETE FROM task_commands WHERE task_id IS NULL;
ALTER TABLE task_commands ALTER COLUMN task_id SET NOT NULL;

COMMIT;
