// mes.madfam.io/ — public marketing landing for the Pravara MES product.
// All sections live in @/components/landing/* (split for testability +
// per-section cache invalidation). The composition itself is just glue.

import { MarketingLanding } from "@/components/landing/marketing-landing";

export default function LandingPage() {
  return <MarketingLanding />;
}
