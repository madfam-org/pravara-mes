"use client";

import Link from "next/link";
import {
  ArrowRight,
  Cpu,
  Activity,
  CheckCircle2,
  Wrench,
} from "lucide-react";

// Hero section.
//
// Conversion math: visitors decide in ~5 seconds whether to keep
// scrolling. The H1 has to communicate (a) what we are (an MES), (b)
// who it's for (manufacturing/fabrication operators), and (c) why
// they should care (one console for everything). The kicker line
// addresses the specific anxiety: "I just bought a new laser, am I
// going to learn its UI from scratch?" — no, Pravara abstracts it.
//
// Two CTAs: primary is "Solicitar demo" (lead-capture, since the
// product backend is gated — see SECRETS_REQUIRED in the README).
// Secondary is "Ver capacidades" anchor that scrolls to the feature
// grid for visitors who need more before committing to a form.
//
// The mockup at the bottom is a CSS/Tailwind composition — no real
// React Three Fiber to keep the bundle small. It hints at the
// product (Kanban + machine telemetry) without pretending to be the
// app.

export function Hero() {
  return (
    <section className="relative overflow-hidden border-b border-border/50 bg-background pt-12 pb-20 sm:pt-16 sm:pb-24">
      {/* Subtle radial gradient — gives the hero some depth without a
          background image. Inherits primary tint from the theme. */}
      <div
        aria-hidden
        className="absolute inset-0 -z-10 bg-[radial-gradient(ellipse_at_top,_hsl(var(--primary)/0.08),_transparent_60%)]"
      />

      <div className="mx-auto max-w-6xl px-4 sm:px-6 lg:px-8">
        {/* Eyebrow */}
        <div className="mx-auto mb-6 flex w-fit items-center gap-2 rounded-full border border-border bg-card px-3 py-1 text-xs text-muted-foreground">
          <span className="relative flex h-2 w-2">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-500 opacity-75" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
          </span>
          Manufactura conectada · MES nativo en la nube
        </div>

        {/* H1 */}
        <h1 className="mx-auto max-w-4xl text-center text-4xl font-bold leading-tight tracking-tight sm:text-5xl md:text-6xl">
          Toda tu fábrica,{" "}
          <span className="bg-gradient-to-r from-primary to-primary/60 bg-clip-text text-transparent">
            en una sola pantalla
          </span>
        </h1>

        {/* Sub */}
        <p className="mx-auto mt-6 max-w-2xl text-center text-lg leading-relaxed text-muted-foreground sm:text-xl">
          Conecta cada máquina —impresora 3D, CNC, láser, plotter—,
          captura cada evento y audita cada pieza producida. Sin
          agentes propietarios, sin integraciones a la medida, sin
          cinco interfaces que tu equipo tiene que aprender de
          memoria.
        </p>

        {/* CTAs */}
        <div className="mt-10 flex flex-col items-center justify-center gap-3 sm:flex-row">
          <Link
            href="#demo"
            className="group inline-flex h-12 items-center justify-center gap-2 rounded-lg bg-primary px-6 text-base font-medium text-primary-foreground shadow-lg shadow-primary/20 transition-all hover:opacity-90 hover:shadow-primary/30"
          >
            Solicitar demo
            <ArrowRight className="h-4 w-4 transition-transform group-hover:translate-x-0.5" />
          </Link>
          <Link
            href="#features"
            className="inline-flex h-12 items-center justify-center rounded-lg border border-border bg-card px-6 text-base font-medium text-foreground transition-colors hover:bg-accent"
          >
            Ver capacidades
          </Link>
        </div>

        {/* Reassurance line */}
        <p className="mt-4 text-center text-xs text-muted-foreground">
          Demo de 30 min con un ingeniero · Sin tarjeta de crédito · Compatible con tu pila actual
        </p>

        {/* Mockup */}
        <FactoryFloorMockup />
      </div>
    </section>
  );
}

/**
 * Visual hook: a stylized command-center mockup hinting at the
 * Kanban + machine telemetry that the real product surfaces.
 *
 * Deliberately CSS-only (no images, no 3D engine) so it's themeable,
 * accessible, and adds zero KB to the bundle beyond Tailwind utility
 * classes the rest of the page already pulls in.
 */
