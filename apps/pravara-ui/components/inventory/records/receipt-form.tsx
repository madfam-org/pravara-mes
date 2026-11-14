"use client";

import { useId, useState, type FormEvent } from "react";
import Link from "next/link";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  ApiError,
  receiptsAPI,
  type CreateReceiptResult,
  type InvoiceStatus,
  type Material,
} from "@/lib/inventory-records-api";
import {
  COMMON_UNITS,
  formatDateTime,
  formatQty,
  isUuid,
  isValidUnit,
  localInputToRfc3339,
  normalizeUnit,
  nowLocalInput,
  parseQuantity,
} from "@/lib/inventory-records";
import { ConfirmRecordDialog } from "./confirm-record-dialog";
import { WriteErrorMessage } from "./states";
import { RECORD_KEYS, newIdempotencyKey } from "./use-records";

export interface ReceiptDraft {
  material?: Material;
  quantity: number;
  unit: string;
  receivedAtIso: string;
  lot: string;
  invoice: InvoiceStatus;
  cfdiUuid: string;
  cfdiLine: number | null;
}

/** The sentence the confirmation shows: exactly what will be recorded. */
export function describeReceipt(d: ReceiptDraft): string {
  const lot = d.lot ? `, lote ${d.lot}` : ", sin lote";
  const invoice =
    d.invoice === "invoiced"
      ? `, con factura CFDI ${d.cfdiUuid} línea ${d.cfdiLine}`
      : ", marcada como sin factura";
  return `Se registrará la entrada de ${formatQty(d.quantity)} ${d.unit} de ${d.material?.code ?? "?"} (${d.material?.name ?? ""}), recibida el ${formatDateTime(d.receivedAtIso)}${lot}${invoice}.`;
}

/** An idempotency key sent again with different data (409). */
function isKeyConflict(error: unknown): boolean {
  return error instanceof ApiError && error.code === "idempotency_key_reused";
}

