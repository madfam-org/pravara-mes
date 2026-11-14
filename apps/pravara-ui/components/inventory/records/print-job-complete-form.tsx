"use client";

import { useId, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  consumptionAPI,
  type Disposition,
  type PrintJobCompletionResult,
} from "@/lib/inventory-records-api";
import {
  CONSUMPTION_MATERIAL_UNITS,
  DISPOSITION_HINTS,
  DISPOSITION_LABELS,
  formatQty,
  isUuid,
  parseQuantity,
} from "@/lib/inventory-records";
import { ConfirmRecordDialog } from "./confirm-record-dialog";
import { RECORD_KEYS } from "./use-records";

export const DISPOSITIONS: Disposition[] = ["good", "scrap", "failed_print"];

export function DispositionChoice({
  name,
  value,
  onChange,
  invalid,
}: {
  name: string;
  value: Disposition | "";
  onChange: (d: Disposition) => void;
  invalid?: boolean;
}) {
  return (
    <fieldset className="space-y-2" aria-invalid={invalid || undefined}>
      <legend className="text-sm font-medium">Cómo salió</legend>
      <div className="grid gap-2 sm:grid-cols-3">
        {DISPOSITIONS.map((d) => (
          <label
            key={d}
            className="flex cursor-pointer items-start gap-2 rounded-md border p-3 text-sm has-[:checked]:border-primary has-[:checked]:bg-muted/60 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring"
          >
            <input
              type="radio"
              name={name}
              value={d}
              checked={value === d}
              onChange={() => onChange(d)}
              className="mt-0.5 h-4 w-4 accent-primary"
            />
            <span>
              <span className="block font-medium">{DISPOSITION_LABELS[d]}</span>
              <span className="block text-xs text-muted-foreground">{DISPOSITION_HINTS[d]}</span>
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/**
 * Marks a print job finished: POST /v1/print-jobs/{id}/complete. A record
 * only; nothing is sent to any machine.
 */
export function PrintJobCompleteForm({ token }: { token: string }) {
  const id = useId();
  const queryClient = useQueryClient();
  const [printJobId, setPrintJobId] = useState("");
  const [weighed, setWeighed] = useState(false);
  const [quantity, setQuantity] = useState("");
  const [unit, setUnit] = useState("g");
  const [code, setCode] = useState("");
  const [disposition, setDisposition] = useState<Disposition | "">("");
  const [touched, setTouched] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [result, setResult] = useState<PrintJobCompletionResult | null>(null);

  const qty = parseQuantity(quantity);
  const problems: string[] = [];
  if (!isUuid(printJobId)) problems.push("Escribe el identificador de la impresión (un UUID con guiones).");
  if (weighed && (qty === null || qty <= 0)) problems.push("Escribe la cantidad pesada, mayor que cero.");
  if (!disposition) problems.push("Elige cómo salió la impresión.");

  const mutation = useMutation({
    mutationFn: () =>
      consumptionAPI.completePrintJob(token, printJobId.trim().toLowerCase(), {
        disposition: disposition as Disposition,
        ...(weighed
          ? { material_quantity: qty as number, material_unit: unit, material_code: code.trim() || undefined }
          : {}),
      }),
    onSuccess: (res) => {
      setConfirming(false);
      setResult(res);
      for (const key of [RECORD_KEYS.consumption, RECORD_KEYS.coverage, RECORD_KEYS.gaps, RECORD_KEYS.stock, RECORD_KEYS.materials]) {
        queryClient.invalidateQueries({ queryKey: key });
      }
      setPrintJobId("");
      setWeighed(false);
      setQuantity("");
      setCode("");
      setDisposition("");
      setTouched(false);
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (problems.length > 0) return;
    setResult(null);
    mutation.reset();
    setConfirming(true);
  };

  const what = weighed
    ? ` y se registrará el material usado: ${formatQty(qty ?? 0)} ${unit}${code.trim() ? ` de ${code.trim()}` : " del material de su perfil"}, como «${DISPOSITION_LABELS[disposition || "good"]}»`
    : `, con su consumo estimado y el reportado por la impresora, como «${DISPOSITION_LABELS[disposition || "good"]}»`;

  return (
    <div className="space-y-4">
      {result && (
        <div role="status" className="rounded-md border border-green-600/40 bg-green-50 p-3 text-sm text-green-800 dark:bg-green-950/30 dark:text-green-300">
          {result.marked_completed ? (
            <p>
              Impresión marcada como terminada. Se registraron {result.created}{" "}
              {result.created === 1 ? "registro" : "registros"} de consumo
              {result.material_recorded ? ", incluido el material pesado" : ""}.
            </p>
          ) : (
            <p>
              Esta impresión ya estaba terminada: no se registró nada nuevo. Para cambiar la
              cantidad, corrige su consumo en la lista de abajo.
            </p>
          )}
        </div>
      )}
      <form onSubmit={submit} noValidate className="space-y-4" aria-label="Marcar impresión terminada">
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-job`}>Identificador de la impresión</Label>
          <Input
            id={`${id}-job`}
            value={printJobId}
            onChange={(e) => setPrintJobId(e.target.value)}
            className="max-w-md font-mono"
            autoComplete="off"
            spellCheck={false}
            aria-describedby={`${id}-job-hint`}
          />
          <p id={`${id}-job-hint`} className="text-xs text-muted-foreground">
            Pravara aún no lista las impresiones en pantalla: copia el identificador de la orden de
            trabajo o del reporte de la impresora.
          </p>
        </div>
        <DispositionChoice name={`${id}-disp`} value={disposition} onChange={setDisposition} invalid={touched && !disposition} />
        <div className="flex items-start gap-2">
          <Checkbox id={`${id}-weighed`} checked={weighed} onCheckedChange={(v) => setWeighed(v === true)} className="mt-0.5" />
          <Label htmlFor={`${id}-weighed`}>Pesé o medí el material usado</Label>
        </div>
        {weighed && (
          <div className="grid gap-3 sm:grid-cols-[8rem_8rem_1fr]">
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-qty`}>Cantidad real</Label>
              <Input id={`${id}-qty`} value={quantity} onChange={(e) => setQuantity(e.target.value)} inputMode="decimal" className="tabular-nums" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-unit`}>Unidad</Label>
              <select
                id={`${id}-unit`}
                value={unit}
                onChange={(e) => setUnit(e.target.value)}
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              >
                {CONSUMPTION_MATERIAL_UNITS.map((u) => (
                  <option key={u} value={u}>
                    {u}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-code`}>Código de material (opcional)</Label>
              <Input id={`${id}-code`} value={code} onChange={(e) => setCode(e.target.value)} className="font-mono" autoComplete="off" spellCheck={false} aria-describedby={`${id}-code-hint`} />
              <p id={`${id}-code-hint`} className="text-xs text-muted-foreground">
                Sin código se usa el material del perfil de impresión.
              </p>
            </div>
          </div>
        )}
        {touched && problems.length > 0 && (
          <ul role="alert" className="text-xs text-red-700 dark:text-red-400">
            {problems.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        )}
        <Button type="submit">Revisar y marcar terminada</Button>
      </form>
      <ConfirmRecordDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Marcar impresión terminada"
        description={`Se marcará la impresión ${printJobId.trim()} como terminada (100 %)${what}. No se envía nada a ninguna máquina.`}
        irreversible="Una vez terminada, otra cantidad solo se registra con una corrección, que conserva el registro anterior."
        confirmLabel="Marcar terminada"
        onConfirm={() => mutation.mutate()}
        isPending={mutation.isPending}
        error={mutation.error}
        forbiddenAction="marcar impresiones terminadas"
        managerOnlyAction={false}
      />
    </div>
  );
}
