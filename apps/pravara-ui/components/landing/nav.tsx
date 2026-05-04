"use client";

import Link from "next/link";
import { Cpu } from "lucide-react";

// Sticky top nav on the landing. Two things only:
//   - Wordmark + brand glyph (anchors back to top)
//   - Two CTAs: "Iniciar sesión" (existing customers) and "Solicitar
//     demo" (lead capture). On mobile we hide the demo button under a
//     menu — the in-flow CtaSection picks up that slack.
//
// Deliberately minimal: no "Features / Pricing / About" mega-menu.
// The whole page IS the marketing flow; in-page anchors do the work.

export function LandingNav() {
  return (
    <header className="sticky top-0 z-40 border-b border-border/40 bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
        <Link
          href="/landing"
          className="flex items-center gap-2 text-sm font-semibold tracking-tight"
        >
          <span className="flex h-7 w-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Cpu className="h-4 w-4" />
          </span>
          <span>Pravara MES</span>
        </Link>

        <nav className="flex items-center gap-2 sm:gap-3">
          <Link
            href="#pricing"
            className="hidden text-sm text-muted-foreground transition-colors hover:text-foreground sm:inline"
          >
            Precios
          </Link>
          <Link
            href="#features"
            className="hidden text-sm text-muted-foreground transition-colors hover:text-foreground sm:inline"
          >
            Capacidades
          </Link>
          <Link
            href="/login"
            className="text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            Iniciar sesión
          </Link>
          <Link
            href="#demo"
            className="inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90"
          >
            Solicitar demo
          </Link>
        </nav>
      </div>
    </header>
  );
}
