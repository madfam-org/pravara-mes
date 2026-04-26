# CLAUDE.md

> **Stub**: This is a minimal CLAUDE.md created to anchor pricing + PMF cross-references for the Pravara MES product. The operator will fill in project overview, architecture, and development commands.

## Pricing & PMF Anchoring

- **Pricing source-of-truth**: `internal-devops/decisions/2026-04-25-tulana-ecosystem-pricing.md`. Pravara MES tiers (Tulana v0.1 recommended, MXN/mo): Starter 4,999 / Growth 14,999 / Enterprise 49,999. Confidence: low — needs validation with real users.
- **PMF measurement**: per RFC 0013, NPS + Sean Ellis + retention via `@madfam/pmf-widget` → Tulana `/v1/pmf/*` endpoints. Composite PMF Score informs price moves + sunset decisions.
