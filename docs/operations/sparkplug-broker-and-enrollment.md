# Sparkplug broker authentication and edge enrollment

How pravara's Sparkplug B (MES-1) pieces fit together on the platform side:
the broker's HTTP authentication and authorization, edge-node enrollment,
the primary host in the telemetry worker, and the rollout order. Site-box
steps are in [`deploy/edge/README.md`](../../deploy/edge/README.md).

> Boundary: values below are placeholders. Secrets live in the cluster
> Secret and are set through Enclii, never in this repository.

## Pieces

| Piece | Where | What it does |
|---|---|---|
| Registry | migration `031_sparkplug_registry` | `machines.sparkplug_edge_id`; `edge_nodes` (one MQTT credential per site box, bcrypt hash only, `disabled_at` revokes); `edge_enrollments`; Sparkplug quarantine columns on `discovered_machines`; `machine_live_state`. All tenant tables use the 028/029 row-level security pattern. |
| Broker auth | pravara-api internal listener (`INTERNAL_HTTP_PORT`, default 4510; Service `pravara-api-internal`) | `POST /v1/mqtt/auth` and `POST /v1/mqtt/acl` for the EMQX HTTP authenticator and authorizer. Every call carries `X-Pravara-Internal-Key` (Secret key `MQTT_AUTH_INTERNAL_KEY`); without a configured key every call is refused. The ACL is `sparkplug.EdgeNodeACL` of the credential's own tenant and edge node; the topic never chooses the tenant. Unknown usernames get `ignore`. |
| Enrollment | pravara-api | `POST /v1/edge/enrollments` and `GET /v1/edge/enrollments/:id` (public; the box has no credential yet), `POST /v1/edge/enrollments/approve` and `POST /v1/edge/nodes/:id/disable` (people only, admin role), `GET /v1/edge/nodes`, `GET /v1/edge/enrollments`. |
| Registry and live state | pravara-api | `PUT /v1/machines/:id/sparkplug` (attach a machine to an edge node), `GET /v1/machines/:id/live-state`, `GET /v1/edge/live-state`. |
| Primary host | telemetry-worker (`PRAVARA_SPARKPLUG_*`, off by default) | `packages/sparkplug/host`: STATE, births/deaths/data of registered edge nodes, live state, quarantine, Command/Job binding to the command ledger, DCMD delivery and `machine.job_completed`. |

## Why a shared key and a separate listener

The broker endpoints are served on their own port, which no Ingress or tunnel
route points at, and every call must present the shared key. NetworkPolicies
(`pravara-api-ingress` admits the broker on 4510; `emqx-egress`) narrow the
path further, but they depend on the cluster's CNI enforcing them and on pod
labels; the key authenticates the caller regardless of network topology and
fails closed when unset.

## Enrollment flow (no person holds the credential)

1. The box runs `pravara-edge -enroll`. It generates 32 random bytes as its
   MQTT password, writes them to its state directory (0600) and sends them
   once, over HTTPS, to `POST /v1/edge/enrollments` with its `group_id`
   (tenant slug) and `edge_node_id`.
2. pravara-api stores a bcrypt hash in `edge_enrollments` (pending, 15
   minutes) and answers with a user code such as `BCDF-GHJK`. The box prints
   the code; it never prints the password.
3. A tenant admin approves the code for that edge node. Row-level security
   limits the match to the admin's own tenant; API keys and service tokens
   are refused even with the wildcard scope.
4. The hash moves to `edge_nodes`; the enrollment row keeps no hash. The box's
   poll returns `approved`; the edge node connects as
   `edge:<group_id>:<edge_node_id>`.

Re-enrollment of the same edge node rotates the credential once approved.
`POST /v1/edge/nodes/:id/disable` revokes it: the next CONNECT and every
publish or subscribe are denied.

## EMQX configuration (operator)

Edge-node credentials are decided by pravara; the host application's own
credential and any other internal client stay in the broker's built-in
database. Order matters: the built-in database first, then HTTP.

```hocon
authentication = [
  { mechanism = password_based, backend = built_in_database, user_id_type = username,
    password_hash_algorithm { name = bcrypt } },
  { mechanism = password_based, backend = http, method = post,
    url = "http://pravara-api-internal.pravara-mes.svc:4510/v1/mqtt/auth",
    headers { "content-type" = "application/json", "X-Pravara-Internal-Key" = "<MQTT_AUTH_INTERNAL_KEY>" },
    body { username = "${username}", password = "${password}", clientid = "${clientid}" },
    request_timeout = "5s" }
]

authorization {
  no_match = deny
  cache { enable = true, max_size = 32, ttl = "1m" }
  sources = [
    { type = built_in_database },
    { type = http, method = post,
      url = "http://pravara-api-internal.pravara-mes.svc:4510/v1/mqtt/acl",
      headers { "content-type" = "application/json", "X-Pravara-Internal-Key" = "<MQTT_AUTH_INTERNAL_KEY>" },
      body { username = "${username}", topic = "${topic}", action = "${action}", clientid = "${clientid}" },
      request_timeout = "5s" }
  ]
}
```

Built-in database rules for the primary host credential:

```text
publish   spBv1.0/STATE/pravara-mes
publish   spBv1.0/+/NCMD/+
publish   spBv1.0/+/DCMD/+/+
subscribe spBv1.0/#
```

Verify the field names against the pinned EMQX release before applying, and
keep `no_match = deny`: with `allow`, any authenticated client that no source
knows would be authorized for everything.

## Primary host (telemetry worker)

```text
PRAVARA_SPARKPLUG_ENABLED=true
PRAVARA_SPARKPLUG_BROKER_URL=ssl://<broker>:8883   # tcp:// only for an in-cluster listener
PRAVARA_SPARKPLUG_USERNAME=<host username>        # built-in database user, not an edge credential
PRAVARA_SPARKPLUG_PASSWORD=<from the Secret>
PRAVARA_SPARKPLUG_CA_FILE=/etc/pravara/broker-ca.pem
PRAVARA_SPARKPLUG_TLS_SERVER_NAME=<broker certificate name>
```

With the host disabled, commands for Sparkplug-registered machines fail
visibly ("primary host is not enabled") instead of using the legacy command
topic.

## Not covered here

- The Cloudflare Access application for the broker hostname and one Access
  service token per site box are platform items (Enclii). The box reads the
  token from `/etc/pravara-edge/cloudflared.env`; issuing it is not automated
  by pravara.
- The EMQX TLS listener reachable through Access is a platform item.
