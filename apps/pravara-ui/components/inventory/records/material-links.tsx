"use client";

import { useId, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { materialsAPI, type AddConversionResult, type MaterialDetail } from "@/lib/inventory-records-api";
import {
  COMMON_UNITS,
  formatDateTime,
  formatQty,
  isValidUnit,
  managerOnly,
  normalizeUnit,
  parseQuantity,
  retryRead,
} from "@/lib/inventory-records";
import { ConfirmRecordDialog } from "./confirm-record-dialog";
import { WriteErrorMessage } from "./states";
import { RECORD_KEYS } from "./use-records";

const REGISTRY = "cambiar el registro de materiales";

function useRefreshMaterial(id: string) {
  const queryClient = useQueryClient();
  return () => {
    for (const key of [RECORD_KEYS.materials, RECORD_KEYS.unlinked, RECORD_KEYS.coverage, RECORD_KEYS.gaps, RECORD_KEYS.stock]) {
      queryClient.invalidateQueries({ queryKey: key });
    }
    queryClient.invalidateQueries({ queryKey: [...RECORD_KEYS.materials, "detail", id] });
  };
}

/** The stock item this material is counted on, or a way to link one. */
export function StockLinkCard({ material, token, canManage }: { material: MaterialDetail; token: string; canManage: boolean }) {
  const refresh = useRefreshMaterial(material.id);
  const [itemId, setItemId] = useState("");
  const unlinked = useQuery({
    queryKey: RECORD_KEYS.unlinked,
    queryFn: () => materialsAPI.unlinked(token),
    enabled: !!token && !material.inventory_item_id && canManage,
    retry: retryRead,
  });
  const mutation = useMutation({
    mutationFn: () => materialsAPI.linkStockItem(token, itemId, material.id),
    onSuccess: refresh,
  });
  const items = unlinked.data?.items ?? [];

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Existencias</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        {material.inventory_item_id ? (
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
            <dt className="text-muted-foreground">Artículo (SKU)</dt>
            <dd className="font-mono">{material.stock_sku}</dd>
            <dt className="text-muted-foreground">En existencia</dt>
            <dd className="tabular-nums">
              {formatQty(material.quantity_on_hand)} {material.stock_unit}
            </dd>
          </dl>
        ) : (
          <>
            <p>
              No tiene artículo de existencias vinculado: sus entradas y conteos no se pueden
              registrar hasta vincular uno.
            </p>
            {!canManage ? (
              <p className="text-muted-foreground">{managerOnly("vincular artículos")}</p>
            ) : unlinked.isError ? (
              <WriteErrorMessage error={unlinked.error} />
            ) : items.length === 0 ? (
              <p className="text-muted-foreground">
                {unlinked.isPending ? "Buscando artículos sin vincular…" : "No hay artículos de existencias sin vincular."}
              </p>
            ) : (
              <div className="flex flex-wrap items-center gap-2">
                <Select value={itemId} onValueChange={setItemId}>
                  <SelectTrigger aria-label="Artículo para vincular" className="h-9 w-full sm:w-72">
                    <SelectValue placeholder="Elige un artículo" />
                  </SelectTrigger>
                  <SelectContent>
                    {items.map((i) => (
                      <SelectItem key={i.id} value={i.id}>
                        {i.sku} · {i.name} ({i.unit})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button variant="outline" size="sm" disabled={!itemId || mutation.isPending} onClick={() => mutation.mutate()}>
                  {mutation.isPending ? "Vinculando…" : "Vincular"}
                </Button>
              </div>
            )}
            <WriteErrorMessage error={mutation.error} forbiddenAction="vincular artículos" />
          </>
        )}
      </CardContent>
    </Card>
  );
}

export function AliasesCard({ material, token, canManage }: { material: MaterialDetail; token: string; canManage: boolean }) {
  const id = useId();
  const refresh = useRefreshMaterial(material.id);
  const [alias, setAlias] = useState("");
  const [confirming, setConfirming] = useState(false);
  const trimmed = alias.trim();
  const mutation = useMutation({
    mutationFn: () => materialsAPI.addAlias(token, material.id, trimmed),
    onSuccess: () => {
      setConfirming(false);
      setAlias("");
      refresh();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (trimmed.length < 1 || trimmed.length > 100) return;
    mutation.reset();
    setConfirming(true);
  };

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Alias</CardTitle>
        <p className="text-sm text-muted-foreground">
          Otros nombres con que llega este material: el tipo de material de un perfil de impresión
          (por ejemplo «PLA») o el código de producto de un lote.
        </p>
      </CardHeader>
      <CardContent className="space-y-3">
        {material.aliases.length === 0 ? (
          <p className="text-sm text-muted-foreground">Sin alias.</p>
        ) : (
          <ul className="flex flex-wrap gap-2" aria-label="Alias registrados">
            {material.aliases.map((a) => (
              <li key={a.id} className="rounded-md border px-2 py-1 font-mono text-sm" title={`Agregado ${formatDateTime(a.created_at)}`}>
                {a.alias}
              </li>
            ))}
          </ul>
        )}
        {canManage ? (
          <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-alias`}>Nuevo alias</Label>
              <Input id={`${id}-alias`} value={alias} maxLength={100} onChange={(e) => setAlias(e.target.value)} className="w-56 font-mono" autoComplete="off" />
            </div>
            <Button type="submit" variant="outline" disabled={!trimmed}>
              Agregar alias
            </Button>
          </form>
        ) : (
          <p className="text-sm text-muted-foreground">{managerOnly("agregar alias")}</p>
        )}
      </CardContent>
      <ConfirmRecordDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Agregar alias"
        description={`Se registrará «${trimmed}» como alias de ${material.code}. El consumo con ese nombre se atribuirá a este material, también el ya registrado al calcular la cobertura.`}
        irreversible="Los alias no se pueden quitar. El stock ya descontado no se recalcula."
        confirmLabel="Agregar alias"
        onConfirm={() => mutation.mutate()}
        isPending={mutation.isPending}
        error={mutation.error}
        forbiddenAction={REGISTRY}
      />
    </Card>
  );
}

export function ConversionsCard({ material, token, canManage }: { material: MaterialDetail; token: string; canManage: boolean }) {
  const id = useId();
  const refresh = useRefreshMaterial(material.id);
  const [fromUnit, setFromUnit] = useState("");
  const [factor, setFactor] = useState("");
  const [toUnit, setToUnit] = useState(material.stock_unit ?? material.base_unit);
  const [note, setNote] = useState("");
  const [touched, setTouched] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [result, setResult] = useState<AddConversionResult | null>(null);

  const factorValue = parseQuantity(factor);
  const problems: string[] = [];
  if (!isValidUnit(fromUnit)) problems.push("Escribe la unidad de origen (por ejemplo spool).");
  if (!isValidUnit(toUnit)) problems.push("Escribe la unidad de destino (por ejemplo g).");
  if (factorValue === null || factorValue <= 0) problems.push("El factor debe ser un número mayor que cero, hasta 4 decimales.");
  if (isValidUnit(fromUnit) && normalizeUnit(fromUnit) === normalizeUnit(toUnit)) problems.push("Las dos unidades deben ser distintas.");

  const mutation = useMutation({
    mutationFn: () =>
      materialsAPI.addConversion(token, material.id, {
        from_unit: normalizeUnit(fromUnit),
        to_unit: normalizeUnit(toUnit),
        factor: factorValue as number,
        note: note.trim() || undefined,
      }),
    onSuccess: (res) => {
      setConfirming(false);
      setResult(res);
      setFromUnit("");
      setFactor("");
      setNote("");
      setTouched(false);
      refresh();
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (problems.length > 0) return;
    mutation.reset();
    setResult(null);
    setConfirming(true);
  };

  const sentence = `Se registrará 1 ${normalizeUnit(fromUnit)} = ${formatQty(factorValue ?? 0)} ${normalizeUnit(toUnit)} para ${material.code}. Lo pendiente por falta de esta conversión se registrará en existencias en el mismo momento.`;

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Conversiones de unidad</CardTitle>
        <p className="text-sm text-muted-foreground">
          Incluidas siempre: misma unidad, kg↔g, l↔ml, m↔mm, cm↔mm. Agrega las propias del material,
          como 1 spool = 1000 g.
        </p>
      </CardHeader>
      <CardContent className="space-y-3">
        {material.conversions.length === 0 ? (
          <p className="text-sm text-muted-foreground">Sin conversiones propias.</p>
        ) : (
          <ul className="space-y-1 text-sm" aria-label="Conversiones registradas">
            {material.conversions.map((c) => (
              <li key={c.id} className="tabular-nums">
                <span className="font-mono">
                  1 {c.from_unit} = {formatQty(c.factor)} {c.to_unit}
                </span>
                {c.note && <span className="text-muted-foreground"> · {c.note}</span>}
              </li>
            ))}
          </ul>
        )}
        {result && (
          <p role="status" className="rounded-md border border-green-600/40 bg-green-50 p-3 text-sm text-green-800 dark:bg-green-950/30 dark:text-green-300">
            Conversión registrada.{" "}
            {result.gaps_resolved > 0
              ? `Se registraron en existencias ${result.gaps_resolved} pendientes que esperaban esta conversión.`
              : "No había pendientes esperando esta conversión."}
          </p>
        )}
        {canManage ? (
          <form onSubmit={submit} noValidate className="space-y-2" aria-label="Nueva conversión">
            <div className="flex flex-wrap items-end gap-2">
              <span className="pb-2 text-sm">1</span>
              <div className="space-y-1.5">
                <Label htmlFor={`${id}-from`}>De</Label>
                <Input id={`${id}-from`} value={fromUnit} onChange={(e) => setFromUnit(e.target.value)} list={`${id}-units`} className="w-28 font-mono" autoComplete="off" spellCheck={false} />
              </div>
              <span className="pb-2 text-sm">=</span>
              <div className="space-y-1.5">
                <Label htmlFor={`${id}-factor`}>Factor</Label>
                <Input id={`${id}-factor`} value={factor} onChange={(e) => setFactor(e.target.value)} inputMode="decimal" className="w-28 tabular-nums" />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor={`${id}-to`}>A</Label>
                <Input id={`${id}-to`} value={toUnit} onChange={(e) => setToUnit(e.target.value)} list={`${id}-units`} className="w-28 font-mono" autoComplete="off" spellCheck={false} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor={`${id}-note`}>Nota (opcional)</Label>
                <Input id={`${id}-note`} value={note} onChange={(e) => setNote(e.target.value)} className="w-48" />
              </div>
              <Button type="submit" variant="outline">
                Agregar conversión
              </Button>
            </div>
            <datalist id={`${id}-units`}>
              {COMMON_UNITS.map((u) => (
                <option key={u} value={u} />
              ))}
            </datalist>
            {touched && problems.length > 0 && (
              <ul className="text-xs text-red-700 dark:text-red-400" role="alert">
                {problems.map((p) => (
                  <li key={p}>{p}</li>
                ))}
              </ul>
            )}
          </form>
        ) : (
          <p className="text-sm text-muted-foreground">{managerOnly("agregar conversiones")}</p>
        )}
      </CardContent>
      <ConfirmRecordDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Agregar conversión"
        description={sentence}
        irreversible="Las conversiones no se pueden cambiar ni borrar."
        confirmLabel="Agregar conversión"
        onConfirm={() => mutation.mutate()}
        isPending={mutation.isPending}
        error={mutation.error}
        forbiddenAction={REGISTRY}
      />
    </Card>
  );
}
