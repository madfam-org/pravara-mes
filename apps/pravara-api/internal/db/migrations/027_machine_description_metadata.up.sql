-- 027: add the machines columns the code has always selected but no migration created.
--
-- machine_repository.go SELECTs/INSERTs `description` and `metadata` on every
-- machine read/write path, but 001_genesis created machines without either
-- column. On any database provisioned purely from the committed migrations
-- (discovered 2026-08-26: production's pravara_mes was in fact EMPTY — never
-- migrated at all), every machine endpoint would error on the missing columns.
-- This closes the code↔migrations drift so `migrate.sh up` on a fresh database
-- yields a schema the deployed code actually runs against.
ALTER TABLE machines ADD COLUMN IF NOT EXISTS description TEXT;
ALTER TABLE machines ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
