import {
  Plug,
  Activity,
  Boxes,
  ScanLine,
  BrainCircuit,
  ScrollText,
} from "lucide-react";

// What the platform actually does. Six features, two columns. Each
// card pairs a verb-led title with a single sentence on the
// concrete benefit — no marketing fluff, no "leverage synergies."
//
// Six is intentional: enough to feel "real product, not a wrapper",
// few enough that visitors can scan all of them in 20 seconds. The
// order moves from infrastructure-y (connectivity, telemetry) to
// outcome-y (compliance, ML) — the buyer's mental model usually
// goes "can it talk to my stuff?" → "what do I get?"

const features = [
  {
    icon: Plug,
    title: "Conectividad universal",
    body: "GRBL, Marlin, Klipper, OctoPrint, Ruida, LinuxCNC. MQTT 5.0 y OPC-UA nativos. Si tu máquina habla, Pravara la escucha.",
  },
  {
    icon: Activity,
    title: "Telemetría en tiempo real",
    body: "WebSocket vivo a Centrifugo. Estado, progreso, temperatura, vibración, consumo — todo en un stream auditado.",
  },
  {
    icon: Boxes,
    title: "Kanban con agentes",
    body: "Órdenes de trabajo asignables, drag-and-drop, con human-in-the-loop. Los operadores ven qué sigue, no buscan papelitos.",
  },
  {
    icon: ScanLine,
    title: "Trazabilidad por pieza",
    body: "Cada lote conecta máquina + operador + receta + materia prima. COC/COA generados automáticos cuando los necesitas.",
  },
  {
    icon: BrainCircuit,
    title: "ML para mantenimiento + calidad",
    body: "Modelos de predicción de falla, detección de anomalía y predicción de calidad — orquestados, no agregados a la fuerza.",
  },
  {
    icon: ScrollText,
    title: "Bitácora inmutable lista para auditoría",
    body: "Cada evento firmado y sellado. Listo para ISO 9001, ISO 13849-1 y trazabilidad NOM-151 sin reconstruir nada.",
  },
];

export function FeatureGrid() {
  return (
    <section
      id="features"
      className="border-b border-border/40 bg-background py-20"
    >
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <div className="mx-auto mb-14 max-w-3xl text-center">
          <p className="mb-3 text-xs font-semibold uppercase tracking-widest text-primary">
            Capacidades
          </p>
          <h2 className="text-3xl font-bold tracking-tight sm:text-4xl">
            Lo que necesita una operación conectada — sin armarlo a mano.
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Seis pilares construidos para trabajar juntos desde el día uno.
            No es un dashboard sobre tu ERP; es la fábrica misma, hablando.
          </p>
        </div>

        <div className="grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2 lg:grid-cols-3">
          {features.map(({ icon: Icon, title, body }) => (
            <div
              key={title}
              className="group relative bg-card p-6 transition-colors hover:bg-card/80"
            >
              <div className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <Icon className="h-5 w-5" />
              </div>
              <h3 className="mb-2 text-base font-semibold">{title}</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {body}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
