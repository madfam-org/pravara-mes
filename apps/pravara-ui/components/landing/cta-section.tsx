"use client";

import { useState } from "react";
import { ArrowRight, Loader2, Mail, CheckCircle2 } from "lucide-react";

// Final ask. The visitor has read everything we have to say —
// either the page worked or it didn't. CtaSection is the last shot.
//
// Form behavior: client-side validation only (no server endpoint
// wired today since the product backend is gated behind missing
// secrets — see #44 in the operator handoff). The submit handler
// posts to a mailto-with-payload fallback so leads still land
// somewhere even before the real demo-request API exists. When the
// backend ships, swap the handler for fetch('/api/demo-request')
// without touching the form UI.

const SALES_EMAIL = "ventas@madfam.io";

export function CtaSection() {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [company, setCompany] = useState("");
  const [machines, setMachines] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);

    // Demo lead capture is currently mailto-based since the
    // /api/demo-request endpoint isn't live yet. The mailto
    // pre-fills with everything the visitor entered so the
    // sales inbox gets a structured lead even from a cold
    // form. Replace with `fetch('/api/demo-request', ...)`
    // once the backend route exists.
    const subject = encodeURIComponent(
      `Demo Pravara MES — ${company || name}`,
    );
    const body = encodeURIComponent(
      [
        `Nombre: ${name}`,
        `Email: ${email}`,
        `Empresa: ${company}`,
        `Número aproximado de máquinas: ${machines || "no especificado"}`,
        ``,
        `(Lead capturado desde mes.madfam.io)`,
      ].join("\n"),
    );

    window.location.href = `mailto:${SALES_EMAIL}?subject=${subject}&body=${body}`;

    // Optimistic UI flip even if the mailto opens a client (the
    // visitor's email client may be slow to launch). Resets after
    // 4s so they can submit again if needed.
    setTimeout(() => {
      setSubmitting(false);
      setSubmitted(true);
    }, 800);

    setTimeout(() => setSubmitted(false), 4000);
  }

  return (
    <section
      id="demo"
      className="relative overflow-hidden border-b border-border/40 bg-card/30 py-20"
    >
      <div
        aria-hidden
        className="absolute inset-0 -z-10 bg-[radial-gradient(ellipse_at_bottom,_hsl(var(--primary)/0.08),_transparent_60%)]"
      />

      <div className="mx-auto grid max-w-6xl gap-12 px-4 sm:px-6 lg:grid-cols-2 lg:gap-16 lg:px-8">
        {/* Pitch column */}
        <div>
          <p className="mb-3 text-xs font-semibold uppercase tracking-widest text-primary">
            Listos para empezar
          </p>
          <h2 className="text-3xl font-bold tracking-tight sm:text-4xl">
            Una demo de 30 minutos. Tu fábrica, en una pantalla.
          </h2>
          <p className="mt-5 text-lg leading-relaxed text-muted-foreground">
            Te conectamos a un ingeniero de Pravara para una sesión
            corta donde te mostramos:
          </p>
          <ul className="mt-6 space-y-3 text-sm text-foreground/90">
            {[
              "Cómo se vería tu floor plan en el visualizador 3D — usando tus máquinas reales.",
              "Qué tan rápido conectamos una máquina nueva (la respuesta corta: minutos).",
              "Un caso real de trazabilidad por lote, fin a fin, con COC/COA generado al cierre.",
              "Cuál de los tres planes encaja con tu operación — sin upselling.",
            ].map((b) => (
              <li key={b} className="flex items-start gap-2.5">
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                <span className="leading-relaxed">{b}</span>
              </li>
            ))}
          </ul>
          <p className="mt-8 flex items-center gap-2 text-sm text-muted-foreground">
            <Mail className="h-4 w-4" />
            ¿Prefieres email directo?{" "}
            <a
              href={`mailto:${SALES_EMAIL}`}
              className="text-foreground underline-offset-4 hover:underline"
            >
              {SALES_EMAIL}
            </a>
          </p>
        </div>

        {/* Form column */}
        <div className="relative rounded-2xl border border-border bg-card p-7 shadow-xl">
          <form onSubmit={handleSubmit} className="space-y-4">
            <Field
              id="name"
              label="Nombre"
              value={name}
              onChange={setName}
              placeholder="María Hernández"
              autoComplete="name"
              required
            />
            <Field
              id="email"
              label="Email de trabajo"
              type="email"
              value={email}
              onChange={setEmail}
              placeholder="maria@tufabrica.com"
              autoComplete="email"
              required
            />
            <Field
              id="company"
              label="Empresa"
              value={company}
              onChange={setCompany}
              placeholder="Fábrica Hernández S.A."
              autoComplete="organization"
              required
            />
            <Field
              id="machines"
              label="Máquinas en operación (aprox.)"
              value={machines}
              onChange={setMachines}
              placeholder="12"
              type="number"
              optional
            />

            <button
              type="submit"
              disabled={submitting}
              className="group inline-flex h-12 w-full items-center justify-center gap-2 rounded-lg bg-primary text-base font-medium text-primary-foreground shadow-lg shadow-primary/20 transition-all hover:opacity-90 disabled:opacity-60"
            >
              {submitted ? (
                <>
                  <CheckCircle2 className="h-4 w-4" />
                  Solicitud enviada
                </>
              ) : submitting ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Enviando…
                </>
              ) : (
                <>
                  Solicitar demo
                  <ArrowRight className="h-4 w-4 transition-transform group-hover:translate-x-0.5" />
                </>
              )}
            </button>

            <p className="text-center text-xs leading-relaxed text-muted-foreground">
              Al enviar aceptas que un ingeniero de Pravara te
              contacte para coordinar la demo. No compartimos tus
              datos con terceros.
            </p>
          </form>
        </div>
      </div>
    </section>
  );
}

function Field({
  id,
  label,
  value,
  onChange,
  placeholder,
  type = "text",
  autoComplete,
  required,
  optional,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: string;
  autoComplete?: string;
  required?: boolean;
  optional?: boolean;
}) {
  return (
    <div>
      <label
        htmlFor={id}
        className="mb-1.5 flex items-center justify-between text-sm font-medium"
      >
        <span>{label}</span>
        {optional && (
          <span className="text-xs text-muted-foreground">opcional</span>
        )}
      </label>
      <input
        id={id}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoComplete={autoComplete}
        required={required}
        className="h-11 w-full rounded-lg border border-input bg-background px-3.5 text-sm text-foreground transition-colors focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
      />
    </div>
  );
}
