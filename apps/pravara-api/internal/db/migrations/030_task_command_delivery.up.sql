-- 030: delivery tracking for machine commands.
--
-- The command ledger (task_commands, 010) becomes the system of record for
-- every machine command, including commands issued directly against a
-- machine (POST /v1/machines/:id/command), so acks can be correlated:
--   * task_id becomes nullable (a direct machine command has no task);
--   * status gains 'timeout' for commands that were never acknowledged in
--     time (or never dispatched);
--   * attempts / sent_at / deadline_at track delivery by the telemetry
--     worker, which consumes the durable command stream.
BEGIN;

ALTER TABLE task_commands ALTER COLUMN task_id DROP NOT NULL;

ALTER TABLE task_commands DROP CONSTRAINT IF EXISTS task_commands_status_check;
ALTER TABLE task_commands ADD CONSTRAINT task_commands_status_check
    CHECK (status IN ('pending', 'sent', 'acknowledged', 'failed', 'completed', 'timeout'));

ALTER TABLE task_commands ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0;
ALTER TABLE task_commands ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ;
ALTER TABLE task_commands ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMPTZ;

-- Deadline sweep: only open commands are ever scanned.
CREATE INDEX IF NOT EXISTS idx_task_commands_open_deadline
    ON task_commands (status, deadline_at, issued_at)
    WHERE status IN ('pending', 'sent');

COMMIT;
