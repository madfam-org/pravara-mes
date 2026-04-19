# PravaraMES — Landing Page Design

**Status**: Design spec — not yet implemented
**Scope**: `https://mes.madfam.io/` (currently redirects to `/dashboard` which is auth-gated → 100% bounce for prospects)
**Author**: Claude Code (investigation turn 2026-04-19)
**Audience**: Product + engineering + whoever picks up the implementation PR

---

## 1. Current state

`apps/pravara-ui/app/page.tsx`:

```tsx
import { redirect } from "next/navigation";

export default function Home() {
  redirect("/dashboard");
}
```

Every anonymous visitor to `mes.madfam.io` is instantly redirected to
`/dashboard` (gated by `(protected)` layout → login), so the public
domain has **no marketing surface at all**. Prospects never see what
PravaraMES *is* before being asked to log in.

Status-page probe is green (`mes.madfam.io` returns 200) but the 200
is the login page, not a landing. Conversion funnel starts from zero.

## 2. Who's coming to this page and what they want

Three primary visitor personas, inferred from PravaraMES's positioning
(docs/MACHINE_UNIVERSE.md — 50+ digital-fab machines; `claudedocs/machine-benchmarking.md`;
ecosystem docs citing 97% digital-twin completion):

| Persona | Pain they arrived with | The one action that converts them |
|---|---|---|
| **Shop-floor operator / production engineer** at a small-to-mid digital-fab shop (3D-print farm, CNC, laser, mixed) | Running 6+ machines via 6+ vendor apps; no single pane for jobs/OEE/maintenance; copy-pasting G-code; losing traceability | **Book a 15-min live demo** *or* **spin up free sandbox** |
| **Contract manufacturer operations lead** (10-100 machines, mixed vendors) | Legacy MES (Siemens Opcenter, iBase-t) is $200k+/yr; rigid; locked to one vendor | **Talk to sales** (enterprise) |
| **Ecosystem developer / integrator** (Forj, Yantra4D, Cotiza consumer) | Needs to route manufacturing jobs to a MES via API; wants contract clarity (OpenAPI, webhooks) | **Read the API docs** + **get API keys** |

These three maps to three distinct CTAs; treat them as parallel tracks,
not a single funnel.

## 3. Conversion goals (measurable)

Primary metric: **qualified-lead rate** (% of landing visitors who
complete one of the three CTAs).

| CTA | Target rate | Attribution |
|---|---|---|
| Book demo | ≥ 2.5% of landing visitors | Calendly/Cal.com embed; source=`mes.landing` |
| Free sandbox sign-up | ≥ 4% of landing visitors | `/login?intent=sandbox` → provisions a demo tenant |
| API docs / get keys | ≥ 1.5% of landing visitors | Click-through to `api.pravara.madfam.io/swagger` |

Secondary metrics (instrumented via PostHog, Enclii's existing
analytics pipe):

- Scroll depth — identify if visitors bounce before hitting pricing
- Section dwell time — which value-prop panels are actually read
- CTA-button click heatmap (all three stacked across sections)

## 4. Information architecture (scroll order)

Top → bottom. Every section answers one question a prospect asks in that
exact order on a cold-visit session.

1. **Hero** — *"What is this and why should I care?"*
2. **Proof strip** — *"Is this real, or a roadmap?"*
3. **Three tracks** — *"Which version of this is for me?"*
4. **Machine universe** — *"Will it talk to MY equipment?"*
5. **Platform diagram** — *"How does this fit into my stack?"*
6. **Transparent pricing** — *"What will it cost?"* (lead-magnet — most MES vendors hide this, using it as differentiation)
7. **Integrations** — *"Does it play with what I already run?"*
8. **Security & compliance** — *"Can I trust this with my shop floor?"*
9. **Community & docs** — *"If I get stuck, what's the escape hatch?"*
10. **Final CTA** — *"Here's the next step for each of the three tracks."*
11. Footer — links, legal, status page, changelog.

## 5. Section-by-section design

### 5.1 Hero

- **H1** (one line, <= 12 words):
  > The cloud-native MES that actually talks to your machines.
