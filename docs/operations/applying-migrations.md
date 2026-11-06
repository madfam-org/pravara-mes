# Applying migrations in production

> Public document. Placeholders only: no hostnames, credentials or
> environment-specific values belong here.

Merging to `main` deploys code. It does **not** apply migrations. The API
image has no `psql`, and no platform adapter reaches the in-namespace
PostgreSQL (`postgres-pravara`, database `pravara_mes`), so an operator applies
each migration through the database pod's own `psql`, in the platform's
documented break-glass shell. A migration that code depends on must be applied
**before** the merge that deploys that code.

Related: [database-app-role.md](./database-app-role.md) (row-level security,
the `pravara_app` role and the switch to it), `infra/db/migrate.sh` (the runner
for databases you can reach with a URL).

## Procedure for one migration

1. Run the read-only check (step 0 below): the version is not recorded, its
   objects do not exist and its prerequisites do.
2. Fetch the file at a pinned commit **on the cluster host**, stream it into the
   pod, and check its size and SHA-256 **inside the pod** before `psql -f` runs
   it. Every migration from 028 on wraps itself in `BEGIN … COMMIT`, so a
   failure leaves nothing behind.
3. Record the version with a guarded `INSERT`, exactly as `infra/db/migrate.sh`
   records it, but only if the migration's effect is present.
4. Run the read-only check again.

Do not send a file through a remote shell's stdin
(`ssh … kubectl exec -i … < file`): that path has been seen to cut a file
silently at 32 KiB. The commands below send only the small script over the
remote shell; the file travels from the host's own `curl` into the pod and is
verified there.

```sh
# One migration. kubectl may need sudo where its config is root-only.
set -euo pipefail
K="kubectl -n pravara-mes"; P=postgres-pravara-0; PODDIR=/tmp
SHA=<pinned commit>
F=<version>.up.sql
SUM=<sha256 of the file at SHA>
curl -fsSL "https://raw.githubusercontent.com/madfam-org/pravara-mes/$SHA/apps/pravara-api/internal/db/migrations/$F" \
  | $K exec -i "$P" -c postgres -- env FILE="$PODDIR/$F" sh -c 'cat > "$FILE"'
$K exec "$P" -c postgres -- env FILE="$PODDIR/$F" SUM="$SUM" sh -euc '
  wc -c "$FILE"
  echo "$SUM  $FILE" | sha256sum -c -
  psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d pravara_mes -f "$FILE"
' </dev/null
$K exec -i "$P" -c postgres -- sh -c 'psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d pravara_mes' <<'SQL'
-- the migration's guarded record (below)
SQL
```

`</dev/null` keeps a command that does not need stdin from reading the rest of
a script that arrives on stdin (`bash -s`). Paste one step at a time; never a
step together with the next one.

## Catch-up of 2026-11-05: 029, then 032, then 033

**State found.** `schema_migrations` ended at `031_sparkplug_registry`,
`030_task_command_delivery`, `028_unify_tenant_rls`: **029 and 032 were not
applied**, while the deployed code (fabrication dispatch, deployed since
2026-10-14) uses 032's tables. If called, `POST /v1/match`,
`GET /v1/dispatches` and `GET /v1/dispatches/:id` fail with «relation does not
exist» (500). The dispatch runner does not start unless `DISPATCH_ENABLED=true`.
The API logs available on 2026-11-05 (about 18 hours) held only health probes,
all 200, so no request failed in that window.

