/**
 * Contract tests for the marketing landing.
 *
 * These don't try to assert visual fidelity — that's what design review
 * + Playwright are for. Instead they lock in the parts of the landing
 * that are *load-bearing for conversion* and would silently regress
 * without a guard:
 *
 *   - TrustBar: 8 certification badges in canonical order. Mirrors the
 *     karafiel#46 contract + the original pravara-ui test.
 *   - Pricing: three Tulana v0.1 tiers with the documented MXN prices.
 *   - Hero: "Solicitar demo" CTA points at #demo.
 *   - LogoBar: the documented compatibility surface.
 *   - HowItWorks: the three steps render in canonical order (new
 *     section introduced in pravara-landing).
 *   - StickyCta: hides initially (visitor hasn't scrolled past hero
 *     yet), so the bar is below the viewport on first paint.
 */
import { describe, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";

// next/link → plain anchor, same trick the dashboard sidebar tests use.
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
import { HowItWorks } from "../how-it-works";
import { StickyCta } from "../sticky-cta";

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

  it("links to the compliance details page", () => {
    render(<TrustBar />);
    const link = screen.getByRole("link", { name: /detalles de cumplimiento/i });
    expect(link).toHaveAttribute("href", "/compliance");
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
    expect(screen.getByText(/m[áa]s popular/i)).toBeInTheDocument();
  });

  it("shows MXN/mes period prominently on every tier", () => {
    render(<Pricing />);
    // Three tiers × one period label each.
    const periods = screen.getAllByText(/MXN \/ mes/i);
    expect(periods).toHaveLength(3);
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

describe("HowItWorks", () => {
  it("renders three steps in canonical order", () => {
    render(<HowItWorks />);
    const list = screen.getByRole("list", { name: /tres pasos/i });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(3);

    // Each step's distinctive title in the documented order.
    expect(items[0]).toHaveTextContent(/conecta/i);
    expect(items[1]).toHaveTextContent(/captura/i);
    expect(items[2]).toHaveTextContent(/audita/i);
  });

  it("labels each step with its 01/02/03 ordinal", () => {
    render(<HowItWorks />);
    expect(screen.getByText(/paso 01/i)).toBeInTheDocument();
    expect(screen.getByText(/paso 02/i)).toBeInTheDocument();
    expect(screen.getByText(/paso 03/i)).toBeInTheDocument();
  });
});

describe("StickyCta", () => {
  it("renders hidden initially (visitor still in the hero)", () => {
    render(<StickyCta />);
    const bar = screen.getByTestId("sticky-cta");
    expect(bar).toHaveAttribute("aria-hidden", "true");
    // The hidden state translates the bar above the viewport.
    expect(bar.className).toMatch(/-translate-y-full/);
  });
});