- **Subhead** (two lines):
  > Unified jobs, telemetry, OEE, and traceability for **50+ digital-fab
  > machines** — FDM, resin, CNC, laser, pen plotter. No vendor lock-in.
  > No legacy MES price tag.
- **Primary CTA button**: *Book a 15-min demo* (scrolls to Calendly)
- **Secondary CTA button** (ghost / outline): *Try the sandbox* (→ `/login?intent=sandbox`)
- **Tertiary link**: *Or read the API docs →* (→ `api.pravara.madfam.io/swagger`)
- **Hero visual**: animated factory-floor terminal UI — real screenshots
  of the dashboard's machine grid with live telemetry ticking (looping
  5-sec video, `.webm` + `.mp4` fallbacks, poster frame for LCP).
  **Not** a stock-photo factory; show the actual product.
- **Trust row below the CTAs**: brand lockups for early-access customers
  (can start as 3 placeholder logos with "Used by 3 active pilots —
  contact us for references"; real logos go in as deals close).

### 5.2 Proof strip

Horizontal band, compact. Single line of numbers a skeptic will verify:

- **50+ machines supported** → on click scrolls to Machine Universe section
- **97% digital-twin completion** → links to roadmap
- **Sub-100ms telemetry latency** → links to observability docs
- **OpenAPI 3 + 40 webhook events** → links to swagger
- **Self-hostable. Open contract.** → links to license

Anchor each number to a verifiable source — prospects who hover-check
one number and find it real will trust the rest.

### 5.3 Three tracks (the segmentation block)

Three side-by-side cards. Each card has: icon, persona label, 3-bullet
value prop, its own CTA.

| Card | Persona | Value bullets | CTA |
|---|---|---|---|
| 🛠️ **Run a shop** | Operator / production engineer | • One dashboard for all 50+ machines • Kanban → machine in 2 clicks • Free up to 3 machines | *Start free sandbox* |
| 🏭 **Scale operations** | Ops lead | • OEE across mixed vendors • Genealogy + traceability to serial # • Replace $200k legacy MES | *Book a demo* |
| ⚙️ **Build on top** | Dev / integrator | • REST + WebSocket APIs, OpenAPI 3 • 40 webhook events • SDKs (Go, TS) | *Read the API docs* |

### 5.4 Machine Universe (the killer differentiator)

This is the section that closes skeptics. Most MES vendors lock you to
one family; PravaraMES explicitly markets universal support.

- **Filter UI**: filter chips for category (FDM, resin, CNC, laser, pen,
  sheet), protocol (MQTT, Moonraker, Marlin, PrusaLink, OctoPrint, etc.),
  and adapter status (Implemented / Registry-only).
- **Grid of cards**: one card per machine. Each card shows machine image
  + name + protocol badge + "Implemented" badge. Pulled from
  `docs/MACHINE_UNIVERSE.md` (already exists — authoritative source).
- **Don't-see-yours CTA at bottom**: *"Your machine isn't here? [Request
  support →]"* — captures long-tail demand for the product roadmap.

Implementation note: render the grid server-side from the existing
MACHINE_UNIVERSE.md (parse the markdown tables at build time); keeps
the page in sync with the source of truth and lets us rank machines by
popularity without a CMS.

### 5.5 Platform diagram

Single SVG (inline, not an image tag) showing:

```
  [Your fleet of machines]
          ↕ MQTT / HTTP / WebSocket / Serial
   [Machine adapters]     ←── open-source; registry-extensible
          ↓
   [PravaraMES API + telemetry worker]
          ↓       ↕       ↓
   [Kanban UI]  [OEE]  [Traceability]   ←── the actual product
          ↕
   [Ecosystem integrations]  ←── Forj, Yantra4D, Cotiza, Dhanam, Karafiel
          ↕
   [Your ERP / accounting / customers]
```

Under the diagram: *"You keep your machines. You keep your ERP. We sit
in the middle and make them behave."*

### 5.6 Transparent pricing

Three tiers, side by side. Mirror Cal.com / Supabase / Enclii
aesthetic — tiered cards, middle one highlighted.

| Tier | Price | For |
|---|---|---|
| **Community** | Free forever | Up to 3 machines, community support, self-host or hosted |
| **Team** | $X/machine/mo | Unlimited machines, SSO, priority support, compliance reports |
| **Enterprise** | Talk to us | On-prem, custom SLAs, audit & compliance bundles, training |

Each tier's CTA: *Start sandbox* / *Start Team trial* / *Contact sales*.

Footnote: *"Pricing is based on connected machines, not seats. No charge
for operators — add as many as you need."* (Direct jab at per-seat
legacy MES pricing.)

