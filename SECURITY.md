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
