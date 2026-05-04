// mes.madfam.io/ — public marketing surface for Pravara MES.
// All sections live in @/components/landing/* (split for testability +
// per-section cache invalidation). The composition itself is just glue.

import { MarketingLanding } from "@/components/landing/marketing-landing";

export default function HomePage() {
  return <MarketingLanding />;
}
