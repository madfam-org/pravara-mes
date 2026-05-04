// Top-level composition for mes.madfam.io. Orders the section flow
// for the conversion funnel: visitor lands on Hero → pain articulated
// in ProblemStatement → "is this for me?" answered by PersonaCards →
// "what does it actually do?" via FeatureGrid → social proof in
// TrustBar → cost in Pricing → final ask in CtaSection.
//
// The order is deliberate. Pricing comes AFTER feature articulation +
// trust signals — visitors need to know what they're buying and that
// we're competent before sticker shock matters. CtaSection lives at
// the bottom for visitors who scroll-through-and-decide; the Hero
// also has a primary CTA for visitors who decide on first impression.

import { Hero } from "./hero";
import { LogoBar } from "./logo-bar";
import { ProblemStatement } from "./problem-statement";
import { PersonaCards } from "./persona-cards";
import { FeatureGrid } from "./feature-grid";
import { TrustBar } from "./trust-bar";
import { Pricing } from "./pricing";
import { CtaSection } from "./cta-section";
import { LandingFooter } from "./footer";
import { LandingNav } from "./nav";

export function MarketingLanding() {
  return (
    <main className="relative">
      <LandingNav />
      <Hero />
      <LogoBar />
      <ProblemStatement />
      <PersonaCards />
      <FeatureGrid />
      <TrustBar />
      <Pricing />
      <CtaSection />
      <LandingFooter />
    </main>
  );
}
