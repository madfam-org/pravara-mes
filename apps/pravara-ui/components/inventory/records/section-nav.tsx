"use client";

import { useEffect, useRef } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";

export const INVENTORY_SECTIONS = [
  { href: "/inventory", label: "Existencias" },
  { href: "/inventory/materials", label: "Materiales" },
  { href: "/inventory/receipts", label: "Entradas" },
  { href: "/inventory/counts", label: "Conteos físicos" },
  { href: "/inventory/consumption", label: "Consumo" },
  { href: "/inventory/attention", label: "Pendientes" },
] as const;

export function isSectionActive(pathname: string, href: string): boolean {
  if (href === "/inventory") return pathname === "/inventory";
  return pathname === href || pathname.startsWith(`${href}/`);
}

/**
 * Links between the inventory screens, styled like the app's tab list. Scrolls
 * sideways on its own at phone widths so the page never does.
 */
export function InventorySectionNav() {
  const pathname = usePathname() ?? "";
  const activeRef = useRef<HTMLAnchorElement>(null);
  useEffect(() => {
    // Keep the current section visible when the list scrolls sideways.
    const link = activeRef.current;
    const strip = link?.closest("nav");
    if (link && strip && strip.scrollWidth > strip.clientWidth) {
      strip.scrollLeft = link.offsetLeft - strip.clientWidth / 2 + link.clientWidth / 2;
    }
  }, [pathname]);
  return (
    <nav aria-label="Secciones de inventario" lang="es-MX" className="-mx-1 overflow-x-auto px-1">
      <ul className="inline-flex h-10 items-center gap-1 rounded-md bg-muted p-1 text-muted-foreground">
        {INVENTORY_SECTIONS.map((section) => {
          const active = isSectionActive(pathname, section.href);
          return (
            <li key={section.href}>
              <Link
                href={section.href}
                ref={active ? activeRef : undefined}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "inline-flex min-h-8 items-center whitespace-nowrap rounded-sm px-3 py-1.5 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                  active
                    ? "bg-background text-foreground shadow-sm"
                    : "hover:bg-background/60 hover:text-foreground"
                )}
              >
                {section.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

/** Page header shared by the inventory records screens. */
export function RecordsHeader({ title, description }: { title: string; description: string }) {
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-3xl font-bold">{title}</h1>
        <p className="max-w-3xl text-pretty text-muted-foreground">{description}</p>
      </div>
      <InventorySectionNav />
    </div>
  );
}
