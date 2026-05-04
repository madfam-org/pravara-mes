import { Plug, Activity, ShieldCheck } from "lucide-react";

// "How does this actually work?" — the missing third leg between
// PersonaCards (who is this for) and FeatureGrid (what does it do). Three
// steps deliberately: more than that and visitors stop reading; fewer
// and the journey doesn't feel like a journey.
//
// The connector line on desktop is a CSS-only horizontal rule behind the
// step circles. On mobile we hide it (vertical layout makes it more
// noise than signal).

const steps = [
  {
    n: "01",
    icon: Plug,
    title: "Conecta",
    body: "Adaptador universal MQTT/OPC-UA. Cualquier máquina con un puerto Ethernet o serial habla con Pravara en minutos.",
  },
  {
    n: "02",
    icon: Activity,
    title: "Captura",
    body: "Cada evento — start, stop, error, alarma — entra al pipeline. Sin polling. Sin pérdidas.",
  },
  {
    n: "03",
    icon: ShieldCheck,
    title: "Audita",
    body: "Cada pieza producida queda con un trail completo. COA/COC se generan al cierre del lote.",
  },
];

export function HowItWorks() {
  return (
    <section
      id="how-it-works"
      className="border-b border-border/40 bg-background py-20"
    >
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <div className="mx-auto mb-14 max-w-3xl text-center">
          <p className="mb-3 text-xs font-semibold uppercase tracking-widest text-primary">
            Cómo funciona
          </p>
          <h2 className="text-3xl font-bold tracking-tight sm:text-4xl">
            Tres pasos. No tres meses de implementación.
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            La diferencia entre un MES tradicional y Pravara es que aquí
            el integrador no tiene que escribir código por cada máquina.
          </p>
        </div>

        <ol
          className="relative grid gap-6 lg:grid-cols-3"
          aria-label="Tres pasos para conectar tu fábrica a Pravara"
        >
          {/* Connector line — sits behind the cards on lg+ screens.
              Pure CSS, no JS, no SVG dance. */}
          <span
            aria-hidden
            className="pointer-events-none absolute left-[16.667%] right-[16.667%] top-[2.75rem] hidden h-px bg-gradient-to-r from-transparent via-border to-transparent lg:block"
          />

          {steps.map(({ n, icon: Icon, title, body }) => (
            <li
              key={n}
              className="relative rounded-2xl border border-border bg-card p-7"
            >
              <div className="mb-5 flex items-center gap-3">
                <span className="flex h-11 w-11 items-center justify-center rounded-full bg-primary/10 text-primary ring-4 ring-card">
                  <Icon className="h-5 w-5" />
                </span>
                <span className="font-mono text-xs uppercase tracking-widest text-muted-foreground">
                  Paso {n}
                </span>
              </div>
              <h3 className="mb-2 text-xl font-semibold">{title}</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {body}
              </p>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}
