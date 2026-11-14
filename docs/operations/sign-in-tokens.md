# Sign-in tokens

> Public document. Placeholders only: no hostnames, credentials or
> environment-specific values belong here.

How `pravara-api` decides that a bearer token is a person or a machine it
should serve. Code: `apps/pravara-api/internal/auth/oidc.go`.

## Two kinds of token

| | Machine | Person |
|---|---|---|
| Issued by | Janua, `client_credentials` grant | Janua, a sign-in in the app |
| Audience | `OIDC_AUDIENCE` (`pravara-api`) | one of `OIDC_SESSION_AUDIENCES` (production: `janua.dev`) |
| Key ID in the header | yes | no |
| Authority | the token's `scope` | `pravara-mes:<role>` application roles |

Every token must be RS256, signed by a key published in the issuer's JWKS
(`OIDC_JWKS_URL`), unexpired, and carry the configured issuer. A token that
names a key ID is checked against that key. A token that names none is checked
against every published RSA signing key.

## A person's session token

Janua signs the access token of a sign-in with its own audience, not this
API's, and the same token is valid across the ecosystem's products. So a
session token is accepted only when all of these hold:

1. Its audience is listed in `OIDC_SESSION_AUDIENCES`. The list is empty by
   default: unless it is set, only `OIDC_AUDIENCE` is accepted.
2. Its `type` claim is `access`. A refresh token has the same signature and
   audience and is refused.
3. It carries no machine marker (`token_use`, `actor_type`, a
   `service-account:` subject). Machine tokens must name `OIDC_AUDIENCE`.
4. Its `roles` claim holds at least one `<OIDC_SESSION_ROLE_APP>:<role>` entry
   (`pravara-mes:admin`, `pravara-mes:inventory_manager`, ...).

The roles the request then has are exactly those entries with the prefix
removed. Everything else in the claim is dropped: another product's roles
(`tulana:admin`) and bare names (an organization-account `admin`) grant
nothing here.

| Answer | When |
|---|---|
| `401 unauthorized` | bad signature, expired, wrong issuer, wrong audience, wrong `type` |
| `403 forbidden`, code `no_application_role` | a valid session token whose holder has no `pravara-mes:*` role |
| `403 forbidden` | no `tenant_id` claim |

## Granting access

Application roles are granted in Janua, per organization membership, as the
pair app `pravara-mes` and a role name. The role names this API reads today:

| Role | Allows |
|---|---|
| `admin` | everything a tenant administrator may do, including inventory writes |
| `inventory_manager` | receipts, physical counts and the material registry ([inventory.md](./inventory.md)) |
| any other name | signing in and every route that has no role gate |

## Tenant

The request runs in the tenant named by the token's `tenant_id` claim. See
[database-app-role.md](./database-app-role.md) for how that is enforced in the
database, and for the state of that enforcement in production.
