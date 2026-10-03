-- Grants for the pravara_app application role. Idempotent; run as the role
-- that owns the schema (the role that runs infra/db/migrate.sh), after the
-- role exists. Same grants as migration 029. See
-- docs/operations/database-app-role.md.
--
-- DML only: no TRUNCATE (it bypasses row-level security), no DDL, nothing on
-- the migration tracking table.

GRANT USAGE ON SCHEMA public TO pravara_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO pravara_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO pravara_app;
REVOKE ALL ON schema_migrations FROM pravara_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pravara_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO pravara_app;
