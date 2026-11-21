-- 037: a general Idempotency-Key for writes (RFC 0064 §5, ruling PV-4).
--
-- A client that may deliver a write more than once (a shop-floor terminal
-- delivers at least once) sends an Idempotency-Key header. The first request
-- with a key runs and its answer is stored here; the same key with the same
-- request gets the stored answer again and nothing runs twice; the same key
-- with another request is 409 idempotency_key_reused. These are the
-- semantics receipts already have with their body field (migration 035).
--
--   client           who the key belongs to inside the tenant: the OAuth
--                    client the token was issued to (a terminal's device),
--                    else the caller (a person, an API key). Two clients
--                    never share a key.
--   idempotency_key  the header, 1-100 printable characters
--   route            the route pattern, for reading the table
--   request_sha256   the method, path, query and canonical JSON body
--   response_*       the answer as it was sent: status, content type and
--                    body bytes, no more than the route returns
--
-- The row is written in the request's own transaction, before the write
-- runs, and filled with the answer before that transaction commits. Only an
-- answer below 400 commits, so a failed or refused write leaves no key and
-- may be retried; a concurrent request with the same key waits on the
-- primary key until the first one ends.
--
-- Rows older than the retention (PRAVARA_IDEMPOTENCY_RETENTION_DAYS,
-- default 35) count as absent and are deleted by later requests of the
-- same tenant.
--
-- Forward-only and additive: a new table, no existing row changes, and code
-- before this migration never reads it. Tenant scoping follows 034: the 028
-- policy, FORCE ROW LEVEL SECURITY (029). Its only foreign key is tenants.
BEGIN;

CREATE TABLE idempotency_keys (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    client TEXT NOT NULL CHECK (length(client) BETWEEN 1 AND 300),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    route TEXT NOT NULL,
    request_sha256 CHAR(64) NOT NULL,
    response_status INTEGER CHECK (response_status BETWEEN 100 AND 399),
    response_content_type TEXT,
    response_body BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, client, idempotency_key)
);

CREATE INDEX idx_idempotency_keys_created ON idempotency_keys (tenant_id, created_at);

-- Row-level security (028/029 pattern).
ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON idempotency_keys FOR ALL
    USING (tenant_id = app_current_tenant_id())
    WITH CHECK (tenant_id = app_current_tenant_id());

-- Application role grants (same as 029 / infra/db/roles/pravara_app_grants.sql).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON idempotency_keys TO pravara_app;
    ELSE
        RAISE NOTICE '037: role pravara_app does not exist yet; apply infra/db/roles/pravara_app_grants.sql after creating it';
    END IF;
END $$;

COMMIT;
