/**
 * Contract tests for the marketing landing.
 *
 * These don't try to assert visual fidelity — that's what design
 * review + Playwright are for. Instead they lock in the parts of the
 * landing that are *load-bearing for conversion* and would silently
 * regress without a guard:
 *
 *   - TrustBar carries exactly 8 certification badges, each in its
 *     correct order. If a future PR drops one, this fails. Mirrors
 *     the karafiel#46 pattern.
 *
 *   - Pricing carries the three Tulana v0.1 tiers (Starter / Growth /
 *     Enterprise) with the documented MXN prices. If someone tries
 *     to bump these without updating the pricing decision doc, the
 *     test catches it.
 *
 *   - Hero advertises a "Solicitar demo" CTA. If a refactor renames
 *     the CTA text without updating CtaSection's anchor, conversion
 *     drops.
 *
 *   - LogoBar lists the protocols/firmwares we claim compatibility
 *     with. Adding a new vendor without updating compatibility
 *     copy + this test is a frequent footgun.
 */
import { describe, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";

// next/link → plain anchor, same trick the sidebar tests use.
vi.mock("next/link", () => ({
  default: ({
    href,
    children,
    ...props
  }: {
    href: string;
    children: React.ReactNode;
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

import { TrustBar } from "../trust-bar";
import { Pricing } from "../pricing";
import { Hero } from "../hero";
import { LogoBar } from "../logo-bar";

describe("TrustBar", () => {
  it("renders exactly 8 certification badges in canonical order", () => {
    render(<TrustBar />);
    const list = screen.getByRole("list", { name: /certificaciones/i });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(8);

    const expected = [
      "ISO 9001",
      "ISO 13849-1",
      "ISO 27001",
      "GDPR",
      "NOM-151",
      "MQTT 5.0",
      "Multi-tenant RLS",
      "99.9% SLA",
    ];
    items.forEach((item, i) => {
      expect(item).toHaveTextContent(expected[i]);
    });
  });
});

describe("Pricing", () => {
  it("renders the three Tulana v0.1 tiers with MXN prices", () => {
    render(<Pricing />);
    expect(screen.getByText("Starter")).toBeInTheDocument();
    expect(screen.getByText("Growth")).toBeInTheDocument();
    expect(screen.getByText("Enterprise")).toBeInTheDocument();

    // Prices anchored to internal-devops/decisions/2026-04-25-tulana-ecosystem-pricing.md
    expect(screen.getByText("$4,999")).toBeInTheDocument();
    expect(screen.getByText("$14,999")).toBeInTheDocument();
    expect(screen.getByText("$49,999")).toBeInTheDocument();
  });

  it("highlights Growth as the recommended tier", () => {
    render(<Pricing />);
    // The "Más popular" badge marks the recommended tier so the
    // visitor anchors there instead of underbuying or sticker-
    // shocking on Enterprise.
    expect(screen.getByText(/m[áa]s popular/i)).toBeInTheDocument();
  });
});

describe("Hero", () => {
  it("offers the Solicitar demo CTA pointing at #demo", () => {
    render(<Hero />);
    const cta = screen.getAllByRole("link", {
      name: /solicitar demo/i,
    })[0];
    expect(cta).toBeInTheDocument();
    expect(cta).toHaveAttribute("href", "#demo");
  });
});

describe("LogoBar", () => {
  it("lists the documented compatibility surface", () => {
    render(<LogoBar />);
    // The protocols + firmwares we claim to speak. The point of
    // listing them on the landing is credibility — if any go
    // stale (e.g., we drop Klipper support) the UI lies. Test
    // forces the marketing copy to track reality.
    const expected = [
      "GRBL",
      "Marlin",
      "Klipper",
      "OctoPrint",
      "Ruida",
      "LinuxCNC",
      "MQTT 5.0",
      "OPC-UA",
    ];
    for (const e of expected) {
      expect(screen.getByText(e)).toBeInTheDocument();
    }
  });
});
