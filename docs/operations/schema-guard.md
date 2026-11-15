# Schema guard

> Public document. Placeholders only: no hostnames, credentials or
> environment-specific values belong here.

`pravara-api` does not accept traffic until the database has every migration
its build was made with. Code: `apps/pravara-api/internal/db/schema_guard.go`.

## Why

- A merge to `main` builds the image, pins its digest and rolls it out. There
  is no separate deploy step.
- Production migrations are applied by hand
  ([database-app-role.md](./database-app-role.md)); the deploy does not run
  them.

So an image can reach production before its schema. Its queries then fail on
columns, tables or functions that do not exist yet. On 2026-11-14 an image
that needed migration 035 ran in production for about 25 minutes without it.

## What it does

The `*.up.sql` files are embedded in the binary. The guard compares their
names with the `version` column of `schema_migrations`, which
`infra/db/migrate.sh` and the hand-apply runbook both write.

| Endpoint | With the database behind the build |
|---|---|
| `GET /health/live` | `200`. The process stays up. |
| `GET /health/ready` | `503`, `reason: schema_behind` and a count of missing migrations. |
| `GET /health` | `503`, component `schema` is `behind`. |

The Kubernetes readiness probe is `/health/ready`; startup and liveness are
`/health/live`. With the Deployment's rolling update the effect is:

1. The new pod starts, stays alive and never becomes ready.
2. The previous pod keeps serving. Nobody sees the new code.
3. The new pod asks the database again every 10 seconds. Once the migration is
   recorded it becomes ready and the rollout completes, with no restart.

A database that is **ahead** of the build (a code rollback after a migration)
is fine: only missing migrations count.

If the guard cannot read `schema_migrations` at all, the reason is
`schema_unverified` and the instance is also not ready: an instance that
cannot tell must not serve. If the table does not exist, every migration
counts as missing.

Which migrations are missing is written to the log, once per change of state,
and not to the endpoints:

```text
Schema guard: the database is behind this build; not accepting traffic until
these migrations are applied   missing_migrations=[037_example]
```

## When a rollout waits

Signs: the new `pravara-api` pod shows `0/1` ready and keeps running; the
Deployment reports that it is still progressing, then that its progress
deadline was exceeded.

1. Read the new pod's log for the `Schema guard` line. It names the
   migrations.
2. Apply them with the runbook in
   [database-app-role.md](./database-app-role.md), each followed by its guarded
   `INSERT INTO schema_migrations`.
3. Do nothing else. Within about 10 seconds the pod becomes ready and the old
   pod is retired.

If the image should not go out after all, revert the merge; the waiting pod is
replaced by the next rollout.

**Apply migrations before merging when you can.** Every migration in this
repository is written to be compatible with the code before it, so that order
is always safe. The guard is for when that step is forgotten.

## Setting

`SCHEMA_GUARD`:

| Value | Behaviour |
|---|---|
| `enforce` (default) | Not ready while a migration is missing. |
| `warn` | Ready; the state is logged and shown on `/health`. |
| `off` | The guard does not look. |

Use `warn` for a local database that was not migrated with
`infra/db/migrate.sh`, since it has no `schema_migrations` table.

## The application role

The guard needs `SELECT` on `schema_migrations`. The bootstrap role production
connects as today has it. The `pravara_app` role gets it from
`infra/db/roles/pravara_app_grants.sql`, and nothing more: it can read which
migrations are recorded and can never record one. Migration 029 revokes
everything on that table from the role, so **apply the grants file after
creating the role**, as the role runbook already requires. Without the grant
the API reports `schema_unverified` and does not become ready.

## What it does not cover

- **Other services.** `telemetry-worker` uses the same database and has no
  guard.
- **What a migration contains.** The guard trusts the recorded version. A row
  inserted without running the file passes; the runbook's guarded `INSERT`
  exists to prevent that.
- **Migrations that are not backward compatible.** While a rollout waits, the
  previous pod keeps running against whatever schema is there. A migration
  that breaks the code before it would break that pod when applied.
- **A migration edited after it was applied.** Only names are compared.
