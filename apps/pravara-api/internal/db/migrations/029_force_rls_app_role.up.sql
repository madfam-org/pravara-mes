-- 029: FORCE ROW LEVEL SECURITY on every tenant table, and grants for the
-- dedicated application role pravara_app.
--
-- FORCE makes the table owner subject to the policies as well, so isolation
-- does not depend on which role the application connects as (superusers and
-- BYPASSRLS roles are still exempt; see docs/operations/database-app-role.md).
--
-- Creating a role needs privileges the migration runner may not have, so the
-- role itself is created by an operator (docs/operations/database-app-role.md).
-- If pravara_app already exists when this runs, the grants below apply now;
-- otherwise the operator applies infra/db/roles/pravara_app_grants.sql after
-- creating the role. Both paths grant the same thing.
--
-- Data migrations written after this one must set a tenant
-- (set_config('app.current_tenant_id', <id>, true)) or run as a role that
-- bypasses RLS.

BEGIN;

DO $$
DECLARE
    t text;
BEGIN
    FOR t IN
        SELECT c.relname FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND c.relrowsecurity
    LOOP
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
        GRANT USAGE ON SCHEMA public TO pravara_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO pravara_app;
        GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO pravara_app;
        REVOKE ALL ON schema_migrations FROM pravara_app;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
            GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pravara_app;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
            GRANT USAGE, SELECT ON SEQUENCES TO pravara_app;
    ELSE
        RAISE NOTICE '029: role pravara_app does not exist yet; apply infra/db/roles/pravara_app_grants.sql after creating it';
    END IF;
END $$;

COMMIT;