> **Input needed from biz lead**: actual $X number for Team tier.
> Draft: $49/machine/mo matches industry floor and leaves margin.

### 5.7 Integrations

Logo wall: Forj • Yantra4D • Cotiza • Dhanam • Karafiel • Stripe •
Octoprint • Prusa Connect • Bambu • Formlabs • Klipper • Marlin • REST
API for anything else.

Secondary line: *"And anything with an HTTP or MQTT endpoint."*

### 5.8 Security & compliance

Three badges horizontally (not certifications we don't have — things
we actually do):

- **Isolated shop-floor networks** — egress allow-list per tenant, no
  direct internet access for machines
- **Audit log + bitemporal lineage** — every setpoint change, every job,
  every QC check is queryable and replayable
- **Open contract** — AGPL-3.0 self-host option; no vendor lock

Avoid compliance claims we can't substantiate on the landing — link
to `SECURITY.md` for what's actually implemented.

### 5.9 Community & docs

Three-column "if you get stuck" block:

- **Docs**: `docs.pravara.madfam.io` (swagger, guides, SDK reference)
- **GitHub**: `github.com/madfam-org/pravara-mes` (source, issues, discussions)
- **Slack/Discord**: community workspace for operators

### 5.10 Final CTA

Stack the three tracks again as a trio of buttons, centered, large,
with white space. No distraction, no fine print below — only the three
actions. This is the page's last breath — one of the three buttons is
the conversion event.

### 5.11 Footer

- Column 1: Product (features, pricing, changelog, roadmap, status)
- Column 2: Developers (API, SDKs, webhooks, GitHub)
- Column 3: Company (MADFAM parent, about, contact, jobs)
- Column 4: Legal (terms, privacy, security, AGPL license)
- Row at bottom: status-page dot (live pull from `status.madfam.io`) +
  build SHA + © year + language selector (ES/EN — Mexican market is a
  primary target per MADFAM positioning).

## 6. Technical implementation plan

**Short version**: replace `apps/pravara-ui/app/page.tsx`'s redirect
with a public marketing page. Keep all protected routes under
`(protected)/*` as they are. No separate app/repo.

### 6.1 Files to add

```
apps/pravara-ui/app/
├── page.tsx                    # NEW — public landing (replace redirect)
├── (marketing)/
│   ├── layout.tsx              # public layout (no auth provider, no sidebar)
│   ├── pricing/page.tsx        # deep-link target for pricing section
│   └── machines/page.tsx       # full Machine Universe filterable grid
└── components/marketing/
    ├── hero.tsx
    ├── proof-strip.tsx
    ├── three-tracks.tsx
    ├── machine-universe.tsx    # parses docs/MACHINE_UNIVERSE.md at build
    ├── platform-diagram.tsx    # inline SVG
    ├── pricing-tiers.tsx
    ├── integrations.tsx
    ├── security-badges.tsx
    ├── community.tsx
    ├── final-cta.tsx
    └── footer.tsx
```

### 6.2 Data source for machine grid

Parse `docs/MACHINE_UNIVERSE.md` at build time:

```ts
// apps/pravara-ui/lib/machines.ts
import fs from "node:fs/promises";
import path from "node:path";
import { remark } from "remark";
import remarkParse from "remark-parse";

export async function loadMachines() {
  const md = await fs.readFile(
    path.join(process.cwd(), "../../docs/MACHINE_UNIVERSE.md"),
    "utf8",
  );
  // parse table rows -> { category, name, protocol, registryKey, status }
}
```

Keeps the landing in sync with the source-of-truth doc without a CMS.
Site rebuild picks up any new machine automatically.

