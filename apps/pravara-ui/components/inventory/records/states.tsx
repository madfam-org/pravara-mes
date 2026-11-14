"use client";

import type { ReactNode } from "react";
import { AlertTriangle, Lock, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { explainError, isForbidden, managerOnly } from "@/lib/inventory-records";

/** Placeholder rows while a list loads (same look as the inventory list). */
export function LoadingRows({ rows = 5, label }: { rows?: number; label: string }) {
  return (
    <Card className="animate-pulse" aria-busy="true">
      <CardContent className="py-8">
        <span className="sr-only">Cargando {label}…</span>
        <div className="space-y-3" aria-hidden="true">
          {Array.from({ length: rows }, (_, i) => (
            <div key={i} className="h-10 w-full rounded bg-muted" />
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

export function EmptyState({
  icon,
  title,
  children,
  action,
}: {
  icon: ReactNode;
  title: string;
  children: ReactNode;
  action?: ReactNode;
}) {
  return (
    <Card>
      <CardContent className="flex flex-col items-center justify-center px-4 py-12 text-center">
        <span className="text-muted-foreground [&_svg]:h-10 [&_svg]:w-10" aria-hidden="true">
          {icon}
        </span>
        <h3 className="mt-4 text-lg font-semibold">{title}</h3>
        <div className="mt-1 max-w-prose text-sm text-muted-foreground">{children}</div>
        {action && <div className="mt-4">{action}</div>}
      </CardContent>
    </Card>
  );
}

/**
 * A failed read, said plainly. 401/403 is a permission state (no retry
 * button: retrying will not help); anything else says what happened, the
 * server's words, and offers a retry. Never rendered as an empty list.
 */
export function ReadErrorState({
  what,
  error,
  onRetry,
  isRetrying,
}: {
  what: string;
  error: unknown;
  onRetry?: () => void;
  isRetrying?: boolean;
}) {
  const explained = explainError(error);
  if (isForbidden(error)) {
    return (
      <Card>
        <CardContent
          role="alert"
          className="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center"
        >
          <Lock className="h-8 w-8 text-muted-foreground" aria-hidden="true" />
          <h3 className="text-base font-semibold">
            {explained.status === 401 ? "Tu sesión expiró" : `No tienes acceso a ${what}`}
          </h3>
          <p className="max-w-md text-sm text-muted-foreground">
            {explained.status === 401
              ? "Vuelve a iniciar sesión para continuar."
              : "Pide a quien administra Pravara en tu organización que te dé acceso de lectura al inventario."}
          </p>
        </CardContent>
      </Card>
    );
  }
  return (
    <Card>
      <CardContent
        role="alert"
        className="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center"
      >
        <AlertTriangle className="h-8 w-8 text-red-700 dark:text-red-400" aria-hidden="true" />
        <h3 className="text-base font-semibold">No se pudo cargar {what}</h3>
        <p className="max-w-md text-sm text-muted-foreground">
          {explained.meaning}
          {explained.serverMessage ? ` Detalle: ${explained.serverMessage}.` : ""} Intenta de
          nuevo en un momento.
        </p>
        {onRetry && (
          <Button variant="outline" size="sm" className="mt-2" onClick={onRetry} disabled={isRetrying}>
            <RefreshCw className={isRetrying ? "animate-spin" : ""} aria-hidden="true" />
            {isRetrying ? "Reintentando…" : "Reintentar"}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

/**
 * A failed write, inline next to the form that sent it. States that nothing
 * was recorded (a failed request changes nothing, per the contract) and is
 * never retried automatically.
 */
export function WriteErrorMessage({
  error,
  extra,
  forbiddenAction,
  managerOnlyAction = true,
}: {
  error: unknown;
  extra?: ReactNode;
  /** What was attempted, for a 403 ("registrar entradas"). */
  forbiddenAction?: string;
  /** Whether the action is one of the role-gated inventory writes. */
  managerOnlyAction?: boolean;
}) {
  if (!error) return null;
  const explained = explainError(error);
  if (explained.status === 403) {
    return (
      <div
        role="alert"
        className="flex gap-2 rounded-md border bg-muted/50 p-3 text-sm text-foreground"
      >
        <Lock className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <div>
          <p className="font-medium">
            No tienes permiso para {forbiddenAction ?? "esta acción"}. No se registró nada.
          </p>
          <p className="mt-1 text-muted-foreground">
            {forbiddenAction && managerOnlyAction
              ? managerOnly(forbiddenAction)
              : "Pide a quien administra Pravara en tu organización que revise tu acceso."}
          </p>
        </div>
      </div>
    );
  }
  return (
    <div
      role="alert"
      className="rounded-md border border-red-300 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300"
    >
      <p className="font-medium">No se registró: {explained.meaning}</p>
      {explained.serverMessage && (
        <p className="mt-1 break-words">Respuesta del servidor: {explained.serverMessage}</p>
      )}
      {extra}
    </div>
  );
}

/** Previous/next for an offset-paginated list. */
export function Pager({
  total,
  limit,
  offset,
  onOffset,
  noun,
}: {
  total: number;
  limit: number;
  offset: number;
  onOffset: (next: number) => void;
  noun: string;
}) {
  if (total <= limit && offset === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {total} {noun}
      </p>
    );
  }
  const first = total === 0 ? 0 : offset + 1;
  const last = Math.min(offset + limit, total);
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <p className="text-sm text-muted-foreground" aria-live="polite">
        {first}–{last} de {total} {noun}
      </p>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={offset === 0}
          onClick={() => onOffset(Math.max(0, offset - limit))}
        >
          Anteriores
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={offset + limit >= total}
          onClick={() => onOffset(offset + limit)}
        >
          Siguientes
        </Button>
      </div>
    </div>
  );
}
