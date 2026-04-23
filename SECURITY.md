# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Pravara MES, please report it responsibly:

1. **Do not** open a public GitHub issue
2. Email **security@madfam.io** with details
3. Include steps to reproduce if possible
4. We will acknowledge receipt within **48 hours**

## Sensitive Data

Pravara MES handles sensitive manufacturing and operational data including:

- **Machine telemetry** -- IoT sensor readings from production lines (temperature, pressure, vibration, cycle counts)
- **MQTT/EMQX broker credentials** -- connection strings and authentication tokens for the message broker
- **Production line operational data** -- work order details, OEE metrics, downtime events, quality records
- **Tenant-specific data** -- multi-tenant manufacturing data that must remain isolated between organizations
- **Database credentials** -- PostgreSQL and Redis connection strings
- **API keys and tokens** -- service-to-service authentication, webhook secrets

## Rules

- IoT device credentials and MQTT passwords must **never** be committed to version control
- MQTT broker credentials must be stored as **Kubernetes Secrets only** -- never in ConfigMaps or environment files checked into Git
- Telemetry data must **not leak between tenants** -- all queries must be scoped to the authenticated tenant
- Database credentials and connection strings must be injected via environment variables or external secret operators, never hardcoded
- Logs must **never** contain passwords, tokens, private keys, or raw connection strings
- API tokens for webhook callbacks must be rotated regularly and stored encrypted at rest

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Known Issues — Audit 2026-04-23

See `/Users/aldoruizluna/labspace/claudedocs/ECOSYSTEM_AUDIT_2026-04-23.md` for the full ecosystem audit.

- ~~**🔴 R2: `eval()` on untrusted Redis pubsub data**~~ — Fixed 2026-04-23 in `apps/ml-orchestrator/main.py`: `json.loads` with malformed-payload drop + type guard.
- **🟠 H11: Placeholder secrets in kustomize base** — `infra/k8s/base/secrets.yaml:18-26` contain `REPLACE_WITH_JWT_SECRET`-style literals. If overlay doesn't override, the literal string becomes the prod secret. Move to SealedSecrets/External Secrets; add CI grep for `REPLACE_WITH_` outside `tests/`.
- **🟡 M5: Path traversal on gcode uploads** — `apps/luban-bridge/src/routes/gcode.ts:30, 52, 74` reads `req.file.path` without verifying resolved path stays under allowlisted upload dir. Use `path.resolve(UPLOAD_DIR, path.basename(req.file.filename))` with prefix check.
