"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { ArrowRight } from "lucide-react";

// Thin always-visible CTA that slides in once the visitor leaves the
// hero and disappears when the demo CTA section enters the viewport.
// Two IntersectionObservers — one watching the hero's sentinel, one
// watching the #demo section. We only show the bar when the visitor
// is in the "no CTA visible" middle band; that's the only window where
// it's actually useful.

export function StickyCta() {
  const [show, setShow] = useState(false);

  useEffect(() => {
    if (typeof window === "undefined") return;

    const sentinel = document.getElementById("hero-sentinel");
    const demo = document.getElementById("demo");
    if (!sentinel) return;

    let pastHero = false;
    let demoVisible = false;
    const apply = () => setShow(pastHero && !demoVisible);

    const heroObserver = new IntersectionObserver(
      ([entry]) => {
        // sentinel is INSIDE the hero; once it's no longer intersecting
        // we know we've scrolled past the hero.
        pastHero = !entry.isIntersecting;
        apply();
      },
      { threshold: 0 },
    );
    heroObserver.observe(sentinel);

    let demoObserver: IntersectionObserver | null = null;
    if (demo) {
      demoObserver = new IntersectionObserver(
        ([entry]) => {
          demoVisible = entry.isIntersecting;
          apply();
        },
        { threshold: 0.05 },
      );
      demoObserver.observe(demo);
    }

    return () => {
      heroObserver.disconnect();
      demoObserver?.disconnect();
    };
  }, []);

  return (
    <div
      data-testid="sticky-cta"
      className={`fixed inset-x-0 top-0 z-50 transition-transform duration-300 ${
        show ? "translate-y-0" : "-translate-y-full"
      }`}
      aria-hidden={!show}
    >
      <div className="border-b border-border/60 bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80">
        <div className="mx-auto flex h-12 max-w-7xl items-center justify-between gap-4 px-4 text-sm sm:px-6 lg:px-8">
          <span className="truncate text-muted-foreground">
            <span className="font-semibold text-foreground">Pravara MES</span>
            <span className="hidden sm:inline">
              {" "}
              · Sistema de ejecución de manufactura
            </span>
          </span>
          <Link
            href="#demo"
            className="group inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground transition-opacity hover:opacity-90"
            tabIndex={show ? 0 : -1}
          >
            Solicitar demo
            <ArrowRight className="h-3.5 w-3.5 transition-transform group-hover:translate-x-0.5" />
          </Link>
        </div>
      </div>
    </div>
  );
}
