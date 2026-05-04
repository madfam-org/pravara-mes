"use client";

import { useEffect, useRef, useState } from "react";

// Tiny IntersectionObserver hook for scroll-reveal sections. Once an
// element crosses the visibility threshold we flip `revealed` and stop
// observing — reveals are one-shot, the user shouldn't see content
// re-fade as they scroll back up.
//
// Returns a ref + boolean so callers compose with whatever className
// strategy they prefer (we use opacity + translate, but a caller could
// swap in any animation).

export function useReveal<T extends HTMLElement = HTMLDivElement>(
  threshold = 0.2,
): { ref: React.RefObject<T | null>; revealed: boolean } {
  const ref = useRef<T | null>(null);
  const [revealed, setRevealed] = useState(false);

  useEffect(() => {
    const node = ref.current;
    if (!node) return;

    // Reduced-motion users get the final state immediately. Saves them
    // a half-second of perceived "loading" and keeps the page accessible
    // without depending on global CSS overrides.
    if (
      typeof window !== "undefined" &&
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches
    ) {
      setRevealed(true);
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            setRevealed(true);
            observer.disconnect();
            break;
          }
        }
      },
      { threshold },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [threshold]);

  return { ref, revealed };
}
