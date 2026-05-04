import Link from "next/link";
import { ArrowRight, ShieldCheck } from "lucide-react";

// Trust strip — quiet legitimacy below the fold. Eight badges, each
// load-bearing for compliance-conscious buyers (manufacturing shops
// chasing ISO certification, EU customers under GDPR, anyone in
// regulated industries who needs an immutable audit trail).
//
// Layout note: 2-row grid on mobile (4-up × 2 rows) keeps each chip
// readable on a 320px screen; on sm+ we let them flow as a single
// wrapped row, which mirrors how karafiel#46 displays its strip.
//
// Order is canonical and contract-tested in
// `__tests__/landing-contract.test.tsx`. Don't reshuffle without
// updating the test in the same PR.

const badges = [
  "ISO 9001",
  "ISO 13849-1",
  "ISO 27001",
  "GDPR",
  "NOM-151",
  "MQTT 5.0",
  "Multi-tenant RLS",
  "99.9% SLA",
];

export function TrustBar() {
  return (
    <section className="border-b border-border/40 bg-card/30 py-14">
      <div className="mx-auto max-w-5xl px-4 sm:px-6 lg:px-8">
        <h2 className="text-center text-xs font-medium uppercase tracking-widest text-muted-foreground">
          Lo que respalda la operación
        </h2>

        <div
          className="mt-6 grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center sm:justify-center sm:gap-3"
          role="list"
          aria-label="Certificaciones, estándares y compromisos"
        >
          {badges.map((badge) => (
            <span
              key={badge}
              role="listitem"
              className="inline-flex items-center justify-center gap-2 rounded-full border border-border bg-card px-3 py-2 text-xs font-medium text-foreground/85 sm:px-4 sm:text-sm"
            >
              <ShieldCheck
                className="h-4 w-4 text-primary"
                aria-hidden="true"
              />
              {badge}
            </span>
          ))}
        </div>

        <p className="mx-auto mt-8 max-w-2xl text-center text-sm leading-relaxed text-muted-foreground">
          Infraestructura europea certificada ISO 27001 + ISO 9001 + GDPR.
          Parte del ecosistema MADFAM: integraciones nativas con Karafiel
          (compliance fiscal), Tezca (inteligencia legal) y Janua (SSO).
        </p>

        <p className="mt-4 text-center">
          <Link
            href="/compliance"
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
          >
            Ver detalles de cumplimiento
            <ArrowRight className="h-3.5 w-3.5" />
          </Link>
        </p>
      </div>
    </section>
  );
}
