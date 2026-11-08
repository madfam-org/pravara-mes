"use client";

import { AlertTriangle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

interface QueryErrorStateProps {
  /** What failed to load, e.g. "inventory". */
  what: string;
  error?: unknown;
  onRetry?: () => void;
  isRetrying?: boolean;
  /** Render without the card frame (inside another card). */
  inline?: boolean;
}

function errorMessage(error: unknown): string | null {
  if (error instanceof Error && error.message) {
    return error.message.replace(/[.\s]+$/, "");
  }
  return null;
}

/**
 * A failed load, said plainly: what could not be loaded, the reason the
 * server gave, and a retry. Never rendered as an empty list.
 */
export function QueryErrorState({
  what,
  error,
  onRetry,
  isRetrying,
  inline,
}: QueryErrorStateProps) {
  const detail = errorMessage(error);
  const body = (
    <div
      role="alert"
      className="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center"
    >
      <AlertTriangle className="h-8 w-8 text-destructive" aria-hidden="true" />
      <h3 className="text-base font-semibold">Couldn&apos;t load {what}</h3>
      <p className="max-w-md text-sm text-muted-foreground">
        {detail ? `${detail}. ` : ""}Try again in a moment.
      </p>
      {onRetry && (
        <Button
          variant="outline"
          size="sm"
          className="mt-2"
          onClick={onRetry}
          disabled={isRetrying}
        >
          <RefreshCw
            className={`mr-2 h-4 w-4 ${isRetrying ? "animate-spin" : ""}`}
            aria-hidden="true"
          />
          {isRetrying ? "Retrying..." : "Try again"}
        </Button>
      )}
    </div>
  );

  if (inline) return body;
  return (
    <Card>
      <CardContent className="p-0">{body}</CardContent>
    </Card>
  );
}
