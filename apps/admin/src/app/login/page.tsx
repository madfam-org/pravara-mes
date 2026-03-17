"use client";

import { useState, useEffect, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { Shield } from "lucide-react";

const januaConfigured =
  !!(process.env.NEXT_PUBLIC_JANUA_PUBLISHABLE_KEY || process.env.NEXT_PUBLIC_OIDC_CLIENT_ID);

export default function LoginPage() {
  const router = useRouter();

  if (!januaConfigured) {
    return <UnconfiguredFallback />;
  }

  return (
    <Suspense
      fallback={
        <PageShell subtitle="Cargando...">
          <div className="flex justify-center py-8">
            <div className="h-8 w-8 animate-spin rounded-full border-4 border-blue-600 border-t-transparent" />
          </div>
        </PageShell>
      }
    >
      <LoginFormContent router={router} />
    </Suspense>
  );
}

function LoginFormContent({
  router,
}: {
  router: ReturnType<typeof useRouter>;
}) {
  const { isAuthenticated, isLoading, login } = useAuth();
  const searchParams = useSearchParams();
  const [ssoError, setSsoError] = useState<string | null>(null);

  useEffect(() => {
    if (isAuthenticated && !isLoading) {
      router.replace("/");
    }
  }, [isAuthenticated, isLoading, router]);

  // Pick up SSO errors from callback redirect
  useEffect(() => {
    const error = searchParams.get("sso_error");
    if (error) {
      setSsoError(error);
    }
  }, [searchParams]);

  if (isLoading) {
    return (
      <PageShell subtitle="Verificando sesion...">
        <div className="flex justify-center py-8">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-blue-600 border-t-transparent" />
        </div>
      </PageShell>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 p-4">
      <div className="w-full max-w-md space-y-6">
        {/* Header */}
        <div className="text-center">
          <h2 className="text-3xl font-bold tracking-tight text-gray-900">
            PravaraMES Admin
          </h2>
          <p className="mt-2 text-sm text-gray-500">
            Consola de administracion
          </p>
        </div>

        {/* SSO Error */}
        {ssoError && (
          <div
            role="alert"
            className="rounded-md bg-red-50 border border-red-200 px-4 py-3 text-sm text-red-700"
          >
            {ssoError}
          </div>
        )}

        {/* Login Card */}
        <div className="rounded-xl border border-gray-200 bg-white p-6 shadow-sm space-y-6">
          {/* Restricted Access Notice */}
          <div className="flex items-center gap-3 p-3 rounded-lg bg-amber-50 border border-amber-200">
            <Shield className="h-5 w-5 text-amber-600 shrink-0" />
            <div className="text-sm">
              <p className="font-medium text-gray-900">Acceso restringido</p>
              <p className="text-gray-500">
                Solo operadores autorizados pueden acceder.
              </p>
            </div>
          </div>

          {/* Enterprise SSO Button */}
          <div className="space-y-3">
            <button
              onClick={login}
              className="w-full flex justify-center items-center gap-2 rounded-md bg-blue-600 px-4 py-3 text-sm font-medium text-white hover:bg-blue-700 transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            >
              <Shield className="h-4 w-4" />
              Iniciar sesion con Janua SSO
            </button>
            <p className="text-center text-xs text-gray-400">
              Seras redirigido a{" "}
              <span className="font-mono text-blue-600">auth.madfam.io</span>
            </p>
          </div>

          {/* Legal Links */}
          <p className="text-xs text-center text-gray-400">
            Al continuar, aceptas los{" "}
            <a
              href="https://madfam.io/terms"
              className="underline hover:text-gray-600"
              target="_blank"
              rel="noopener noreferrer"
            >
              Terminos de Servicio
            </a>{" "}
            y la{" "}
            <a
              href="https://madfam.io/privacy"
              className="underline hover:text-gray-600"
              target="_blank"
              rel="noopener noreferrer"
            >
              Politica de Privacidad
            </a>
            .
          </p>
        </div>

        {/* Footer */}
        <p className="text-xs text-center text-gray-400 opacity-60">
          Powered by Janua
        </p>
      </div>
    </div>
  );
}

function PageShell({
  subtitle,
  children,
}: {
  subtitle: string;
  children: React.ReactNode;
}) {
  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <div className="w-full max-w-md space-y-8 px-4">
        <div className="text-center">
          <h2 className="text-3xl font-bold tracking-tight text-gray-900">
            PravaraMES Admin
          </h2>
          <p className="mt-2 text-sm text-gray-500">{subtitle}</p>
        </div>
        {children}
      </div>
    </div>
  );
}

function UnconfiguredFallback() {
  return (
    <PageShell subtitle="Autenticacion no configurada">
      <div className="rounded-lg border border-gray-200 bg-gray-50 p-6 space-y-4">
        <p className="text-sm text-gray-500">
          Las variables de entorno de Janua no estan configuradas. Para
          habilitar autenticacion, agrega las siguientes variables:
        </p>
        <pre className="text-xs bg-gray-100 p-3 rounded overflow-x-auto text-gray-700">
          {`NEXT_PUBLIC_JANUA_BASE_URL=https://auth.madfam.io
NEXT_PUBLIC_JANUA_PUBLISHABLE_KEY=jnc_...
JANUA_SECRET_KEY=jns_...`}
        </pre>
        <a
          href="/"
          className="inline-flex items-center justify-center rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition-colors"
        >
          Continuar sin autenticacion
        </a>
      </div>
    </PageShell>
  );
}
