# Pravara MES Public Repo Sanitization Contract

Date: 2026-06-01
Status: launch-blocking when Pravara MES is tied to manufacturing execution, quoting, telemetry, or fulfillment SKUs

## Position

Pravara MES affects fulfillment reality. Public repo sanitization must ensure manufacturing, telemetry, billing, SDK, and operational docs are safe and do not overpromise production execution.

## Current remediation posture

- Preliminary audit found no exact credential-signature matches in tracked current-tree files.
- No repo-level pass is granted until current-tree scan, history scan, public artifact review, and owner approval are recorded in Tulana.

## Launch-blocking checks

A Pravara-linked platform/SKU cannot pass Product/Offer GA public-repo sanitization until evidence confirms:

- MQTT, database, Redis, billing, and telemetry examples are placeholders or synthetic local values.
- Public docs clearly separate development-only examples from production fulfillment.
- No customer manufacturing data, machine identifiers, vendor data, or facility details are present.
- Fulfillment claims match actual production MES capability.
