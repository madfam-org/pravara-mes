import Link from "next/link";
import { Cpu } from "lucide-react";

// Minimal footer. Carries legal-required nav (Privacy, Terms),
// ecosystem cross-links, and a "compatible con tu pila actual" line
// duplicating the LogoBar — long-tail SEO benefits from the protocol
// list living in a footer crawlers index alongside the boilerplate.

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

export function LandingFooter() {
  return (
    <footer className="border-t border-border/40 bg-background py-12">
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <div className="flex flex-col items-start gap-8 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <Link
              href="/"
              className="flex items-center gap-2 text-sm font-semibold tracking-tight"
            >
              <span className="flex h-7 w-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
                <Cpu className="h-4 w-4" />
              </span>
              <span>Pravara MES</span>
            </Link>
            <p className="mt-3 max-w-md text-xs leading-relaxed text-muted-foreground">
              Sistema de ejecución de manufactura nativo en la nube,
              construido por Innovaciones MADFAM. Hecho en México;
              alojado en infraestructura europea certificada
              ISO&nbsp;27001 + GDPR.
            </p>
          </div>

          <nav className="flex flex-wrap gap-x-8 gap-y-3 text-sm text-muted-foreground">
            <FooterLinkGroup
              title="Producto"
              links={[
                { label: "Capacidades", href: "#features" },
                { label: "Precios", href: "#pricing" },
                { label: "Solicitar demo", href: "#demo" },
              ]}
            />
            <FooterLinkGroup
              title="Ecosistema MADFAM"
              links={[
                { label: "Karafiel · Compliance fiscal", href: "https://karafiel.mx" },
                { label: "Tezca · Inteligencia legal", href: "https://tezca.mx" },
                { label: "Dhanam · Finanzas", href: "https://dhan.am" },
              ]}
            />
            <FooterLinkGroup
              title="Legal"
              links={[
                { label: "Privacidad", href: "/legal/privacy" },
                { label: "Términos", href: "/legal/terms" },
                { label: "Contacto", href: "mailto:hola@madfam.io" },
              ]}
            />
          </nav>
        </div>

        <div className="mt-10 flex flex-col gap-2 border-t border-border/40 pt-6 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
          <span>
            © {new Date().getFullYear()} Innovaciones MADFAM S.A.S. de
            C.V. · Cuernavaca, Morelos, México · Todos los derechos
            reservados.
          </span>
          <span className="font-mono text-[11px] text-muted-foreground/80">
            Compatible con tu pila actual: {compatibilities.join(" · ")}
          </span>
        </div>
      </div>
    </footer>
  );
}

function FooterLinkGroup({
  title,
  links,
}: {
  title: string;
  links: { label: string; href: string }[];
}) {
  return (
    <div>
      <p className="mb-3 text-xs font-semibold uppercase tracking-wider text-foreground">
        {title}
      </p>
      <ul className="space-y-2">
        {links.map(({ label, href }) => (
          <li key={label}>
            <Link
              href={href}
              className="transition-colors hover:text-foreground"
            >
              {label}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
