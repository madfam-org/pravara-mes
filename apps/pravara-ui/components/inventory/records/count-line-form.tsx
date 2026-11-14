"use client";

import { useId, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { countsAPI, type CountLine, type Material } from "@/lib/inventory-records-api";
import {
  COMMON_UNITS,
  formatQty,
  isValidUnit,
  localInputToRfc3339,
  normalizeUnit,
  nowLocalInput,
  parseQuantity,
} from "@/lib/inventory-records";
import { WriteErrorMessage } from "./states";
import { RECORD_KEYS } from "./use-records";

/**
 * Records one counted quantity on an open count. Posting the same material
 * again replaces its line while the count is open, so no confirmation here:
 * nothing moves until the count is closed.
 */
export function CountLineForm({
  token,
  countId,
  materials,
  lines,
}: {
  token: string;
  countId: string;
  materials: Material[];
  lines: CountLine[];
}) {
  const id = useId();
  const queryClient = useQueryClient();
  const [materialId, setMaterialId] = useState("");
  const [quantity, setQuantity] = useState("");
  const [unit, setUnit] = useState("");
  const [countedAt, setCountedAt] = useState("");
  const [touched, setTouched] = useState(false);
  const [saved, setSaved] = useState<CountLine | null>(null);

  const material = materials.find((m) => m.id === materialId);
  const existing = lines.find((l) => l.material_id === materialId);
  const qty = parseQuantity(quantity);
  const countedAtIso = countedAt ? localInputToRfc3339(countedAt) : undefined;

  const problems: string[] = [];
  if (!material) problems.push("Elige el material contado.");
  if (qty === null || qty < 0) problems.push("Escribe la cantidad contada (0 o más, hasta 4 decimales).");
  if (!isValidUnit(unit)) problems.push("Escribe la unidad en que contaste.");
  if (countedAt && !countedAtIso) problems.push("La fecha y hora del conteo no es válida.");
  if (countedAtIso && new Date(countedAtIso).getTime() > Date.now() + 60_000)
    problems.push("La fecha del conteo no puede estar en el futuro.");

  const mutation = useMutation({
    mutationFn: () =>
      countsAPI.recordLine(token, countId, {
        material_id: materialId,
        counted_quantity: qty as number,
        unit: normalizeUnit(unit),
        counted_at: countedAtIso ?? undefined,
      }),
    onSuccess: (line) => {
      setSaved(line);
      setMaterialId("");
      setQuantity("");
      setUnit("");
      setCountedAt("");
      setTouched(false);
      queryClient.invalidateQueries({ queryKey: RECORD_KEYS.counts });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    setSaved(null);
    if (problems.length > 0 || mutation.isPending) return;
    mutation.mutate();
  };

  return (
    <form onSubmit={submit} noValidate className="space-y-3" aria-label="Registrar cantidad contada">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-[2fr_1fr_1fr_1.5fr_auto] lg:items-end">
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-material`}>Material</Label>
          <Select
            value={materialId}
            onValueChange={(v) => {
              setMaterialId(v);
              const m = materials.find((x) => x.id === v);
              if (m) setUnit(m.stock_unit ?? m.base_unit);
            }}
          >
            <SelectTrigger id={`${id}-material`}>
              <SelectValue placeholder="Elige el material" />
            </SelectTrigger>
            <SelectContent>
              {materials.map((m) => (
                <SelectItem key={m.id} value={m.id}>
                  {m.code} · {m.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-qty`}>Cantidad contada</Label>
          <Input id={`${id}-qty`} value={quantity} onChange={(e) => setQuantity(e.target.value)} inputMode="decimal" className="tabular-nums" />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-unit`}>Unidad</Label>
          <Input id={`${id}-unit`} value={unit} onChange={(e) => setUnit(e.target.value)} list={`${id}-units`} className="font-mono" autoComplete="off" spellCheck={false} />
          <datalist id={`${id}-units`}>
            {COMMON_UNITS.map((u) => (
              <option key={u} value={u} />
            ))}
          </datalist>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-at`}>Contado el (opcional)</Label>
          <Input id={`${id}-at`} type="datetime-local" value={countedAt} max={nowLocalInput()} onChange={(e) => setCountedAt(e.target.value)} />
        </div>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? "Guardando…" : existing ? "Reemplazar" : "Guardar"}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Sin fecha, cuenta como contado ahora.
        {existing &&
          ` Ya tiene ${formatQty(existing.counted_quantity)} ${existing.unit}: guardar reemplaza esa línea.`}
      </p>
      {touched && problems.length > 0 && (
        <ul role="alert" className="text-xs text-red-700 dark:text-red-400">
          {problems.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      )}
      {saved && (
        <p role="status" className="text-sm text-green-800 dark:text-green-300">
          Guardado: {saved.material_code}, {formatQty(saved.counted_quantity)} {saved.unit}.
        </p>
      )}
      <WriteErrorMessage error={mutation.error} forbiddenAction="registrar conteos" />
    </form>
  );
}
