import Link from "next/link";
import { ArrowLeft } from "lucide-react";

export default function NotFound() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center px-6 text-center">
      <p className="mb-3 text-xs font-semibold uppercase tracking-widest text-primary">
        404
      </p>
      <h1 className="text-4xl font-bold tracking-tight sm:text-5xl">
        Esa página no existe.
      </h1>
      <p className="mt-4 max-w-md text-base text-muted-foreground">
        Puede que el enlace haya cambiado o que la sección haya sido
        movida. Volvamos al inicio.
      </p>
      <Link
        href="/"
        className="mt-8 inline-flex h-11 items-center justify-center gap-2 rounded-lg bg-primary px-6 text-sm font-medium text-primary-foreground shadow-lg shadow-primary/20 transition-opacity hover:opacity-90"
      >
        <ArrowLeft className="h-4 w-4" />
        Volver a Pravara MES
      </Link>
    </main>
  );
}
