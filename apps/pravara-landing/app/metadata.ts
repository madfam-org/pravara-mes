import type { Metadata, Viewport } from "next";

// Metadata + viewport split into a standalone module so the contract
// tests in components/landing/__tests__/seo.test.tsx can import these
// values without dragging in `geist/font` (Next-only) or globals.css
// (Tailwind directives that vitest's loader doesn't understand).

export const SITE_URL = "https://mes.madfam.io";

export const siteMetadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: {
    default: "Pravara MES — Sistema de ejecución de manufactura, unificado",
    template: "%s · Pravara MES",
  },
  description:
    "Una sola consola para todas tus máquinas: 3D printers, CNC, láser, plotter. Telemetría en tiempo real, trazabilidad ISO 9001 y NOM-151, mantenimiento predictivo. Sin agentes propietarios, sin integraciones a la medida.",
  keywords: [
    "MES",
    "Manufacturing Execution System",
    "manufactura",
    "telemetría industrial",
    "OPC-UA",
    "MQTT",
    "ISO 9001",
    "NOM-151",
    "trazabilidad",
    "OEE",
    "MADFAM",
    "México",
  ],
  authors: [{ name: "Innovaciones MADFAM" }],
  creator: "Innovaciones MADFAM",
  publisher: "Innovaciones MADFAM S.A.S. de C.V.",
  alternates: {
    canonical: SITE_URL,
  },
  openGraph: {
    type: "website",
    locale: "es_MX",
    url: SITE_URL,
    siteName: "Pravara MES",
    title: "Pravara MES — La fábrica, en una pantalla",
    description:
      "Conecta cada máquina, captura cada evento, audita cada pieza. MES nativo en la nube para talleres y fábricas en México.",
    images: [
      {
        url: "/og-image.svg",
        width: 1200,
        height: 630,
        alt: "Pravara MES — Sistema de ejecución de manufactura",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "Pravara MES — La fábrica, en una pantalla",
    description:
      "Conecta cada máquina, captura cada evento, audita cada pieza. MES nativo en la nube.",
    images: ["/og-image.svg"],
  },
  robots: {
    index: true,
    follow: true,
    googleBot: {
      index: true,
      follow: true,
      "max-image-preview": "large",
      "max-snippet": -1,
    },
  },
  icons: {
    icon: "/favicon.ico",
  },
};

export const siteViewport: Viewport = {
  themeColor: "#0a0f1a",
  width: "device-width",
  initialScale: 1,
};
