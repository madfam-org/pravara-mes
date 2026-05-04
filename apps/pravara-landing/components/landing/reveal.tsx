"use client";

import { useReveal } from "@/hooks/use-reveal";
import { cn } from "@/lib/utils";

// Section wrapper. Content is ALWAYS visible — SEO crawlers,
// screenshot tools, and reduced-motion users see the page exactly
// the same as a real visitor. The IntersectionObserver only adds a
// subtle 12px slide-up when the section enters view; if the observer
// never fires (tall sections, threshold edge cases, headless tools),
// nothing breaks because we never hide content in the first place.

export function Reveal({
  children,
  className,
  threshold = 0.1,
}: {
  children: React.ReactNode;
  className?: string;
  threshold?: number;
}) {
  const { ref, revealed } = useReveal<HTMLDivElement>(threshold);
  return (
    <div
      ref={ref}
      className={cn(
        "transition-transform duration-700 ease-out will-change-transform",
        revealed ? "translate-y-0" : "translate-y-3",
        className,
      )}
    >
      {children}
    </div>
  );
}
