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
