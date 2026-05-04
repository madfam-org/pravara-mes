import Link from "next/link";
import { Cpu } from "lucide-react";

// Minimal footer. The point of /landing isn't to be a corporate
// website — it's to convert. Footer carries legal-required nav
// (Privacy, Terms) and ecosystem cross-links so visitors can verify
// MADFAM is a real, multi-product company. Anything else lives on
// the actual app or sits in the in-page CTA above.

export function LandingFooter() {
  return (
    <footer className="border-t border-border/40 bg-background py-12">
      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        <div className="flex flex-col items-start gap-8 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <Link
              href="/landing"
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

        <div className="mt-10 border-t border-border/40 pt-6 text-xs text-muted-foreground">
          © {new Date().getFullYear()} Innovaciones MADFAM S.A.S. de
          C.V. · Cuernavaca, Morelos, México · Todos los derechos
          reservados.
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