function FactoryFloorMockup() {
  const machines = [
    {
      label: "CNC-01 · Brother M200",
      state: "Producción",
      tone: "emerald",
      progress: 72,
    },
    {
      label: "LASER-02 · Trotec Speedy",
      state: "En cola",
      tone: "amber",
      progress: 0,
    },
    {
      label: "3DP-03 · Prusa XL",
      state: "Calibración",
      tone: "sky",
      progress: 18,
    },
    {
      label: "CNC-04 · Haas VF-2",
      state: "Mantenimiento",
      tone: "rose",
      progress: 0,
    },
  ];

  const kpis = [
    { label: "Disponibilidad", value: "94.2%", icon: Activity },
    { label: "OEE 24h", value: "78%", icon: Cpu },
    { label: "Piezas OK", value: "1,284", icon: CheckCircle2 },
    { label: "MTBF", value: "162h", icon: Wrench },
  ];

  return (
    <div
      className="relative mx-auto mt-16 w-full max-w-5xl"
      style={{ perspective: "1400px" }}
    >
      <div
        className="rounded-xl border border-border bg-card p-4 shadow-2xl shadow-primary/5 sm:p-6"
        style={{ transform: "rotateX(2deg)" }}
      >
        {/* Browser chrome */}
        <div className="mb-4 flex items-center gap-2 border-b border-border/60 pb-3">
          <span className="h-2.5 w-2.5 rounded-full bg-rose-500/60" />
          <span className="h-2.5 w-2.5 rounded-full bg-amber-500/60" />
          <span className="h-2.5 w-2.5 rounded-full bg-emerald-500/60" />
          <span className="ml-3 text-xs text-muted-foreground">
            mes.madfam.io / dashboard
          </span>
        </div>

        {/* KPI strip */}
        <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
          {kpis.map(({ label, value, icon: Icon }) => (
            <div
              key={label}
              className="rounded-lg border border-border/60 bg-background p-3"
            >
              <div className="mb-1.5 flex items-center gap-2 text-muted-foreground">
                <Icon className="h-3.5 w-3.5" />
                <span className="text-[11px] uppercase tracking-wide">
                  {label}
                </span>
              </div>
              <p className="text-xl font-bold tracking-tight sm:text-2xl">
                {value}
              </p>
            </div>
          ))}
        </div>

        {/* Machines list — mimics the real factory floor view */}
        <div className="space-y-2">
          {machines.map(({ label, state, tone, progress }) => (
            <div
              key={label}
              className="flex items-center gap-4 rounded-lg border border-border/40 bg-background/60 p-3"
            >
              <div className="flex-1">
                <div className="flex items-center gap-3">
                  <span className="font-mono text-xs text-foreground">
                    {label}
                  </span>
                  <StatusPill tone={tone}>{state}</StatusPill>
                </div>
                <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-muted">
                  <div
                    className={progressColor(tone)}
                    style={{ width: `${progress}%` }}
                  />
                </div>
              </div>
              <span className="font-mono text-xs tabular-nums text-muted-foreground">
                {progress.toString().padStart(2, "0")}%
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function StatusPill({
  tone,
  children,
}: {
  tone: string;
  children: React.ReactNode;
}) {
  const tones: Record<string, string> = {
    emerald: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
    amber: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
    sky: "bg-sky-500/15 text-sky-700 dark:text-sky-400",
    rose: "bg-rose-500/15 text-rose-700 dark:text-rose-400",
  };
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium ${tones[tone] ?? tones.sky}`}
    >
      {children}
    </span>
  );
}

function progressColor(tone: string) {
  const colors: Record<string, string> = {
    emerald: "h-full bg-emerald-500 transition-all",
    amber: "h-full bg-amber-500 transition-all",
    sky: "h-full bg-sky-500 transition-all",
    rose: "h-full bg-rose-500 transition-all",
  };
  return colors[tone] ?? colors.sky;
}