export function ReceiptForm({ token, materials }: { token: string; materials: Material[] }) {
  const id = useId();
  const queryClient = useQueryClient();
  const [idempotencyKey, setIdempotencyKey] = useState(newIdempotencyKey);
  const [materialId, setMaterialId] = useState("");
  const [quantity, setQuantity] = useState("");
  const [unit, setUnit] = useState("");
  const [receivedAt, setReceivedAt] = useState(() => nowLocalInput());
  const [lot, setLot] = useState("");
  const [invoice, setInvoice] = useState<InvoiceStatus | "">("");
  const [cfdiUuid, setCfdiUuid] = useState("");
  const [cfdiLine, setCfdiLine] = useState("");
  const [notes, setNotes] = useState("");
  const [touched, setTouched] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [result, setResult] = useState<CreateReceiptResult | null>(null);

  const material = materials.find((m) => m.id === materialId);
  const qty = parseQuantity(quantity);
  const receivedAtIso = localInputToRfc3339(receivedAt);
  const line = /^\d+$/.test(cfdiLine.trim()) ? Number(cfdiLine.trim()) : null;

  const problems: Record<string, string> = {};
  if (!material) problems.material = "Elige el material que llegó.";
  if (qty === null || qty <= 0) problems.quantity = "Escribe una cantidad mayor que cero (hasta 4 decimales).";
  if (!isValidUnit(unit)) problems.unit = "Escribe la unidad (g, kg, spool, pcs).";
  if (!receivedAtIso) problems.receivedAt = "Escribe la fecha y hora en que llegó.";
  else if (new Date(receivedAtIso).getTime() > Date.now() + 60_000)
    problems.receivedAt = "La fecha no puede estar en el futuro.";
  if (!invoice) problems.invoice = "Indica si la entrada tiene factura o no. No se puede omitir.";
  if (invoice === "invoiced") {
    if (!isUuid(cfdiUuid)) problems.cfdiUuid = "Escribe el UUID (folio fiscal) del CFDI, con guiones.";
    if (line === null || line < 1) problems.cfdiLine = "Escribe el número de línea del concepto en el CFDI (1 o más).";
  }
  const valid = Object.keys(problems).length === 0;

  const draft: ReceiptDraft = {
    material,
    quantity: qty ?? 0,
    unit: normalizeUnit(unit),
    receivedAtIso: receivedAtIso ?? "",
    lot: lot.trim(),
    invoice: (invoice || "no_invoice") as InvoiceStatus,
    cfdiUuid: cfdiUuid.trim().toLowerCase(),
    cfdiLine: line,
  };

  const mutation = useMutation({
    mutationFn: () =>
      receiptsAPI.create(token, {
        material_id: materialId,
        quantity: qty as number,
        unit: draft.unit,
        received_at: draft.receivedAtIso,
        invoice_status: draft.invoice,
        ...(draft.invoice === "invoiced" ? { cfdi_uuid: draft.cfdiUuid, cfdi_line: line as number } : {}),
        lot_number: draft.lot || undefined,
        notes: notes.trim() || undefined,
        idempotency_key: idempotencyKey,
      }),
    onSuccess: (res) => {
      setConfirming(false);
      setResult(res);
      for (const key of [RECORD_KEYS.receipts, RECORD_KEYS.materials, RECORD_KEYS.gaps, RECORD_KEYS.unlinked, RECORD_KEYS.stock]) {
        queryClient.invalidateQueries({ queryKey: key });
      }
      startNew(false);
    },
  });

  function startNew(clearResult = true) {
    setIdempotencyKey(newIdempotencyKey());
    setMaterialId("");
    setQuantity("");
    setUnit("");
    setReceivedAt(nowLocalInput());
    setLot("");
    setInvoice("");
    setCfdiUuid("");
    setCfdiLine("");
    setNotes("");
    setTouched(false);
    mutation.reset();
    if (clearResult) setResult(null);
  }

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!valid) return;
    setResult(null);
    setConfirming(true);
  };

  const err = (key: string) =>
    touched && problems[key] ? (
      <p id={`${id}-${key}-err`} className="text-xs text-red-700 dark:text-red-400">
        {problems[key]}
      </p>
    ) : null;
  const inv = (key: string) => ({
    "aria-invalid": touched && !!problems[key],
    "aria-describedby": touched && problems[key] ? `${id}-${key}-err` : undefined,
  });

  return (
    <div className="space-y-4">
      {result && (
        <div role="status" className="rounded-md border border-green-600/40 bg-green-50 p-3 text-sm text-green-800 dark:bg-green-950/30 dark:text-green-300">
          {result.replayed ? (
            <p>Esta entrada ya estaba registrada con este mismo envío; no se duplicó.</p>
          ) : result.receipt.status === "posted" ? (
            <p>
              Entrada registrada: {formatQty(result.receipt.quantity)} {result.receipt.unit} de{" "}
              {result.receipt.material_code}
              {result.receipt.stock_quantity !== undefined &&
                ` (${formatQty(result.receipt.stock_quantity)} en la unidad de existencias)`}
              .
            </p>
          ) : (
            <p>
              Entrada guardada, pero <strong>no movió existencias</strong>: {result.receipt.material_code} no
              tiene conversión de {result.receipt.unit} a su unidad de existencias. Agrega la conversión en{" "}
              <Link className="underline underline-offset-4" href={`/inventory/materials/${result.receipt.material_id}`}>
                el material
              </Link>{" "}
              y se registrará en ese momento.
            </p>
          )}
        </div>
      )}
      <form onSubmit={submit} noValidate className="space-y-4" aria-label="Registrar entrada">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor={`${id}-material`}>Material</Label>
            <Select
              value={materialId}
              onValueChange={(v) => {
                setMaterialId(v);
                const m = materials.find((x) => x.id === v);
                if (m && !unit) setUnit(m.stock_unit ?? m.base_unit);
              }}
            >
              <SelectTrigger id={`${id}-material`} {...inv("material")}>
                <SelectValue placeholder="Elige el material" />
              </SelectTrigger>
              <SelectContent>
                {materials.map((m) => (
                  <SelectItem key={m.id} value={m.id} disabled={!m.inventory_item_id}>
                    {m.code} · {m.name}
                    {!m.inventory_item_id && " (sin artículo de existencias)"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {err("material")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-qty`}>Cantidad</Label>
            <Input id={`${id}-qty`} value={quantity} onChange={(e) => setQuantity(e.target.value)} inputMode="decimal" className="tabular-nums" {...inv("quantity")} />
            {err("quantity")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-unit`}>Unidad</Label>
            <Input id={`${id}-unit`} value={unit} onChange={(e) => setUnit(e.target.value)} list={`${id}-units`} className="font-mono" autoComplete="off" spellCheck={false} {...inv("unit")} />
            <datalist id={`${id}-units`}>
              {COMMON_UNITS.map((u) => (
                <option key={u} value={u} />
              ))}
            </datalist>
            {material?.stock_unit && (
              <p className="text-xs text-muted-foreground">Existencias en {material.stock_unit}.</p>
            )}
            {err("unit")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-at`}>Recibida el</Label>
            <Input id={`${id}-at`} type="datetime-local" value={receivedAt} max={nowLocalInput()} onChange={(e) => setReceivedAt(e.target.value)} {...inv("receivedAt")} />
            {err("receivedAt")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-lot`}>Lote (opcional)</Label>
            <Input id={`${id}-lot`} value={lot} onChange={(e) => setLot(e.target.value)} className="font-mono" autoComplete="off" />
          </div>
        </div>

        <fieldset className="space-y-3 border-t pt-4" aria-describedby={touched && problems.invoice ? `${id}-invoice-err` : undefined}>
          <legend className="pr-2 text-sm font-medium">Factura</legend>
          <div className="flex flex-col gap-2 sm:flex-row sm:gap-6">
            {(
              [
                ["invoiced", "Con factura (CFDI)"],
                ["no_invoice", "Sin factura"],
              ] as const
            ).map(([value, text]) => (
              <label key={value} className="flex min-h-9 items-center gap-2 text-sm">
                <input
                  type="radio"
                  name={`${id}-invoice`}
                  value={value}
                  checked={invoice === value}
                  onChange={() => setInvoice(value)}
                  className="h-4 w-4 accent-primary"
                />
                {text}
              </label>
            ))}
          </div>
          {err("invoice")}
          {invoice === "invoiced" && (
            <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
              <div className="space-y-1.5">
                <Label htmlFor={`${id}-uuid`}>UUID del CFDI</Label>
                <Input id={`${id}-uuid`} value={cfdiUuid} onChange={(e) => setCfdiUuid(e.target.value)} className="font-mono" autoComplete="off" spellCheck={false} {...inv("cfdiUuid")} />
                {err("cfdiUuid")}
              </div>
              <div className="space-y-1.5">
                <Label htmlFor={`${id}-line`}>Línea</Label>
                <Input id={`${id}-line`} value={cfdiLine} onChange={(e) => setCfdiLine(e.target.value)} inputMode="numeric" className="tabular-nums" {...inv("cfdiLine")} />
                {err("cfdiLine")}
              </div>
            </div>
          )}
          {invoice === "no_invoice" && (
            <p className="text-sm text-muted-foreground">
              La entrada quedará marcada explícitamente como sin factura.
            </p>
          )}
        </fieldset>

        <div className="space-y-1.5">
          <Label htmlFor={`${id}-notes`}>Notas (opcional)</Label>
          <Textarea id={`${id}-notes`} rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} />
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="submit">Revisar y registrar</Button>
          <Button type="button" variant="ghost" onClick={() => startNew()}>
            Limpiar
          </Button>
        </div>
      </form>

      <ConfirmRecordDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Registrar entrada"
        description={describeReceipt(draft)}
        irreversible="Las entradas no se pueden editar ni anular: un error se corrige con un conteo físico."
        confirmLabel="Registrar entrada"
        onConfirm={() => mutation.mutate()}
        isPending={mutation.isPending}
        error={mutation.error}
        forbiddenAction="registrar entradas"
      />
      {isKeyConflict(mutation.error) && !confirming && (
        <WriteErrorMessage
          error={mutation.error}
          extra={
            <div className="mt-2 space-y-2">
              <p>Revisa la lista de entradas: la primera versión pudo haberse registrado.</p>
              <Button size="sm" variant="outline" onClick={() => startNew()}>
                Empezar una entrada nueva
              </Button>
            </div>
          }
        />
      )}
    </div>
  );
}
