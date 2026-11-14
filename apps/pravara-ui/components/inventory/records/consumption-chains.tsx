"use client";

import { Fragment, useId, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { consumptionAPI, type ConsumptionRow, type Disposition } from "@/lib/inventory-records-api";
import {
  BASIS_LABELS,
  CONSUMPTION_MATERIAL_UNITS,
  DISPOSITION_LABELS,
  JOB_KIND_LABELS,
  SOURCE_LABELS,
  formatDateTime,
  formatQty,
  label,
  parseQuantity,
  shortId,
  type ConsumptionChain,
} from "@/lib/inventory-records";
import { ConfirmRecordDialog } from "./confirm-record-dialog";
import { DispositionChoice } from "./print-job-complete-form";
import { RECORD_KEYS } from "./use-records";

const th = "p-3 text-xs font-medium text-muted-foreground";

function what(row: ConsumptionRow): string {
  if (row.kind === "machine_time") return "Tiempo de máquina";
  return row.registry_code ?? row.material_code ?? "—";
}

function amount(row: ConsumptionRow): string {
  return `${formatQty(row.quantity)} ${row.unit}`;
}

/** The sentence the correction confirmation shows. */
export function describeCorrection(row: ConsumptionRow, quantity: number, unit: string, disposition: string): string {
  const disp = row.kind === "material" ? ` (${label(DISPOSITION_LABELS, disposition)})` : "";
  return `Se agregará una corrección a ${what(row)} del trabajo ${shortId(row.job_id)}: ${amount(row)} → ${formatQty(quantity)} ${unit}${disp}. El registro de ${amount(row)} se conserva en el historial y deja de contar; las existencias se ajustan por la diferencia.`;
}

function CorrectionForm({ token, row, onDone }: { token: string; row: ConsumptionRow; onDone: () => void }) {
  const id = useId();
  const queryClient = useQueryClient();
  const isMaterial = row.kind === "material";
  const [quantity, setQuantity] = useState("");
  const [unit, setUnit] = useState(row.unit);
  const [disposition, setDisposition] = useState<Disposition | "">((row.disposition as Disposition) || "good");
  const [reason, setReason] = useState("");
  const [touched, setTouched] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const qty = parseQuantity(quantity);

  const problems: string[] = [];
  if (qty === null || qty < 0) problems.push("Escribe la cantidad correcta (0 o más, hasta 4 decimales).");
  if (reason.length > 500) problems.push("El motivo admite hasta 500 caracteres.");

  const mutation = useMutation({
    mutationFn: () =>
      consumptionAPI.correct(token, row.id, {
        quantity: qty as number,
        unit,
        disposition: isMaterial ? (disposition as Disposition) : undefined,
        reason: reason.trim() || undefined,
      }),
    onSuccess: () => {
      setConfirming(false);
      for (const key of [RECORD_KEYS.consumption, RECORD_KEYS.coverage, RECORD_KEYS.gaps, RECORD_KEYS.stock, RECORD_KEYS.materials]) {
        queryClient.invalidateQueries({ queryKey: key });
      }
      onDone();
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (problems.length > 0) return;
    mutation.reset();
    setConfirming(true);
  };

  return (
    <form onSubmit={submit} noValidate className="space-y-3 rounded-md border bg-background p-4" aria-label={`Corregir ${what(row)}`}>
      <p className="text-sm">
        Registrado: <strong className="tabular-nums">{amount(row)}</strong>. La corrección se agrega como un
        registro nuevo; el actual queda en el historial.
      </p>
      <div className="grid gap-3 sm:grid-cols-[8rem_8rem]">
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-qty`}>Cantidad correcta</Label>
          <Input id={`${id}-qty`} value={quantity} onChange={(e) => setQuantity(e.target.value)} inputMode="decimal" className="tabular-nums" autoFocus />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-unit`}>Unidad</Label>
          {isMaterial ? (
            <select
              id={`${id}-unit`}
              value={unit}
              onChange={(e) => setUnit(e.target.value)}
              className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              {Array.from(new Set([row.unit, ...CONSUMPTION_MATERIAL_UNITS])).map((u) => (
                <option key={u} value={u}>
                  {u}
                </option>
              ))}
            </select>
          ) : (
            <p id={`${id}-unit`} className="flex h-10 items-center font-mono text-sm">
              {row.unit}
            </p>
          )}
        </div>
      </div>
      {isMaterial && <DispositionChoice name={`${id}-disp`} value={disposition} onChange={setDisposition} />}
      <div className="space-y-1.5">
        <Label htmlFor={`${id}-reason`}>Motivo (opcional)</Label>
        <Textarea id={`${id}-reason`} rows={2} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
      </div>
      {touched && problems.length > 0 && (
        <ul role="alert" className="text-xs text-red-700 dark:text-red-400">
          {problems.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit">Revisar corrección</Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancelar
        </Button>
      </div>
      <ConfirmRecordDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Registrar corrección"
        description={describeCorrection(row, qty ?? 0, unit, disposition || "good")}
        irreversible="Las correcciones no se borran: para cambiarla otra vez, se corrige la corrección."
        confirmLabel="Registrar corrección"
        onConfirm={() => mutation.mutate()}
        isPending={mutation.isPending}
        error={mutation.error}
        forbiddenAction="corregir consumo"
        managerOnlyAction={false}
      />
    </form>
  );
}

/**
 * Consumption grouped into correction chains. Only the row that counts today
 * offers "Corregir"; what it replaced stays listed under it.
 */
export function ConsumptionChains({ token, chains }: { token: string; chains: ConsumptionChain[] }) {
  const [correcting, setCorrecting] = useState<string | null>(null);
  return (
    <div className="overflow-x-auto rounded-md border bg-card">
      <table className="w-full text-left">
        <thead>
          <tr className="border-b bg-muted/50">
            <th scope="col" className={th}>Trabajo</th>
            <th scope="col" className={th}>Material</th>
            <th scope="col" className={`${th} text-right`}>Cantidad</th>
            <th scope="col" className={th}>Base</th>
            <th scope="col" className={th}>Cómo salió</th>
            <th scope="col" className={th}>Terminó</th>
            <th scope="col" className={th}>
              <span className="sr-only">Acciones</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {chains.map(({ current, history, historyIncomplete, effective }) => {
            return (
              <Fragment key={current.id}>
                <tr className="border-b align-top">
                  <td className="whitespace-nowrap p-3 text-sm">
                    {label(JOB_KIND_LABELS, current.job_kind)}{" "}
                    <span className="font-mono text-muted-foreground" title={current.job_id}>
                      {shortId(current.job_id)}
                    </span>
                  </td>
                  <td className="p-3 text-sm">
                    {current.kind === "material" ? (
                      <span className="font-mono">{what(current)}</span>
                    ) : (
                      <span>{what(current)}</span>
                    )}
                    {current.registry_code && current.material_code && current.material_code !== current.registry_code && (
                      <span className="block text-xs text-muted-foreground">
                        registrado como <span className="font-mono">{current.material_code}</span>
                      </span>
                    )}
                    {current.kind === "material" && !current.registry_code && (
                      <Badge variant="warning" className="ml-2 whitespace-nowrap">
                        Sin código de registro
                      </Badge>
                    )}
                  </td>
                  <td className="whitespace-nowrap p-3 text-right text-sm font-medium tabular-nums">{amount(current)}</td>
                  <td className="whitespace-nowrap p-3 text-sm text-muted-foreground">
                    {label(BASIS_LABELS, current.basis)} · {label(SOURCE_LABELS, current.source)}
                    {current.supersedes_id && <span className="block text-xs">Corrección</span>}
                  </td>
                  <td className="p-3 text-sm">{current.kind === "material" ? label(DISPOSITION_LABELS, current.disposition) : "—"}</td>
                  <td className="whitespace-nowrap p-3 text-sm text-muted-foreground">{formatDateTime(current.occurred_at)}</td>
                  <td className="p-3 text-right">
                    {effective ? (
                      correcting !== current.id && (
                        <Button variant="outline" size="sm" onClick={() => setCorrecting(current.id)} aria-label={`Corregir ${what(current)} del trabajo ${shortId(current.job_id)}`}>
                          Corregir
                        </Button>
                      )
                    ) : (
                      <span className="text-xs text-muted-foreground">
                        {current.superseded_by
                          ? "Ya corregido (fuera de esta página)"
                          : "No cuenta: otro registro de este trabajo es el vigente"}
                      </span>
                    )}
                  </td>
                </tr>
                {(history.length > 0 || historyIncomplete) && (
                  <tr className="border-b bg-muted/30">
                    <td colSpan={7} className="px-3 pb-3 pt-1 text-xs text-muted-foreground">
                      <p className="font-medium text-foreground">Historial (se conserva; ya no cuenta)</p>
                      <ol className="mt-1 space-y-0.5">
                        {history.map((h) => (
                          <li key={h.id} className="tabular-nums">
                            {amount(h)} · {label(BASIS_LABELS, h.basis)} · {label(SOURCE_LABELS, h.source)}
                            {h.kind === "material" && ` · ${label(DISPOSITION_LABELS, h.disposition)}`} · registrado el{" "}
                            {formatDateTime(h.created_at)} · sustituido por corrección
                          </li>
                        ))}
                        {historyIncomplete && <li>Hay registros anteriores fuera de esta página.</li>}
                      </ol>
                    </td>
                  </tr>
                )}
                {correcting === current.id && (
                  <tr className="border-b">
                    <td colSpan={7} className="p-3">
                      <CorrectionForm token={token} row={current} onDone={() => setCorrecting(null)} />
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
