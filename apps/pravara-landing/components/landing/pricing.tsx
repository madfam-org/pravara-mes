import Link from "next/link";
import { Check, Sparkles, X } from "lucide-react";

// Pricing.
//
// Tiers + numbers come from Tulana v0.1 recommendations
// (internal-devops/decisions/2026-04-25-tulana-ecosystem-pricing.md).
// Confidence is currently low — prices are anchored on competitor band,
// not yet validated against real customer WTP. Each tier shows MXN/mes
// because Mexico is the primary launch market.
//
// CTA pattern: every tier links to #demo because the product is not yet
// self-serve — sales-assisted onboarding gates conversion today. Growth
// is highlighted as "Más popular" with a gradient ring so the buyer
// anchors there instead of underbuying or sticker-shocking on Enterprise.
//
// The "what's NOT in this tier" footer row is deliberate: it makes the
// upsell ladder legible without adding a separate comparison table.
// Visitors with a real budget pick the tier that includes what they
// actually need; the exclusion line is the gentlest way to signal that.

const tiers = [
  {
    name: "Starter",
    price: "$4,999",
    period: "MXN / mes",
    cap: "Hasta 10 máquinas",
    blurb:
      "Para talleres y maker shops que quieren ver toda su operación en una pantalla.",
    features: [
      "Conectividad universal (GRBL, Marlin, OctoPrint, Klipper)",
      "Kanban de órdenes con drag-and-drop",
      "Telemetría en tiempo real",
      "Trazabilidad básica por orden",
      "Hasta 3 operadores",
      "Soporte por email · respuesta en 24h",
    ],
    excludes: "Sin ML, sin COC/COA automáticos, sin SSO corporativo.",
    cta: "Solicitar demo",
  },
  {
    name: "Growth",
    price: "$14,999",
    period: "MXN / mes",
    cap: "Hasta 50 máquinas",
    blurb:
      "Para fábricas con múltiples turnos que necesitan visibilidad cruzada y mantenimiento predictivo.",
    features: [
      "Todo lo de Starter, más:",
      "Trazabilidad por lote con COC/COA automáticos",
      "Mantenimiento predictivo (ML básico)",
      "Detección de anomalías en tiempo real",
      "Integraciones ERP (REST + webhooks)",
      "Operadores ilimitados",
      "Soporte prioritario · SLA de 4h en horario laboral",
    ],
    excludes: "Sin multi-sitio RLS, sin SLA 24/7, sin éxito dedicado.",
    cta: "Solicitar demo",
    highlight: true,
  },
  {
    name: "Enterprise",
    price: "$49,999",
    period: "MXN / mes",
    cap: "Multi-sitio · máquinas ilimitadas",
    blurb:
      "Para operaciones distribuidas que necesitan compliance ISO/NOM y un centro de mando con nivel de auditoría.",
    features: [
      "Todo lo de Growth, más:",
      "Multi-tenant con aislamiento por sitio (RLS)",
      "Bitácora inmutable lista para ISO 9001 / NOM-151",
      "ML orquestado: predicción de calidad + OEE",
      "SSO con Janua + integración a tu IdP corporativo",
      "Ingeniero de éxito dedicado",
      "SLA 24/7 · 99.9% uptime garantizado",
    ],
    excludes: "Todo incluido. Lo único que no traemos: tus máquinas.",
    cta: "Hablar con ventas",
  },
];

export function Pricing() {
  return (
    <section
      id="pricing"
      className="border-b border-border/40 bg-background py-20"
    >
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <div className="mx-auto mb-14 max-w-3xl text-center">
          <p className="mb-3 text-xs font-semibold uppercase tracking-widest text-primary">
            Precios
          </p>
          <h2 className="text-3xl font-bold tracking-tight sm:text-4xl">
            Misma plataforma. Precio anclado al tamaño de tu operación.
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Sin sorpresas. Sin licencias por máquina al estilo SAP.
            Empieza con un par de máquinas y crece sin migrar de
            sistema.
          </p>
        </div>

        <div className="grid gap-6 lg:grid-cols-3">
          {tiers.map(
            ({
              name,
              price,
              period,
              cap,
              blurb,
              features,
              excludes,
              cta,
              highlight,
            }) => (
              <div
                key={name}
                className={`relative flex flex-col rounded-2xl border p-7 transition-all ${
                  highlight
                    ? "border-primary bg-card shadow-xl shadow-primary/15 ring-2 ring-primary/40"
                    : "border-border bg-card hover:border-border/80"
                }`}
              >
                {highlight && (
                  <>
                    {/* Soft gradient halo so the recommended tier reads
                        as "this is the answer most buyers want" without
                        screaming. Sits behind the card via -z-10. */}
                    <span
                      aria-hidden
                      className="pointer-events-none absolute -inset-px -z-10 rounded-2xl bg-gradient-to-br from-primary/30 via-primary/10 to-transparent blur-xl"
                    />
                    <span className="absolute -top-3 left-1/2 inline-flex -translate-x-1/2 items-center gap-1 rounded-full bg-primary px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-primary-foreground shadow-lg shadow-primary/40">
                      <Sparkles className="h-3 w-3" />
                      Más popular
                    </span>
                  </>
                )}

                <div className="mb-1 flex items-baseline justify-between">
                  <h3 className="text-xl font-semibold">{name}</h3>
                </div>

                <p className="mb-5 text-xs uppercase tracking-wide text-muted-foreground">
                  {cap}
                </p>

                <div className="mb-1 flex items-baseline gap-2">
                  <span className="text-4xl font-bold tracking-tight">
                    {price}
                  </span>
                  <span className="text-sm font-medium text-foreground/80">
                    {period}
                  </span>
                </div>
                <p className="mb-5 text-[11px] uppercase tracking-wide text-muted-foreground/80">
                  Sin IVA · Cancela cuando quieras
                </p>

                <p className="mb-6 text-sm text-muted-foreground">{blurb}</p>

                <ul className="mb-6 flex-1 space-y-3">
                  {features.map((f) => (
                    <li
                      key={f}
                      className="flex items-start gap-2.5 text-sm text-foreground/90"
                    >
                      <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                      <span className="leading-relaxed">{f}</span>
                    </li>
                  ))}
                </ul>

                <p className="mb-6 flex items-start gap-2 rounded-lg border border-border/60 bg-background/50 p-3 text-xs leading-relaxed text-muted-foreground">
                  <X className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground/80" />
                  <span>{excludes}</span>
                </p>

                <Link
                  href="#demo"
                  className={`inline-flex h-11 items-center justify-center rounded-lg px-6 text-sm font-medium transition-all ${
                    highlight
                      ? "bg-primary text-primary-foreground shadow-lg shadow-primary/20 hover:opacity-90"
                      : "border border-border bg-card text-foreground hover:bg-accent"
                  }`}
                >
                  {cta}
                </Link>
              </div>
            ),
          )}
        </div>

        <p className="mt-10 text-center text-xs text-muted-foreground">
          Precios en MXN sin IVA. Onboarding gratuito incluido en todos los planes.
          Cancela en cualquier momento — sin contratos anuales obligatorios.
        </p>
      </div>
    </section>
  );
}
