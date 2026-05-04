"use client";

import { useState } from "react";
import { ArrowRight, Loader2, Mail, CheckCircle2 } from "lucide-react";

// Final ask. Form is mailto-based today since the /api/demo-request
// endpoint isn't wired yet — when the backend ships, swap the handler
// for fetch('/api/demo-request') without touching the form UI.

const SALES_EMAIL = "ventas@madfam.io";

export function CtaSection() {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [company, setCompany] = useState("");
  const [machines, setMachines] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  function reset() {
    setName("");
    setEmail("");
    setCompany("");
    setMachines("");
    setSubmitted(false);
    setSubmitting(false);
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);

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
    // visitor's email client may be slow to launch). The full
    // confirmation panel stays up until they click "enviar otra".
    setTimeout(() => {
      setSubmitting(false);
      setSubmitted(true);
    }, 800);
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

        {/* Form column — flips into a confirmation panel on submit. */}
        <div className="relative rounded-2xl border border-border bg-card p-7 shadow-xl">
          {submitted ? (
            <div
              role="status"
              aria-live="polite"
              className="flex flex-col items-center justify-center py-10 text-center"
            >
              <span className="mb-5 inline-flex h-16 w-16 items-center justify-center rounded-full bg-emerald-500/15 text-emerald-400 ring-4 ring-emerald-500/10">
                <CheckCircle2 className="h-8 w-8" />
              </span>
              <h3 className="text-2xl font-semibold tracking-tight">
                Solicitud enviada
              </h3>
              <p className="mt-3 max-w-sm text-sm leading-relaxed text-muted-foreground">
                Te contactaremos en menos de 24 horas para coordinar la
                demo. Mientras tanto, puedes ir explorando las
                capacidades del producto.
              </p>
              <div className="mt-8 flex flex-col gap-2 sm:flex-row">
                <button
                  type="button"
                  onClick={reset}
                  className="inline-flex h-11 items-center justify-center rounded-lg border border-border bg-card px-5 text-sm font-medium text-foreground transition-colors hover:bg-accent"
                >
                  Enviar otra solicitud
                </button>
                <a
                  href="#features"
                  className="inline-flex h-11 items-center justify-center rounded-lg bg-primary px-5 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90"
                >
                  Ver capacidades
                </a>
              </div>
            </div>
          ) : (
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
                {submitting ? (
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
          )}
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
