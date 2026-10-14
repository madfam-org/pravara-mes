-- 032 (down): remove fabrication dispatch tables. Manufacturing records and
-- undelivered passport rows are dropped with them; export them first if the
-- passports were not delivered.
BEGIN;

DROP TABLE IF EXISTS passport_outbox;
DROP TRIGGER IF EXISTS trg_manufacturing_records_append_only ON manufacturing_records;
DROP TABLE IF EXISTS manufacturing_records;
DROP FUNCTION IF EXISTS manufacturing_records_append_only();
DROP TABLE IF EXISTS machine_reservations;
DROP TABLE IF EXISTS dispatch_jobs;

COMMIT;
