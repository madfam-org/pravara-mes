// Marketing-only layout. Deliberately bypasses the (protected) auth
// gate and the dashboard sidebar — visitors hit a public marketing
// surface, not a login redirect. The "Iniciar sesión" + "Solicitar
// demo" CTAs in the page itself route into auth or a demo form.
//
// Why a separate layout (not just a page under the root layout):
// - Cloudflare cache-key is per-route — keeping marketing on /landing
//   means /dashboard cache keys can't bleed in (same trick CEQ uses
//   in apps/studio/src/app/landing/).
// - Keeps the marketing bundle independent of dashboard JS so the
//   first paint is small (Hero + a CSS animation, no React Query, no
//   Zustand stores, no @dnd-kit/core, no factory-floor 3D engine).

import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Pravara MES — Sistema de ejecución de manufactura, unificado",
  description:
    "Una sola consola para todas tus máquinas: 3D printers, CNC, láser, plotter. Telemetría en tiempo real, trazabilidad ISO-9001, mantenimiento predictivo. Para talleres y plantas que necesitan visibilidad y control sin un ejército de integradores.",
  openGraph: {
    title: "Pravara MES — La fábrica, en una pantalla",
    description:
      "Conecta cada máquina, captura cada evento, audita cada pieza. Sin agentes propietarios, sin integraciones a la medida.",
    type: "website",
    url: "https://mes.madfam.io/landing",
  },
  robots: { index: true, follow: true },
};

export default function LandingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="min-h-screen bg-background text-foreground antialiased">
      {children}
    </div>
  );
}
