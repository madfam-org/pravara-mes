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
| `CONNECTION LIMIT` | 60 in production | covers the API and worker pools; see the connection-budget precondition |
| Table privileges | `SELECT, INSERT, UPDATE, DELETE` | DML only; no `TRUNCATE` (it ignores RLS), no DDL |
| `schema_migrations` | none | migration tracking stays with the owner |

## Production rollout (state and plan)

State on 2026-10-04:

- Production runs the in-namespace PostgreSQL 16 StatefulSet `postgres-pravara`
  (`infra/k8s/base/postgres.yaml`), database `pravara_mes`. It is **not** the
  shared, platform-managed Postgres, so the platform's generated app-role
  mechanism (`enclii onboard ensure --app-role …`) does not reach it.
- The application still connects as the bootstrap role from Secret
  `pravara-db`, which is a superuser and owns every table. Row-level security
  is therefore bypassed until the switch below.
- Migrations through 027 and **030** are applied; 030 was applied on
  2026-10-04 and is recorded in `schema_migrations`. **028 and 029 and the
  role switch remain.**
- No credential is generated, typed, copied or printed by a person in any step
  below. Every command runs in one break-glass shell session on the cluster
  (the platform's documented break-glass path; there is no platform adapter for
  an in-namespace database yet). Never paste a step together with its
  verification or with the next step.

### Preconditions (each must be true before the switch in step 3)

1. **Database identity.** The target is `postgres-pravara-0`, database
   `pravara_mes`. Re-check before starting:
   `kubectl -n pravara-mes get pod postgres-pravara-0 -o name`.
2. **Migrations without an owner URL.** The API image has no `psql`, so
   `infra/db/migrate.sh` cannot run in-cluster. Migrations run through the
   database pod's own `psql` (step 2). Follow-up: a migration Job or init
   container with `psql` that reads the owner credentials only it can see.
3. **The telemetry worker takes split settings, not a URL.** It reads
   `DATABASE_HOST`, `DATABASE_PORT`, `DATABASE_USER`, `DATABASE_PASSWORD` and
   `DATABASE_NAME` (or the `PRAVARA_`-prefixed forms) and falls back to
   `postgres-pravara`, `5432`, `pravara`, `pravara_mes`. Before the switch,
   list the key **names** in `pravara-secrets` (never the values):
   `kubectl -n pravara-mes get secret pravara-secrets -o go-template='{{range $k, $v := .data}}{{$k}}{{"\n"}}{{end}}'`.
   The switch sets `DATABASE_USER` and `DATABASE_PASSWORD` explicitly
   (step 3), so it does not depend on what the Secret carries today.
4. **Connection budget.** The API opens up to `DATABASE_MAX_CONNECTIONS`
   connections per pod (default 25); the telemetry worker opens up to 25 (fixed
   in code, `apps/telemetry-worker/internal/db/store.go`). The role's
   `CONNECTION LIMIT` must cover the sum across replicas plus headroom, and stay
   well under the instance's `max_connections` (100 by default), which the
   bootstrap role and maintenance also use. This plan uses **60** (25 + 25 + 10)
   with one replica of each. Follow-up: make the worker pool configurable, set
   `DATABASE_MAX_CONNECTIONS` explicitly, and lower the limit to match.
5. **Statement logging.** The role password passes through one `CREATE ROLE`
   or `ALTER ROLE` statement inside the pod. Confirm the server does not log
   DDL statements (`SHOW log_statement;` must return `none`) before step 1.

### Step 1: create `pravara_app` with an in-cluster generated password

The password is drawn from `/dev/urandom` inside the database pod (it has no
`openssl`). It exists only in that pod's shell and in the Secret: it leaves the
pod only as a JSON patch on stdin, piped straight into the Secret, and is never
echoed, logged or held in a variable on the operator's side. The step is
idempotent: re-running it rotates the password and rewrites the Secret keys.

```sh
set -o pipefail
kubectl -n pravara-mes exec -i postgres-pravara-0 -c postgres -- sh -eu -s <<'SH' \
  | kubectl -n pravara-mes patch secret pravara-secrets --type merge --patch-file /dev/stdin -o name
pw=$(head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 40)
[ "${#pw}" -eq 40 ]
export APP_PW="$pw"
psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d pravara_mes >/dev/null <<'SQL'
\set app_pw `printf '%s' "$APP_PW"`
SELECT CASE WHEN EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app')
            THEN 'ALTER' ELSE 'CREATE' END AS verb \gset
:verb ROLE pravara_app LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION
  CONNECTION LIMIT 60 PASSWORD :'app_pw';
GRANT CONNECT ON DATABASE pravara_mes TO pravara_app;
SQL
printf '{"stringData":{"APP_DATABASE_PASSWORD":"%s","APP_DATABASE_URL":"postgresql://pravara_app:%s@postgres-pravara:5432/pravara_mes?sslmode=disable"}}' "$pw" "$pw"
SH
```

The script runs in the pod's shell and reaches `psql` over the pod's local
socket, so no database password is needed or shown. The JSON it prints is the
only thing that leaves the pod, and it goes straight into the Secret patch.

Expected output: `secret/pravara-secrets`. The two new keys change nothing
yet: the running services keep using their current settings until step 3.

Verify (read-only, prints no secret):

