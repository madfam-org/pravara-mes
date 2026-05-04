import { ShieldCheck } from "lucide-react";

// Trust strip — quiet legitimacy below the fold. The badges are
// load-bearing for compliance-conscious buyers (manufacturing
// shops chasing ISO certification, EU customers under GDPR, anyone
// in regulated industries who needs an immutable audit trail), but
// they're not the pitch — they live here as calm reassurance, not as
// a headline claim.
//
// Why these eight specifically:
//   - ISO 9001 (quality management): manufacturing-table-stakes
//   - ISO 13849-1 (machinery safety): why MES even exists for some buyers
//   - ISO 27001 (info security): the platform itself, via Hetzner-cert infra
//   - GDPR: EU-market readiness
//   - NOM-151: Mexican electronic timestamping for audit trails
//   - MQTT 5.0: industrial protocol native
//   - RLS multi-tenant: data isolation across sites/BUs
//   - 99.9% uptime: SLA stake
//
// Mirror karafiel's TrustBar pattern (8 badges + an ecosystem caption);
// karafiel#46 has a contract test that enforces the count, doing the
// same here when Pravara has a vitest setup wired in.

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
          className="mt-6 flex flex-wrap items-center justify-center gap-3"
          role="list"
          aria-label="Certificaciones, estándares y compromisos"
        >
          {badges.map((badge) => (
            <span
              key={badge}
              role="listitem"
              className="inline-flex items-center gap-2 rounded-full border border-border bg-card px-4 py-2 text-sm font-medium text-foreground/85"
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
      </div>
    </section>
  );
}