**Why it matters now.** [#65](https://github.com/madfam-org/pravara-mes/pull/65)
adds `033_job_consumption_engagement`, which references `dispatch_jobs`, and
its code reads `orders.engagement_slug` on every order read. 029, 032 and 033
must all be in production before #65 merges.

**Why 029 is safe to apply now.**

- The services connect as the bootstrap role, a superuser
  ([database-app-role.md](./database-app-role.md)). Superusers are exempt from
  row-level security, `FORCE` included, so 029 changes nothing for them. Gate G4
  below confirms the connection role before anything is written.
- 029 does not need `pravara_app`. Without the role it sets `FORCE` and prints a
  `NOTICE`; the grants follow when the role is created
  (`infra/db/roles/pravara_app_grants.sql`, the same grants). Creating the role
  and switching the services to it stay steps 1 and 3 of
  [database-app-role.md](./database-app-role.md), a separate rollout.
- Applying 029 after 030 and 031 reaches the same end state as the file order:
  030 changes no RLS, 031 already forces its own three tables, 029 forces the
  rest.

### Files

All files are well under 32 KiB. 029 and 032 are byte-identical at `620d759d`
(the deployed image), `9ab38e6` and `7175b5c7` (#65's head).

| Step | File | Commit | Bytes | SHA-256 |
|---|---|---|---|---|
| 1 | `029_force_rls_app_role.up.sql` | `620d759db8084c67a9bb98cd43bfd0232b4c4c52` | 1,986 | `de9973cdfebb57320e27119dd7eccc2fdb1b95cd3552a084106a8ef784dc5705` |
| 2 | `032_dispatch_matchmaking_passport.up.sql` | `620d759db8084c67a9bb98cd43bfd0232b4c4c52` | 9,500 | `5164da2b0aa418bab409940277fe50fa0c2f4063539d218b830c64a3aafbd713` |
| 3 | `033_job_consumption_engagement.up.sql` | `7175b5c75cc00c9950b71ef9ce2cfaf3a3af6d69` | 8,634 | `3995dccefc5cec2b39ac80c6ef1dba280e1a927a169fefe21f9b485d0657b34a` |
| Rollback 3 | `033_job_consumption_engagement.down.sql` | `7175b5c75cc00c9950b71ef9ce2cfaf3a3af6d69` | 701 | `a779ca681c3ed6e6880da43546a437c6e1f214e37cc2dc55090255ca361b4db5` |
| Rollback 2 | `032_dispatch_matchmaking_passport.down.sql` | `620d759db8084c67a9bb98cd43bfd0232b4c4c52` | 513 | `9e58034c09c23dc60b030f03c9312e6816015434235ac479d5972497182966cc` |
| Rollback 1 | `029_force_rls_app_role.down.sql` | `620d759db8084c67a9bb98cd43bfd0232b4c4c52` | 1,037 | `5aeaf6a6bba17ca9ea00225fd3e68e4983f405730a626ded0291da5b08714b98` |

### Step 0: read-only check

Runs in a read-only transaction. Prints no secret.

```sh
K="kubectl -n pravara-mes"; P=postgres-pravara-0
$K get pods -o custom-columns=NAME:.metadata.name,IP:.status.podIP,PHASE:.status.phase,STARTED:.status.startTime </dev/null
$K exec -i "$P" -c postgres -- sh -c 'psql -X -At -F " | " -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d pravara_mes' <<'SQL'
BEGIN READ ONLY;
SELECT 'server', current_setting('server_version');
SELECT 'runner', current_user, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user;
SELECT 'migration', version, applied_at FROM schema_migrations ORDER BY version DESC LIMIT 8;
SELECT 'recorded_029_032_033', count(*) FROM schema_migrations
 WHERE version IN ('029_force_rls_app_role', '032_dispatch_matchmaking_passport', '033_job_consumption_engagement');
SELECT 'new_table', t, to_regclass('public.' || t) IS NOT NULL
  FROM unnest(ARRAY['dispatch_jobs', 'machine_reservations', 'manufacturing_records', 'passport_outbox', 'job_consumption']) t;
SELECT 'orders_engagement_slug', EXISTS (SELECT 1 FROM information_schema.columns
 WHERE table_schema = 'public' AND table_name = 'orders' AND column_name = 'engagement_slug');
SELECT 'prereq_table', t, to_regclass('public.' || t) IS NOT NULL
  FROM unnest(ARRAY['tenants', 'tasks', 'orders', 'order_items', 'product_definitions', 'machines',
                    'product_genealogy', 'print_jobs', 'inventory_transactions', 'task_commands',
                    'edge_nodes', 'machine_live_state']) t;
SELECT 'prereq_function', p, to_regprocedure(p) IS NOT NULL
  FROM unnest(ARRAY['app_current_tenant_id()', 'app_system_scope()', 'gen_random_uuid()']) p;
SELECT 'rls', count(*) FILTER (WHERE c.relrowsecurity) AS enabled,
       count(*) FILTER (WHERE c.relrowsecurity AND c.relforcerowsecurity) AS forced,
       count(*) FILTER (WHERE c.relrowsecurity AND NOT c.relforcerowsecurity) AS not_forced
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p');
SELECT 'forced_tables', string_agg(c.relname, ',' ORDER BY c.relname)
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND c.relforcerowsecurity;
SELECT 'policy', policyname, count(*) FROM pg_policies WHERE schemaname = 'public' GROUP BY policyname ORDER BY policyname;
SELECT 'login_role', rolname, rolsuper, rolbypassrls, rolinherit, rolconnlimit
  FROM pg_roles WHERE rolcanlogin OR rolname = 'pravara_app' ORDER BY rolname;
SELECT 'table_owner', t.tableowner, r.rolsuper, count(*)
  FROM pg_tables t JOIN pg_roles r ON r.rolname = t.tableowner
 WHERE t.schemaname = 'public' GROUP BY t.tableowner, r.rolsuper;
SELECT 'connection', a.usename, r.rolsuper, r.rolbypassrls, coalesce(host(a.client_addr), 'local'), count(*)
  FROM pg_stat_activity a LEFT JOIN pg_roles r ON r.rolname = a.usename
 WHERE a.datname = 'pravara_mes' AND a.pid <> pg_backend_pid()
 GROUP BY a.usename, r.rolsuper, r.rolbypassrls, a.client_addr ORDER BY 2, 5;
SELECT 'size', pg_size_pretty(pg_database_size('pravara_mes')),
       (SELECT count(*) FROM orders) AS orders, (SELECT count(*) FROM inventory_transactions) AS inventory_transactions;
ROLLBACK;
SQL
```

Go only if every gate holds:

| Gate | Expect | If not |
|---|---|---|
| G1 | `recorded_029_032_033 \| 0`; every `new_table … \| f`; `orders_engagement_slug \| f` | Stop: an earlier partial attempt must be reconciled first |
| G2 | every `prereq_table` and `prereq_function` row ends in `t` | Stop |
| G3 | `runner \| … \| t \| t` (the pod's `POSTGRES_USER` is a superuser) | Stop |
| G4 | every `connection` row from the API and telemetry-worker pod IPs has `rolsuper` or `rolbypassrls` = `t` | Stop: 029 would start enforcing RLS on that role, which is the switch, not a catch-up |
| G5 | `rls \| 52 \| 3 \| 49`; policies `system_scope_read 6`, `tenant_isolation 52`, `tenant_references 38` | Stop: production differs from the repository's schema through 031; compare first |

### Step 0b (recommended): dump before writing

No backup job for `postgres-pravara` is defined in this repository. A custom
format dump on the database volume is a cheap restore point:

```sh
$K exec "$P" -c postgres -- env VOL=/var/lib/postgresql/data sh -euc '
  f="$VOL/pre-catchup-029-032-033-20261105.dump"
  df -h "$VOL" | tail -1
  pg_dump -U "$POSTGRES_USER" -d pravara_mes -Fc -f "$f"
  ls -l "$f"
  echo "tables with data in the dump: $(pg_restore -l "$f" | grep -c "TABLE DATA")"
' </dev/null
```

### Step 1: 029

Apply `029_force_rls_app_role.up.sql` at `620d759d` with the procedure above,
then record it:

```sql
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
               AND c.relrowsecurity AND NOT c.relforcerowsecurity) THEN
    RAISE EXCEPTION '029 effect not present (a table has RLS without FORCE); not recording it';
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
    IF NOT has_table_privilege('pravara_app', 'public.machines', 'SELECT') THEN
      RAISE EXCEPTION '029 grants missing for the existing role pravara_app; not recording it';
    END IF;
  END IF;
  INSERT INTO schema_migrations (version) VALUES ('029_force_rls_app_role');
  RAISE NOTICE 'recorded 029_force_rls_app_role';
END $$;
```

### Step 2: 032

Apply `032_dispatch_matchmaking_passport.up.sql` at `620d759d`, then:

```sql
DO $$
DECLARE
  t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['dispatch_jobs', 'machine_reservations', 'manufacturing_records', 'passport_outbox'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = 'public' AND c.relname = t
                     AND c.relrowsecurity AND c.relforcerowsecurity) THEN
      RAISE EXCEPTION '032 effect not present: % is missing or not under FORCE RLS', t;
    END IF;
    IF (SELECT count(*) FROM pg_policies WHERE schemaname = 'public' AND tablename = t
          AND policyname IN ('tenant_isolation', 'tenant_references')) <> 2 THEN
      RAISE EXCEPTION '032 effect not present: % lacks tenant_isolation or tenant_references', t;
    END IF;
  END LOOP;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger
                 WHERE tgname = 'trg_manufacturing_records_append_only' AND NOT tgisinternal) THEN
    RAISE EXCEPTION '032 effect not present: append-only trigger missing';
  END IF;
  INSERT INTO schema_migrations (version) VALUES ('032_dispatch_matchmaking_passport');
  RAISE NOTICE 'recorded 032_dispatch_matchmaking_passport';
END $$;
```

### Step 3: 033

Apply `033_job_consumption_engagement.up.sql` at `7175b5c7`, then:

```sql
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_schema = 'public' AND table_name = 'orders' AND column_name = 'engagement_slug') THEN
    RAISE EXCEPTION '033 effect not present: orders.engagement_slug missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint
                 WHERE conname = 'orders_engagement_slug_format' AND conrelid = 'public.orders'::regclass) THEN
    RAISE EXCEPTION '033 effect not present: orders_engagement_slug_format missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                 WHERE n.nspname = 'public' AND c.relname = 'job_consumption'
                   AND c.relrowsecurity AND c.relforcerowsecurity) THEN
    RAISE EXCEPTION '033 effect not present: job_consumption missing or not under FORCE RLS';
  END IF;
  IF (SELECT count(*) FROM pg_policies WHERE schemaname = 'public' AND tablename = 'job_consumption'
        AND policyname IN ('tenant_isolation', 'tenant_references')) <> 2 THEN
    RAISE EXCEPTION '033 effect not present: job_consumption policies missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger
                 WHERE tgname = 'trg_job_consumption_append_only' AND NOT tgisinternal) THEN
    RAISE EXCEPTION '033 effect not present: append-only trigger missing';
  END IF;
  INSERT INTO schema_migrations (version) VALUES ('033_job_consumption_engagement');
  RAISE NOTICE 'recorded 033_job_consumption_engagement';
END $$;
```

### After step 3

Step 0 again shows `recorded_029_032_033 | 3`, every `new_table` and
`orders_engagement_slug` `t`, `rls | 57 | 57 | 0`, and policies
`system_scope_read 6`, `tenant_isolation 57`, `tenant_references 43`. The
running code ignores the new column and tables, so nothing changes for it.

### Then merge #65

The merge runs Build and Deploy, which builds pravara-api and commits its digest
to `infra/k8s/production/kustomization.yaml` as
«deploy: update digests to <merge sha, 8 characters>»; Argo CD syncs that
commit. Check that the run's «Commit Image Digests» job succeeded and that the
commit exists. If the job fails at «Setup Kustomize» (the kustomize installer is
fetched from GitHub without a token and can be rate-limited, as on 2026-11-04),
re-run the failed jobs a few minutes later; the built digests are reused.

### Rollback (reverse order only)

Each down file is applied with the same procedure (pinned commit, in-pod
checksum), then un-recorded with a guard that checks the down took effect.

- **033.** If #65's code is deployed, revert it first and wait for the rollout:
  that code reads `orders.engagement_slug`. The down drops `job_consumption`
  (export its rows first if a ledger has not read them).

  ```sql
  DO $$
  DECLARE
    deleted int;
  BEGIN
    IF to_regclass('public.job_consumption') IS NOT NULL
       OR EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_schema = 'public' AND table_name = 'orders' AND column_name = 'engagement_slug') THEN
      RAISE EXCEPTION '033 down not complete; leaving it recorded';
    END IF;
    DELETE FROM schema_migrations WHERE version = '033_job_consumption_engagement';
    GET DIAGNOSTICS deleted = ROW_COUNT;
    RAISE NOTICE 'unrecorded 033_job_consumption_engagement (% row)', deleted;
  END $$;
  ```

- **032.** Only after 033 is rolled back: `job_consumption` references
  `dispatch_jobs`, and the down fails while it exists. Drops dispatch rows,
  manufacturing records and undelivered passport rows.

  ```sql
  DO $$
  DECLARE
    deleted int;
  BEGIN
    IF to_regclass('public.dispatch_jobs') IS NOT NULL OR to_regclass('public.machine_reservations') IS NOT NULL
       OR to_regclass('public.manufacturing_records') IS NOT NULL OR to_regclass('public.passport_outbox') IS NOT NULL THEN
      RAISE EXCEPTION '032 down not complete; leaving it recorded';
    END IF;
    DELETE FROM schema_migrations WHERE version = '032_dispatch_matchmaking_passport';
    GET DIAGNOSTICS deleted = ROW_COUNT;
    RAISE NOTICE 'unrecorded 032_dispatch_matchmaking_passport (% row)', deleted;
  END $$;
  ```

- **029.** The down removes `FORCE` from **every** table, including the ones
  031, 032 and 033 force themselves, and revokes `pravara_app`'s grants (the
  role stays). Un-record it, then restore `FORCE` where those migrations set it:

  ```sql
  DO $$
  DECLARE
    deleted int;
  BEGIN
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND c.relforcerowsecurity) THEN
      RAISE EXCEPTION '029 down not complete (a table is still FORCE); leaving it recorded';
    END IF;
    DELETE FROM schema_migrations WHERE version = '029_force_rls_app_role';
    GET DIAGNOSTICS deleted = ROW_COUNT;
    RAISE NOTICE 'unrecorded 029_force_rls_app_role (% row)', deleted;
  END $$;
  DO $$
  DECLARE
    t text;
  BEGIN
    FOREACH t IN ARRAY ARRAY['edge_nodes', 'edge_enrollments', 'machine_live_state',
                             'dispatch_jobs', 'machine_reservations', 'manufacturing_records',
                             'passport_outbox', 'job_consumption'] LOOP
      IF to_regclass('public.' || t) IS NOT NULL THEN
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
      END IF;
    END LOOP;
  END $$;
  ```

### How this was tested

A scratch PostgreSQL 16.2 replayed production's order (001–027, 030, 028, 031),
then ran the commands on this page as written, with the files fetched from
GitHub at the pinned commits. Confirmed: each guard refuses when its effect is
missing; a checksum mismatch stops before `psql`; re-running 032 fails and
changes nothing; 032's down refuses while 033 exists; the full rollback returns
to `rls | 52 | 3 | 49`; re-applying works; with `pravara_app` present the grants
reach every table and none reach `schema_migrations`. The scratch server lacked
`uuid-ossp` and `pgcrypto`; no migration calls their functions.

## Limits of this procedure

- No platform adapter reaches the in-namespace database, so migrations (and the
  `pravara_app` role and its password) need the break-glass shell. A migration
  Job with `psql` is the follow-up noted in
  [database-app-role.md](./database-app-role.md).
- Nothing stops code from deploying ahead of its schema; 029 and 032 stayed
  unapplied for three weeks after the code that needs 032 shipped.
