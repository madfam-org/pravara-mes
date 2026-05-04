import {
  Layers,
  EyeOff,
  ClipboardX,
  AlertTriangle,
} from "lucide-react";

// Pain articulation. Order is intentional, mirroring the typical buyer's
// awareness curve: fragmentation → invisibility → traceability →
// unplanned downtime.

const pains = [
  {
    icon: Layers,
    title: "Cada máquina, su propia interfaz",
    body: "GRBL, Marlin, OctoPrint, Ruida. Tu equipo aprende cinco UIs distintas para hacer un mismo trabajo, y cada nueva máquina suma una más.",
  },
  {
    icon: EyeOff,
    title: "No sabes qué está corriendo ahora mismo",
    body: "Cuando alguien pregunta 'qué hay en producción', te toca caminar el piso y revisar pantallas una por una. La visibilidad debería venir sola.",
  },
  {
    icon: ClipboardX,
    title: "La trazabilidad se reconstruye a mano",
    body: "Defecto detectado en QA, dos horas después. ¿Qué máquina lo hizo? ¿Quién operaba? ¿Qué receta? El historial está en una libreta, una hoja de Excel, o en la cabeza del supervisor.",
  },
  {
    icon: AlertTriangle,
    title: "El mantenimiento es siempre reactivo",
    body: "El husillo falla a media corrida. Pierdes la pieza, las horas, y la confianza del cliente. Las señales estaban ahí — sólo no las leíamos.",
  },
];

export function ProblemStatement() {
  return (
    <section className="border-b border-border/40 bg-background py-20">
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <div className="mx-auto mb-14 max-w-3xl text-center">
          <p className="mb-3 text-xs font-semibold uppercase tracking-widest text-primary">
            Esto suena familiar
          </p>
          <h2 className="text-3xl font-bold tracking-tight sm:text-4xl">
            La fábrica corre. La información, no.
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Cuatro fricciones que separan a un taller con buenas
            herramientas de una operación realmente conectada.
          </p>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          {pains.map(({ icon: Icon, title, body }) => (
            <div
              key={title}
              className="group rounded-xl border border-border bg-card p-6 transition-all hover:border-primary/40 hover:shadow-md"
            >
              <div className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary transition-colors group-hover:bg-primary/15">
                <Icon className="h-5 w-5" />
              </div>
              <h3 className="mb-2 text-lg font-semibold">{title}</h3>
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
