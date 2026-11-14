# Sign-in tokens

> Public document. Placeholders only: no hostnames, credentials or
> environment-specific values belong here.

How `pravara-api` decides that a bearer token is a person or a machine it
should serve. Code: `apps/pravara-api/internal/auth/oidc.go`.

## Every token

RS256 only, signed by a key the issuer publishes for signing in its JWKS
(`OIDC_JWKS_URL`), carrying an expiry that has not passed and the configured
issuer. A token that names a key ID is checked against that key. A token that
names none is checked against every published RSA signing key.

## Machine or person

A token with a machine marker (`token_use: client_credentials`,
`actor_type: service_account` or a `service-account:` subject) is a machine's.
Anything else is a person's.

| | Machine | Person |
|---|---|---|
| Issued by | Janua, `client_credentials` grant | Janua, a sign-in |
| Audience | `OIDC_AUDIENCE` (`pravara-api`) only | `OIDC_AUDIENCE`, or one of `OIDC_SESSION_AUDIENCES` for a first-party session |
| Authority | the token's `scope` | `pravara-mes:<role>` application roles |

## A person's token

The same rules apply whichever accepted audience the token names:

1. Its `type` claim is `access`. A refresh token has the same signature and
   audience and is refused.
2. It is not a Janua service principal (`is_service_account`). Technical
   logins use a `client_credentials` token.
3. Its `roles` claim holds at least one `<OIDC_SESSION_ROLE_APP>:<role>` entry
   (`pravara-mes:admin`, `pravara-mes:inventory_manager`, ...).

The roles the request then has are exactly those entries with the prefix
removed. Everything else in the claim is dropped. Janua's `roles` claim mixes
application roles of every product with organization-account roles, so
another product's role (`tulana:admin`) and a bare name (an
organization-account `admin` or `owner`) grant nothing here.

### Session audiences

Janua signs the access token of a sign-in in the app with its own audience,
not this API's. `OIDC_SESSION_AUDIENCES` lists the audiences accepted for
that case (production: `janua.dev`). The list is empty by default: unless it
is set, only `OIDC_AUDIENCE` is accepted.

That audience is shared: other products use it, and so does any OAuth client
registered without an audience of its own. So a token accepted on a session
audience must also be a first-party session token, with **no `client_id` and
no `scope`**. Every token Janua issues to an OAuth client carries both, which
keeps a token obtained by some other application from being replayed here.

## Answers

| Answer | When |
|---|---|
| `401 unauthorized` | bad signature, no or past expiry, wrong issuer, wrong audience, wrong `type`, a service principal, an OAuth client's token on a session audience |
| `403 forbidden`, code `no_application_role` | a valid person's token whose holder has no `pravara-mes:*` role |
| `403 forbidden` | no `tenant_id` claim |

## Granting access

Application roles are granted in Janua, per organization membership, as the
pair app `pravara-mes` and a role name. The role names this API reads today:

| Role | Allows |
|---|---|
| `admin` | everything a tenant administrator may do, including inventory writes and API keys |
| `inventory_manager` | receipts, physical counts and the material registry ([inventory.md](./inventory.md)) |
| any other name | signing in and every route that has no role gate |

## Limits to know

- **A shared audience can be replayed between products.** A sibling product
  that receives a person's first-party session token could present it here,
  and it would be accepted if that person holds a `pravara-mes` role. Closing
  this needs Janua to issue the app's session with this API's audience.
- **Revocation is not immediate.** Verification is offline. After a sign-out,
  a removed membership or a revoked role, a token already issued keeps
  working here until it expires. Role changes reach a token at its next
  refresh.
- **Key rotation.** Published keys are cached for an hour and are not
  re-fetched when a token names an unknown key.

## Tenant

The request runs in the tenant named by the token's `tenant_id` claim. See
[database-app-role.md](./database-app-role.md) for how that is enforced in the
database, and for the state of that enforcement in production.
