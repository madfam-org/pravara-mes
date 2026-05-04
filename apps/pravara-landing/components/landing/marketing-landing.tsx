// Top-level composition for mes.madfam.io. Section flow follows the
// conversion funnel:
//   Hero → LogoBar → ProblemStatement → PersonaCards → HowItWorks →
//   FeatureGrid → TrustBar → Pricing → CtaSection → Footer.
//
// The StickyCta floats above the main content and only appears between
// leaving the hero and entering #demo (see sticky-cta.tsx).
//
// The empty <span id="hero-sentinel"> right after Hero exists so
// StickyCta's IntersectionObserver knows when the hero has scrolled
// out — keeps the observer logic simple and doesn't require the Hero
// component to expose a ref.

import { Hero } from "./hero";
import { LogoBar } from "./logo-bar";
import { ProblemStatement } from "./problem-statement";
import { PersonaCards } from "./persona-cards";
import { HowItWorks } from "./how-it-works";
import { FeatureGrid } from "./feature-grid";
import { TrustBar } from "./trust-bar";
import { Pricing } from "./pricing";
import { CtaSection } from "./cta-section";
import { LandingFooter } from "./footer";
import { LandingNav } from "./nav";
import { StickyCta } from "./sticky-cta";
import { Reveal } from "./reveal";

export function MarketingLanding() {
  return (
    <main className="relative">
      <StickyCta />
      <LandingNav />
      <Hero />
      <span id="hero-sentinel" aria-hidden className="block h-px" />
      <LogoBar />
      <Reveal>
        <ProblemStatement />
      </Reveal>
      <Reveal>
        <PersonaCards />
      </Reveal>
      <Reveal>
        <HowItWorks />
      </Reveal>
      <Reveal>
        <FeatureGrid />
      </Reveal>
      <Reveal>
        <TrustBar />
      </Reveal>
      <Reveal>
        <Pricing />
      </Reveal>
      <CtaSection />
      <LandingFooter />
    </main>
  );
}
