-- 034 (down): remove the cost-event outbox. Turn PRAVARA_COST_EVENTS_ENABLED
-- off first. Events Dhanam already stored stay in Dhanam; dropping the
-- outbox only forgets what was sent, so turning the switch on again later
-- re-sends every line in the lookback (Dhanam answers `unchanged`).
BEGIN;

DROP TABLE IF EXISTS cost_event_outbox;

COMMIT;
