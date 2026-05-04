import { Wrench, Factory, Building2, Check } from "lucide-react";

// "Is this for me?" answer card. Three personas keyed to operation size.
// Maps directly to the Tulana pricing tiers (Starter / Growth / Enterprise).

const personas = [
  {
    icon: Wrench,
    label: "Maker shop / taller pequeño",
    range: "1–10 máquinas · 1–3 operadores",
    headline: "Un solo dashboard, no diez.",
    bullets: [
      "Conecta cada impresora 3D, CNC, láser bajo una pantalla.",
      "Kanban compartido para órdenes — el operador ve qué sigue, sin papelitos.",
      "Plug-and-play con GRBL/Marlin/OctoPrint que ya estás corriendo.",
    ],
    tier: "Starter",
  },
  {
    icon: Factory,
    label: "Fábrica en crecimiento",
    range: "10–50 máquinas · múltiples turnos",
    headline: "Visibilidad cruzada de turnos en tiempo real.",
    bullets: [
      "Telemetría continua — quién está produciendo qué, ahora.",
      "Trazabilidad por lote: máquina, operador, receta, materia prima.",
      "Mantenimiento predictivo basado en señales, no en calendario.",
    ],
    tier: "Growth",
    highlight: true,
  },
  {
    icon: Building2,
    label: "Multi-sitio / Enterprise",
    range: "50+ máquinas · operación distribuida",
    headline: "Centro de mando con control de auditoría.",
    bullets: [
      "Multi-tenant con aislamiento RLS — sitios y BU separados nativamente.",
      "Bitácora inmutable de eventos lista para ISO 9001 / NOM-151.",
      "ML orquestado: predicción de calidad, anomalías, optimización OEE.",
    ],
    tier: "Enterprise",
  },
];

export function PersonaCards() {
  return (
    <section className="border-b border-border/40 bg-card/30 py-20">
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <div className="mx-auto mb-14 max-w-3xl text-center">
          <p className="mb-3 text-xs font-semibold uppercase tracking-widest text-primary">
            ¿Esto es para mi operación?
          </p>
          <h2 className="text-3xl font-bold tracking-tight sm:text-4xl">
            Construido para la planta que ya tienes — no la que tendrás en cinco años.
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Misma plataforma, escalada al tamaño de tu fábrica. Empieza con un par
            de máquinas, crece sin migrar de sistema.
          </p>
        </div>

        <div className="grid gap-6 lg:grid-cols-3">
          {personas.map(({ icon: Icon, label, range, headline, bullets, tier, highlight }) => (
            <div
              key={label}
              className={`relative rounded-2xl border p-6 transition-all ${
                highlight
                  ? "border-primary/60 bg-card shadow-lg shadow-primary/10"
                  : "border-border bg-card hover:border-border/80"
              }`}
            >
              {highlight && (
                <span className="absolute -top-3 left-6 rounded-full bg-primary px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-primary-foreground">
                  Más popular
                </span>
              )}

              <div className="mb-5 flex items-start justify-between gap-4">
                <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                  <Icon className="h-5 w-5" />
                </div>
                <span className="rounded-md border border-border bg-background px-2 py-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                  {tier}
                </span>
              </div>

              <p className="text-xs uppercase tracking-wide text-muted-foreground">
                {label}
              </p>
              <p className="mb-4 mt-1 text-xs text-muted-foreground/80">
                {range}
              </p>

              <h3 className="mb-5 text-xl font-semibold leading-snug">
                {headline}
              </h3>

              <ul className="space-y-3">
                {bullets.map((b) => (
                  <li
                    key={b}
                    className="flex items-start gap-2.5 text-sm text-foreground/90"
                  >
                    <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                    <span className="leading-relaxed">{b}</span>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