### 6.3 CTAs — where they go

| CTA | Target |
|---|---|
| *Book a demo* | `https://cal.com/madfam/pravara-demo-15min` (or Calendly; pick one) |
| *Try sandbox* | `/login?intent=sandbox&source=mes.landing` — Janua flow with `intent` metadata; API provisions a 14-day demo tenant with seeded machines |
| *Read the API docs* | `https://api.pravara.madfam.io/swagger` (existing) |
| *Talk to sales* | `mailto:hello@madfam.io?subject=PravaraMES%20Enterprise` or `/contact` |

### 6.4 Analytics

Reuse the ecosystem's PostHog proxy at `analytics.madfam.io` (per
enclii/CLAUDE.md). Events:

- `landing_viewed` (once per session, with `referrer`, `utm_*`)
- `section_viewed:<section_id>` (IntersectionObserver, once per section)
- `cta_clicked:<cta_id>:<section_id>`
- `machine_grid_filtered:<category>:<protocol>:<status>`

### 6.5 Performance budget

- LCP < 2.0s on 4G mobile — hero copy + poster frame must be SSR'd,
  video loads async
- Total page weight < 500 KB (excluding hero video)
- Zero client-side fetches before first paint — everything SSR'd from
  MACHINE_UNIVERSE.md + a static `pricing.json`
- `next/image` for all raster images; inline SVG for icons/diagram

### 6.6 SEO

- Title: `PravaraMES — The cloud-native MES for 50+ digital-fab machines`
- Meta description: written above, keep under 155 chars
- `og:image` — render a static OG card per page via `@vercel/og` or
  equivalent; hero screenshot + title
- `schema.org/SoftwareApplication` JSON-LD with offers (the pricing
  tiers), aggregateRating stub (fill when we have reviews)
- Sitemap + robots.txt (auto-generated at build)
- `alternate` hreflang for `es-MX` / `en-US`

## 7. Rollout plan

Three incremental PRs — each shippable on its own, each improves
conversion:

| PR | Scope | Value delivered |
|---|---|---|
| **1** | Replace redirect → ship skeleton (hero + three tracks + final CTA only). All other sections stubbed with `Coming soon`. | Landing exists at all; primary CTA captures leads |
| **2** | Machine Universe section + Platform diagram + Integrations | Closes the *"does it support mine"* objection |
| **3** | Pricing + Security + Community + Footer polish | Closes the *"what's the catch / can I trust this"* objection |

PR 1 alone eliminates the 100% prospect-bounce we have today.

## 8. Open questions (need product/biz input before PR 2 or 3)

1. **Team-tier price point** — $49/machine/mo is a draft; needs
   validation against target margin and competitor pricing
   (Opcenter, MachineMetrics, Tulip).
2. **Demo-tenant provisioning** — `intent=sandbox` needs backend work
   in `pravara-api` to auto-provision on signup. Separate RFC.
3. **Cal/Calendly choice** — pick one and lock the URL shape for the
   CTA analytics.
4. **Compliance claims** — what, if any, can we legitimately state
   (SOC 2 roadmap? NOM-151 via Karafiel? GDPR?). Avoid over-claiming.
5. **Logos for the trust row** — need permission from the 3 pilot
   customers; until then use "Trusted by pilot customers → request references".

## 9. What *not* to do

Anti-patterns we should explicitly avoid:

- **No "Request a demo" forms on the hero** — demo = 15-min Cal link.
  Forms bounce.
- **No chat bubble** unless we can actually staff it in CDMX hours;
  an unanswered chat kills trust faster than having none.
- **No stock-photo factories** — show the actual product UI, or nothing.
- **No "Starting from" pricing asterisks without a visible price** —
  prospects treat this the same as "Contact us", which is what the
  Enterprise tier is for.
- **No newsletter modal on scroll** — the final CTA is the final CTA.
- **No carousels** — prospects skip them; static stacked sections beat
  any carousel on conversion metrics.

---

**Next step**: confirm the three open questions with product/biz; open
PR 1 against `fix/public-landing-skeleton` branch with the replaced
`page.tsx` + hero + three tracks + final CTA.
