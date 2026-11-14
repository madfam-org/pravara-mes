"use client";

import type { ReactNode } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { WriteErrorMessage } from "./states";

interface ConfirmRecordDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** One sentence saying exactly what will be recorded. */
  description: ReactNode;
  /** Shown under the sentence when the action cannot be undone. */
  irreversible?: string;
  confirmLabel: string;
  onConfirm: () => void;
  isPending: boolean;
  error: unknown;
  /** What was attempted, for a 403 answer. */
  forbiddenAction?: string;
  /** False for shop-floor writes open to every signed-in person. */
  managerOnlyAction?: boolean;
  /** More about a failure (for example, which records conflict). */
  errorExtra?: ReactNode;
}

/**
 * Confirms a record once before it is sent. The dialog stays open on a
 * failure and shows the API's answer; it never resends by itself.
 */
export function ConfirmRecordDialog({
  open,
  onOpenChange,
  title,
  description,
  irreversible,
  confirmLabel,
  onConfirm,
  isPending,
  error,
  forbiddenAction,
  managerOnlyAction,
  errorExtra,
}: ConfirmRecordDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => !isPending && onOpenChange(next)}>
      <DialogContent lang="es-MX">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription className="text-foreground">{description}</DialogDescription>
          {irreversible && <p className="text-sm font-medium">{irreversible}</p>}
        </DialogHeader>
        <WriteErrorMessage
          error={error}
          forbiddenAction={forbiddenAction}
          managerOnlyAction={managerOnlyAction}
          extra={errorExtra}
        />
        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isPending}>
            Volver
          </Button>
          <Button onClick={onConfirm} disabled={isPending}>
            {isPending && <Spinner size="sm" />}
            {isPending ? "Registrando…" : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
