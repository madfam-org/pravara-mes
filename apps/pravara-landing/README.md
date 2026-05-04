# @pravara-mes/landing

Public marketing surface for **mes.madfam.io**. Standalone Next.js 15 + React 19 + Tailwind 3 service. No auth gate, no backend integration, no secrets.

## Why this lives outside `pravara-ui`

The dashboard app (`@pravara-mes/ui`, port 4501) drags in the full operator
stack: React Query, Zustand, `@dnd-kit`, `@react-three`, Centrifugo client,
PostHog, Janua SSO, Recharts, and a handful of Radix primitives. That is the
right shape for an authenticated factory-floor console. It is the wrong
shape for a 5-second cold-visitor landing where bundle size and first paint
decide whether the visitor sticks around.

`pravara-landing` is the lean, public-only counterpart:

- ~10 runtime deps (vs ~40 in `pravara-ui`)
- No private `@janua/*` registry token needed at build time
- Server renders to static HTML; client only hydrates a few interactive bits
  (animated KPI counter, sticky CTA bar, scroll reveals, active-section nav)
- Lives at `mes.madfam.io/` (root) — `pravara-ui` keeps owning `/login`,
  `/dashboard`, `/workorders`, etc.

The split is one-way: copy components from `apps/pravara-ui/components/landing/`
into `apps/pravara-landing/components/landing/`, beautify, ship. The dashboard
app's `/landing` route stays in place during the transition.

## Run locally

```bash
npm install
npm run dev   # → http://localhost:4502
```

## Build

```bash
npm run build   # standalone Next output
npm start       # serve the built bundle on :4502
```

## Tests

```bash
npm run test:run
```

Contract tests (`components/landing/__tests__/landing-contract.test.tsx`)
mirror the assertions from `apps/pravara-ui` — TrustBar badge order, Pricing
tier prices, Hero CTA target, LogoBar protocol list — plus the new sections
introduced here (HowItWorks 3 steps, StickyCta default-hidden).

## Section order

`Hero → LogoBar → ProblemStatement → PersonaCards → HowItWorks → FeatureGrid → TrustBar → Pricing → CtaSection → Footer`

`StickyCta` floats above all of these and only appears between leaving the
hero and entering the CTA section.

## What this service does NOT do

- No database, no API calls, no fetch at request time.
- The demo-request form falls back to `mailto:ventas@madfam.io` until
  `/api/demo-request` ships in `pravara-ui` (or wherever it lands).
- No auth. The "Iniciar sesión" link points at `https://mes.madfam.io/login`
  which is served by `pravara-ui`, not this app.
