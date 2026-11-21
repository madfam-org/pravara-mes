-- 037 (down): remove the Idempotency-Key store. Roll the API back to a build
-- without it first. Writes already made stay; only the memory of which keys
-- were used is lost, so a client retrying an old key afterwards runs again.
BEGIN;

DROP TABLE IF EXISTS idempotency_keys;

COMMIT;
