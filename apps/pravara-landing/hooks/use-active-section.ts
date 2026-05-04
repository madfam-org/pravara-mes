"use client";

import { useEffect, useState } from "react";

// Tracks which in-page section is currently in the visitor's viewport
// so the nav can underline the matching link. Picks the section closest
// to the top of the viewport that's still intersecting — gives a stable
// answer when multiple sections are partially visible (typical on tall
// monitors).

export function useActiveSection(sectionIds: string[]): string | null {
  const [active, setActive] = useState<string | null>(null);

  useEffect(() => {
    if (typeof window === "undefined") return;

    const elements = sectionIds
      .map((id) => document.getElementById(id))
      .filter((el): el is HTMLElement => el !== null);

    if (elements.length === 0) return;

    // rootMargin pulls the trigger band into the upper third of the
    // viewport so the underline updates as the visitor scrolls *into* a
    // section, not when it's already half off-screen.
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort(
            (a, b) =>
              a.boundingClientRect.top - b.boundingClientRect.top,
          );
        if (visible.length > 0) {
          setActive(visible[0].target.id);
        }
      },
      { rootMargin: "-20% 0px -60% 0px", threshold: 0 },
    );

    elements.forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, [sectionIds]);

  return active;
}
