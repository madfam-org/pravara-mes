// Logo strip — credibility ladder for visitors who don't recognize the
// product. Lists the machine vendors / firmware our universal adapter
// speaks to, framed as "compatible con tu pila actual" not "we partner
// with these brands". Avoids implying endorsement.

const compatibilities = [
  "GRBL",
  "Marlin",
  "Klipper",
  "OctoPrint",
  "Ruida",
  "LinuxCNC",
  "MQTT 5.0",
  "OPC-UA",
];

export function LogoBar() {
  return (
    <section className="border-b border-border/40 bg-card/30 py-10">
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <p className="mb-6 text-center text-xs uppercase tracking-widest text-muted-foreground">
          Compatible con tu pila actual — sin agentes propietarios
        </p>
        <div className="flex flex-wrap items-center justify-center gap-x-8 gap-y-3">
          {compatibilities.map((c) => (
            <span
              key={c}
              className="font-mono text-sm font-medium text-muted-foreground/80 transition-colors hover:text-foreground"
            >
              {c}
            </span>
          ))}
        </div>
      </div>
    </section>
  );
}
