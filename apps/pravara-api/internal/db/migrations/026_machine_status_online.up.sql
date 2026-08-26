-- =============================================================================
-- machine_status: add 'online'
-- =============================================================================
-- The application already writes status = 'online' on machine heartbeat
-- (MachineRepository.UpdateHeartbeat) and the Go SDK defines
-- MachineStatusOnline, but the canonical enum from 001_genesis never had the
-- value — so against a genesis-built database every heartbeat write fails
-- and machines can never look alive to capability-based assignment.
--
-- ALTER TYPE ... ADD VALUE cannot run inside a transaction block; the
-- migration runner (infra/db/migrate.sh) executes files without wrapping
-- them in BEGIN/COMMIT, and IF NOT EXISTS makes this a no-op on databases
-- where the value was already added out-of-band.

ALTER TYPE machine_status ADD VALUE IF NOT EXISTS 'online';
