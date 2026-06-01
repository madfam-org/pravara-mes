# Pravara Mes Agent Operating Guide

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


<!-- MADFAM-AGENTS-CANONICAL v1 -->

This is the canonical instruction file for Claude, Codex, and any other LLM
agent working in this repository. `CLAUDE.md` is kept only as a compatibility
redirect and should not become the source of truth again.

## Required operating doctrine

- Read this file before making repo changes.
- Prefer existing repo conventions, scripts, and docs over introducing new
  patterns.
- Preserve user work and never revert unrelated changes.
- Treat production operations as Enclii-first: use Enclii web, API, or CLI for
  provisioning, deployment, observability, domains, secrets, provider
  operations, scaling, rollback, and remediation.
- Use direct `kubectl`, `helm`, SSH, provider CLIs/APIs, `docker exec`, or
  direct container access only for platform bootstrap or documented break-glass
  emergencies when Enclii is unavailable or lacks an implemented adapter.
- Record any missing Enclii adapter gap instead of normalizing raw production
  access in docs or runbooks.
- Treat this repository as physical-operations software. Do not run commands,
  scripts, tests, migrations, or adapters that can move machines, dispatch jobs,
  open network access to fabrication equipment, or change machine credentials
  unless the user explicitly asks and the command is scoped to simulator,
  dry-run, or a named safe environment.
- Keep environment examples placeholder-only. Do not add live credentials,
  token-shaped examples, machine connection strings, base64-encoded secrets, or
  production webhook URLs to docs, templates, workflow logs, issues, PRs, or
  LLM chat.

## Repo entrypoints

- `README.md`
- `ECOSYSTEM.md`
- `docs/`
- `infra/`
- `.github/workflows/`

## LLM context files

- `llms.txt` is the compact context index.
- `llms-full.txt` is the durable full-context map and operating contract.
- `AGENTS.md` is canonical for agent instructions.
- `CLAUDE.md` redirects here for Claude compatibility.

## Maintenance

Regenerate or repair these files with
`internal-devops/scripts/sync-agent-docs.py` from the labspace ecosystem.

---

## Legacy CLAUDE.md guidance imported on 2026-05-13

<!-- BEGIN LEGACY_CLAUDE_IMPORT -->

# CLAUDE.md

> **Stub**: This is a minimal CLAUDE.md created to anchor pricing + PMF cross-references for the Pravara MES product. The operator will fill in project overview, architecture, and development commands.

## Pricing & PMF Anchoring

- **Pricing source-of-truth**: `internal-devops/decisions/2026-04-25-tulana-ecosystem-pricing.md`. Pravara MES tiers (Tulana v0.1 recommended, MXN/mo): Starter 4,999 / Growth 14,999 / Enterprise 49,999. Confidence: low — needs validation with real users.
- **PMF measurement**: per RFC 0013, NPS + Sean Ellis + retention via `@madfam/pmf-widget` → Tulana `/v1/pmf/*` endpoints. Composite PMF Score informs price moves + sunset decisions.

## Marketing landing — `mes.madfam.io/landing`

Public marketing surface lives at `apps/pravara-ui/app/landing/` (own layout, no auth gate). Composition is in `apps/pravara-ui/components/landing/marketing-landing.tsx`. Section order is the conversion funnel: Hero → LogoBar (compatibility credibility) → ProblemStatement (4 pain cards) → PersonaCards (small-shop / factory / enterprise self-select, mapped to the 3 pricing tiers) → FeatureGrid (6 capabilities) → TrustBar (8 certification badges) → Pricing (Tulana v0.1 tiers) → CtaSection (lead-capture form, mailto-fallback until /api/demo-request lands) → Footer.

**Why `app/page.tsx` redirects to `/landing` instead of `/dashboard`**: cold visitors should hit marketing first; the auth-gated dashboard is one click in via "Iniciar sesión" on the nav. Mirrors the CEQ pattern (`ceq/apps/studio/src/app/landing/page.tsx`).

**CTA today**: every "Solicitar demo" button anchors to `#demo`. The CtaSection form is currently mailto-based (`ventas@madfam.io`) since the `/api/demo-request` server endpoint isn't wired yet — when the backend ships, swap the handler in `cta-section.tsx`. Form UI doesn't change.

**Tests**: `apps/pravara-ui/components/landing/__tests__/landing-contract.test.tsx` enforces:
- TrustBar: 8 badges in canonical order (ISO 9001, ISO 13849-1, ISO 27001, GDPR, NOM-151, MQTT 5.0, Multi-tenant RLS, 99.9% SLA)
- Pricing: 3 Tulana tiers with documented MXN prices ($4,999 / $14,999 / $49,999)
- Hero: "Solicitar demo" CTA points at `#demo`
- LogoBar: compatibility list matches the runtime protocol surface

Adding a tier, badge, or compat entry requires updating this test in the same PR (mirrors the karafiel#46 contract pattern). Keeps marketing copy from drifting out of sync with what the product actually does.

**Brand tone**: Spanish (es-MX) by default, relief-focused not feature-first. Pain → relief, not "leverage synergies." Same voice as Karafiel's landing.

<!-- END LEGACY_CLAUDE_IMPORT -->
