-- 029 down: remove FORCE ROW LEVEL SECURITY and the pravara_app grants.
-- The role itself is left in place; an operator drops it if wanted.

BEGIN;

DO $$
DECLARE
    t text;
BEGIN
    FOR t IN
        SELECT c.relname FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND c.relforcerowsecurity
    LOOP
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
            REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM pravara_app;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
            REVOKE USAGE, SELECT ON SEQUENCES FROM pravara_app;
        REVOKE ALL ON ALL TABLES IN SCHEMA public FROM pravara_app;
        REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM pravara_app;
        REVOKE USAGE ON SCHEMA public FROM pravara_app;
    END IF;
END $$;

COMMIT;
