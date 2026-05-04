"use client";

import Link from "next/link";
import { Cpu, Menu } from "lucide-react";
import { useActiveSection } from "@/hooks/use-active-section";

// Sticky top nav with active-section underline (driven by
// IntersectionObserver via useActiveSection). Mobile collapses the
// secondary anchors into a CSS-only details/summary menu so we don't
// pull in any JS for a hamburger toggle.
//
// Section ids tracked here MUST match the `id` attributes on the
// matching <section> tags downstream — silent regression risk if they
// drift.

const sections = [
  { id: "features", label: "Capacidades" },
  { id: "how-it-works", label: "Cómo funciona" },
  { id: "pricing", label: "Precios" },
];

export function LandingNav() {
  const active = useActiveSection(sections.map((s) => s.id));

  return (
    <header className="sticky top-0 z-40 border-b border-border/40 bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
        <Link
          href="/"
          className="flex items-center gap-2 text-sm font-semibold tracking-tight"
        >
          <span className="flex h-7 w-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Cpu className="h-4 w-4" />
          </span>
          <span>Pravara MES</span>
        </Link>

        {/* Desktop nav */}
        <nav className="hidden items-center gap-1 sm:flex" aria-label="Secciones principales">
          {sections.map(({ id, label }) => (
            <Link
              key={id}
              href={`#${id}`}
              className="relative rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
              aria-current={active === id ? "true" : undefined}
            >
              {label}
              <span
                aria-hidden
                className={`pointer-events-none absolute inset-x-3 -bottom-px h-0.5 rounded-full bg-primary transition-all duration-300 ${
                  active === id ? "scale-x-100 opacity-100" : "scale-x-0 opacity-0"
                }`}
                style={{ transformOrigin: "center" }}
              />
            </Link>
          ))}
          <Link
            href="https://mes-app.madfam.io/login"
            className="ml-2 rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            Iniciar sesión
          </Link>
          <Link
            href="#demo"
            className="ml-1 inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90"
          >
            Solicitar demo
          </Link>
        </nav>

        {/* Mobile: single CTA + a CSS-only details menu. No JS for the
            hamburger; <details> open-state is browser-native. */}
        <div className="flex items-center gap-2 sm:hidden">
          <Link
            href="#demo"
            className="inline-flex h-9 items-center rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground transition-opacity hover:opacity-90"
          >
            Solicitar demo
          </Link>
          <details className="relative">
            <summary
              className="flex h-9 w-9 cursor-pointer list-none items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:bg-accent"
              aria-label="Menú de navegación"
            >
              <Menu className="h-4 w-4" />
            </summary>
            <div className="absolute right-0 top-11 w-52 rounded-lg border border-border bg-popover p-2 shadow-xl">
              {sections.map(({ id, label }) => (
                <Link
                  key={id}
                  href={`#${id}`}
                  className="block rounded-md px-3 py-2 text-sm text-foreground/90 hover:bg-accent"
                >
                  {label}
                </Link>
              ))}
              <Link
                href="https://mes-app.madfam.io/login"
                className="block rounded-md px-3 py-2 text-sm text-foreground/90 hover:bg-accent"
              >
                Iniciar sesión
              </Link>
            </div>
          </details>
        </div>
      </div>
    </header>
  );
}
