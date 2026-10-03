# Database application role

> Public document. Placeholders only: no hostnames, credentials or
> environment-specific values belong here.

Pravara's API and telemetry worker connect to PostgreSQL as a dedicated
application role, `pravara_app`. The role is subject to row-level security,
holds DML grants only, and owns nothing. Schema changes run as the schema
owner through `infra/db/migrate.sh`.

## How tenant isolation works

- Every tenant table has a `tenant_isolation` policy (migration 028) that
  reads the transaction-local setting `app.current_tenant_id`, with both
  `USING` and `WITH CHECK`. An unset setting matches no rows.
- A restrictive `tenant_references` policy (also 028) requires every
  foreign key from a tenant table to point at a parent row of the same
  tenant, because foreign-key checks themselves ignore row-level security.
- The application sets `app.current_tenant_id` with
  `SELECT set_config('app.current_tenant_id', $1, true)` as the first
  statement of each transaction (`apps/pravara-api/internal/db/tenant_scope.go`,
  `apps/telemetry-worker/internal/db/tenant_tx.go`). Nothing is set on a
  pooled connection.
- A read-only "system scope" (`app.system_scope = 'on'`) may SELECT four
  queue-like tables across tenants: `event_outbox`, `webhook_subscriptions`,
  `webhook_deliveries` and `api_keys`. Writes always run in a tenant scope.
- Migration 029 sets `FORCE ROW LEVEL SECURITY`, so the table owner is
  subject to the policies too. Superusers and roles with `BYPASSRLS` are
  always exempt, which is why the application must not connect as one.
- Global reference tables (`tenants`, `health_snapshots`, protocol and
  firmware catalogs, `simulation_cache`, `schema_migrations`) have no RLS.

## Role attributes

| Attribute | Value | Why |
|-----------|-------|-----|
| `SUPERUSER` | no | superusers bypass RLS |
| `BYPASSRLS` | no | bypasses RLS |
| `INHERIT` | no | grants come only from explicit `GRANT`s, not role membership |
| `CREATEDB`, `CREATEROLE`, `REPLICATION` | no | not needed |
| Table privileges | `SELECT, INSERT, UPDATE, DELETE` | DML only; no `TRUNCATE` (it ignores RLS), no DDL |
| `schema_migrations` | none | migration tracking stays with the owner |

## Creating the role

Run as a role allowed to create roles. `psql` prompts for the password; do
not put it on the command line or in a file under version control.

```sql
CREATE ROLE pravara_app
    LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT
    NOCREATEDB NOCREATEROLE NOREPLICATION;
\password pravara_app
```

Then, as the schema owner (the role that runs `infra/db/migrate.sh`):

```sh
psql "$OWNER_DATABASE_URL" --set ON_ERROR_STOP=1 -f infra/db/roles/pravara_app_grants.sql
```

The grants file is idempotent and grants the same privileges as migration
029 (029 applies them itself when the role already exists). On PostgreSQL 14
the `public` schema is owned by the bootstrap superuser, so a non-superuser
owner sees `WARNING: no privileges were granted for "public"`; that is
expected, because `USAGE` on `public` is granted to everyone by default on
14. On PostgreSQL 15+ grant `USAGE ON SCHEMA public` as the schema owner.

## Switching the application

Store the new connection string as the database secret of `pravara-api` and
`telemetry-worker` through Enclii (same host and database, user
`pravara_app`), and roll both services. Keep the owner credentials only for
running migrations.

Verify from the application's connection:

```sql
SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user;
-- expect: f | f
```

## Testing isolation locally

The isolation suites run against a throwaway database migrated through 029,
connected as `pravara_app`:

```sh
export PRAVARA_ISOLATION_DB_URL='postgres://pravara_app@127.0.0.1:<port>/<db>?sslmode=disable'
(cd apps/pravara-api && go test -race -run TestTenantIsolation ./internal/api/)
(cd apps/telemetry-worker && go test -race -run TestIngest_TenantFromTopic ./internal/mqtt/)
```

They are skipped when the variable is unset and in `-short` mode.

## Rolling back

Point the services back at the previous credentials. Migrations 028 and 029
have down files (`infra/db/migrate.sh down 1`, once per migration); 029's down
removes `FORCE` and the `pravara_app` grants and leaves the role in place.
