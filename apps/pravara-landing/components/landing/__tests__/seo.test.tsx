/**
 * SEO contract tests.
 *
 * The metadata exported from app/layout.tsx is a load-bearing surface
 * for the marketing site — search engine snippets, social previews, and
 * canonical URL all live here. Visual review won't catch a regression
 * (the fields are invisible at runtime), so we lock the important ones
 * in code.
 *
 * What we DON'T assert: every field. Just the ones that, if dropped,
 * would silently break OpenGraph previews, social shares, or the
 * canonical signal to crawlers.
 */
import { describe, it, expect } from "vitest";
import { siteMetadata as metadata } from "../../../app/metadata";

describe("root layout metadata", () => {
  it("declares a canonical URL pointing at production", () => {
    expect(metadata.alternates?.canonical).toBe("https://mes.madfam.io");
  });

  it("ships an OpenGraph block with locale + image", () => {
    const og = metadata.openGraph;
    expect(og).toBeDefined();
    expect(og?.locale).toBe("es_MX");
    expect(og?.url).toBeTruthy();
    expect(og?.siteName).toBe("Pravara MES");
    expect(Array.isArray(og?.images) ? og?.images.length : 0).toBeGreaterThan(
      0,
    );
  });

  it("ships a Twitter card block", () => {
    const tw = metadata.twitter;
    expect(tw).toBeDefined();
    expect((tw as { card?: string } | undefined)?.card).toBe(
      "summary_large_image",
    );
  });

  it("allows indexing", () => {
    const r = metadata.robots;
    // Next typings allow object-or-string here; we use the object form.
    expect(typeof r === "object" && r !== null && "index" in r ? r.index : true)
      .toBe(true);
  });

  it("uses an es-MX-friendly title default", () => {
    const t = metadata.title;
    const value =
      typeof t === "string"
        ? t
        : (t as { default?: string } | undefined)?.default;
    expect(value).toMatch(/Pravara MES/);
  });
});
