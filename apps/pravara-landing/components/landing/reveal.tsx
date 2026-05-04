"use client";

import { useReveal } from "@/hooks/use-reveal";
import { cn } from "@/lib/utils";

// Section-level scroll-reveal wrapper. The hook handles
// IntersectionObserver + reduced-motion; this component just maps the
// boolean state to opacity/translate classes. Defaults to a 600ms
// transition, matching the global "fade-up" keyframe.

export function Reveal({
  children,
  className,
  threshold = 0.2,
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
        "transition-all duration-700 ease-out will-change-transform",
        revealed ? "translate-y-0 opacity-100" : "translate-y-2 opacity-0",
        className,
      )}
    >
      {children}
    </div>
  );
}