```sh
kubectl -n pravara-mes exec postgres-pravara-0 -c postgres -- sh -c \
  'psql -X -At -U "$POSTGRES_USER" -d pravara_mes -c "SELECT rolsuper, rolbypassrls, rolinherit, rolconnlimit FROM pg_roles WHERE rolname = '"'"'pravara_app'"'"'"'
# expect: f|f|f|60
```

### Step 2: apply 028, then 029, through the database pod

Each migration is streamed from GitHub at a pinned commit into the pod's
`psql`, then recorded in `schema_migrations` exactly as `infra/db/migrate.sh`
records it (the file, then one `INSERT` of its version). The `INSERT` is
guarded: it runs only if the migration's effect is present. 029 grants
`pravara_app` its privileges because the role exists after step 1.

Set the commit once (the merge commit that is deployed):

```sh
SHA=<deployed main commit>
RAW=https://raw.githubusercontent.com/madfam-org/pravara-mes/$SHA/apps/pravara-api/internal/db/migrations
PSQL='psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d pravara_mes'
```

Check that neither is recorded yet (expect no rows):

```sh
kubectl -n pravara-mes exec postgres-pravara-0 -c postgres -- sh -c "$PSQL -At -c \"SELECT version FROM schema_migrations WHERE version IN ('028_unify_tenant_rls','029_force_rls_app_role')\""
```

Apply 028:

```sh
set -o pipefail
curl -fsSL "$RAW/028_unify_tenant_rls.up.sql" \
  | kubectl -n pravara-mes exec -i postgres-pravara-0 -c postgres -- sh -c "$PSQL -f -"
kubectl -n pravara-mes exec -i postgres-pravara-0 -c postgres -- sh -c "$PSQL" <<'SQL'
DO $$
BEGIN
  IF (SELECT count(*) FROM pg_policies WHERE schemaname = 'public' AND policyname = 'tenant_isolation') <> 49
     OR (SELECT count(*) FROM pg_policies WHERE schemaname = 'public' AND policyname = 'tenant_references') <> 36 THEN
    RAISE EXCEPTION '028 effect not present; not recording it';
  END IF;
  INSERT INTO schema_migrations (version) VALUES ('028_unify_tenant_rls');
END $$;
SQL
```

Apply 029:

```sh
set -o pipefail
curl -fsSL "$RAW/029_force_rls_app_role.up.sql" \
  | kubectl -n pravara-mes exec -i postgres-pravara-0 -c postgres -- sh -c "$PSQL -f -"
kubectl -n pravara-mes exec -i postgres-pravara-0 -c postgres -- sh -c "$PSQL" <<'SQL'
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'public' AND c.relrowsecurity AND NOT c.relforcerowsecurity)
     OR NOT has_table_privilege('pravara_app', 'public.machines', 'SELECT') THEN
    RAISE EXCEPTION '029 effect not present; not recording it';
  END IF;
  INSERT INTO schema_migrations (version) VALUES ('029_force_rls_app_role');
END $$;
SQL
```

The services still connect as the bootstrap superuser, which RLS never
applies to, so nothing changes for them yet. Smoke the API and the worker as
usual.

### Step 3: the switch, as a manifest change

The switch is a reviewed pull request to `infra/k8s/base/apps.yaml`, deployed
by the normal sync. Explicit `env` entries take precedence over `envFrom`, so
the bootstrap credentials stay in place for everything else:

```yaml
# pravara-api container
env:
  - name: DATABASE_URL
    valueFrom: { secretKeyRef: { name: pravara-secrets, key: APP_DATABASE_URL } }
# telemetry-worker container
env:
  - name: DATABASE_USER
    value: pravara_app
  - name: DATABASE_PASSWORD
    valueFrom: { secretKeyRef: { name: pravara-secrets, key: APP_DATABASE_PASSWORD } }
```

Verify from the application's connection after the rollout:

```sql
SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user;
-- expect: f | f
```

Then smoke: tenant-scoped reads return the tenant's data, telemetry keeps
arriving, and command delivery keeps moving.

### Longer-term option: platform-managed Postgres

Moving pravara onto the shared, platform-managed Postgres would let
`enclii onboard ensure --db-name <db> --app-role <db>_app --app-role-connection-limit <n>`
create the role, generate its password, add the pooler entry and write the URL
into the Secret in one step, with no break-glass shell. That move is a data
migration with its own downtime and pool resizing (the shared instance caps a
role at 20 connections), so it is not part of this rollout.

**Recommendation:** do the in-pod rollout above now, because the data lives in
`postgres-pravara`. Plan the move to the platform-managed Postgres as a
separate change, after the worker pool is configurable.

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

| Situation | Action |
|-----------|--------|
| Problem after the switch (step 3) | Revert the manifest pull request; the services return to the bootstrap credentials. No database change |
| Problem after 029 | Stream `029_force_rls_app_role.down.sql` through the pod as in step 2, then `DELETE FROM schema_migrations WHERE version = '029_force_rls_app_role'`. It removes `FORCE` and the `pravara_app` grants and keeps the role |
| Problem after 028 | The same with `028_unify_tenant_rls.down.sql`; it restores the previous policies exactly |
| Drop the role entirely | `DROP ROLE pravara_app` after 029's down, then remove the two `APP_DATABASE_*` keys from `pravara-secrets` |
