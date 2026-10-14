-- 031 down: remove the Sparkplug registry, edge-node credentials, quarantine
-- columns and live machine state. Edge-node credentials and live state are
-- dropped; edge nodes must enroll again after a re-apply.

BEGIN;

DROP TABLE IF EXISTS machine_live_state;
DROP TABLE IF EXISTS edge_enrollments;
DROP TABLE IF EXISTS edge_nodes;

DROP INDEX IF EXISTS uq_discovered_machines_sparkplug_device;
-- Quarantined Sparkplug rows cannot satisfy the 012 discovery methods.
DELETE FROM discovered_machines WHERE discovery_method = 'sparkplug';
ALTER TABLE discovered_machines DROP COLUMN IF EXISTS birth_count;
ALTER TABLE discovered_machines DROP COLUMN IF EXISTS first_seen_at;
ALTER TABLE discovered_machines DROP COLUMN IF EXISTS birth_payload;
ALTER TABLE discovered_machines DROP COLUMN IF EXISTS device_id;
ALTER TABLE discovered_machines DROP COLUMN IF EXISTS sparkplug_edge_id;
ALTER TABLE discovered_machines DROP CONSTRAINT IF EXISTS discovered_machines_discovery_method_check;
ALTER TABLE discovered_machines ADD CONSTRAINT discovered_machines_discovery_method_check
    CHECK (discovery_method IN ('mdns', 'ssdp', 'usb', 'network_scan', 'bluetooth', 'manual'));

ALTER TABLE machines DROP CONSTRAINT IF EXISTS machines_sparkplug_device_key;
ALTER TABLE machines DROP CONSTRAINT IF EXISTS machines_sparkplug_edge_id_check;
ALTER TABLE machines DROP COLUMN IF EXISTS sparkplug_edge_id;

COMMIT;
