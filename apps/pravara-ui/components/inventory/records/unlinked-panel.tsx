"use client";

import { useId, useState } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  materialsAPI,
  type Material,
  type UnlinkedItem,
  type UnresolvedCode,
} from "@/lib/inventory-records-api";
import {
  UNLINKED_REASON_LABELS,
  formatDateTime,
  label,
  managerOnly,
  retryRead,
} from "@/lib/inventory-records";
import { ConfirmRecordDialog } from "./confirm-record-dialog";
import { LoadingRows, ReadErrorState, WriteErrorMessage } from "./states";
import { RECORD_KEYS, useCanManageInventory, useMaterialOptions } from "./use-records";

type Part = "items" | "codes" | "lots";

const th = "p-3 text-xs font-medium text-muted-foreground";

function MaterialPicker({
  materials,
  value,
  onChange,
  labelText,
}: {
  materials: Material[];
  value: string;
  onChange: (v: string) => void;
  labelText: string;
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger aria-label={labelText} className="h-9 w-full min-w-48 text-xs sm:w-56">
        <SelectValue placeholder="Elige un material" />
      </SelectTrigger>
      <SelectContent>
        {materials.map((m) => (
          <SelectItem key={m.id} value={m.id}>
            {m.code} · {m.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function LinkItemRow({
  item,
  token,
  candidates,
  canManage,
}: {
  item: UnlinkedItem;
  token: string;
  candidates: Material[];
  canManage: boolean;
}) {
  const queryClient = useQueryClient();
  const [materialId, setMaterialId] = useState("");
  const mutation = useMutation({
    mutationFn: () => materialsAPI.linkStockItem(token, item.id, materialId),
    onSuccess: () => {
      for (const key of [RECORD_KEYS.unlinked, RECORD_KEYS.materials, RECORD_KEYS.coverage, RECORD_KEYS.stock]) {
        queryClient.invalidateQueries({ queryKey: key });
      }
    },
  });
  return (
    <tr className="border-b align-top">
      <td className="p-3 font-mono text-sm">{item.sku}</td>
      <td className="p-3 text-sm">{item.name}</td>
      <td className="p-3 font-mono text-sm text-muted-foreground">{item.unit}</td>
      <td className="p-3 text-sm text-muted-foreground">{label(UNLINKED_REASON_LABELS, item.reason)}</td>
      <td className="p-3">
        {!canManage ? (
          <p className="text-xs text-muted-foreground">{managerOnly("vincular artículos")}</p>
        ) : candidates.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            Todos los materiales ya tienen artículo.
          </p>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <MaterialPicker
              materials={candidates}
              value={materialId}
              onChange={setMaterialId}
              labelText={`Material para vincular ${item.sku}`}
            />
            <Button
              size="sm"
              variant="outline"
              disabled={!materialId || mutation.isPending}
              onClick={() => mutation.mutate()}
            >
              {mutation.isPending ? "Vinculando…" : "Vincular"}
            </Button>
          </div>
        )}
        {canManage && (
          <Button size="sm" variant="link" asChild className="mt-1 px-0 underline">
            <Link href={`/inventory/materials?nuevo=${encodeURIComponent(item.sku)}&articulo=${encodeURIComponent(item.id)}`}>
              Registrar como material nuevo
            </Link>
          </Button>
        )}
        <div className="mt-2">
          <WriteErrorMessage error={mutation.error} forbiddenAction="vincular artículos" />
        </div>
      </td>
    </tr>
  );
}

function CodeRow({
  code,
  token,
  materials,
  canManage,
}: {
  code: UnresolvedCode;
  token: string;
  materials: Material[];
  canManage: boolean;
}) {
  const queryClient = useQueryClient();
  const [materialId, setMaterialId] = useState("");
  const [confirming, setConfirming] = useState(false);
  const target = materials.find((m) => m.id === materialId);
  const mutation = useMutation({
    mutationFn: () => materialsAPI.addAlias(token, materialId, code.material_code),
    onSuccess: () => {
      setConfirming(false);
      for (const key of [RECORD_KEYS.unlinked, RECORD_KEYS.materials, RECORD_KEYS.coverage]) {
        queryClient.invalidateQueries({ queryKey: key });
      }
    },
  });
  return (
    <tr className="border-b align-top">
      <td className="p-3 font-mono text-sm">{code.material_code}</td>
      <td className="p-3 text-right text-sm tabular-nums">{code.rows}</td>
      <td className="p-3 font-mono text-sm text-muted-foreground">{code.units.join(", ")}</td>
      <td className="p-3 text-sm text-muted-foreground">{formatDateTime(code.last_occurred_at)}</td>
      <td className="p-3">
        {!canManage ? (
          <p className="text-xs text-muted-foreground">{managerOnly("cambiar el registro de materiales")}</p>
        ) : (
        <div className="flex flex-wrap items-center gap-2">
          <MaterialPicker
            materials={materials}
            value={materialId}
            onChange={setMaterialId}
            labelText={`Material del que ${code.material_code} es alias`}
          />
          <Button size="sm" variant="outline" disabled={!materialId} onClick={() => setConfirming(true)}>
            Agregar como alias
          </Button>
          <Button size="sm" variant="link" asChild className="px-0 underline">
            <Link href={`/inventory/materials?nuevo=${encodeURIComponent(code.material_code)}`}>
              Registrar como material nuevo
            </Link>
          </Button>
        </div>
        )}
      </td>
      <ConfirmRecordDialog
        open={confirming}
        onOpenChange={(o) => {
          setConfirming(o);
          if (!o) mutation.reset();
        }}
        title="Agregar alias"
        description={`Se registrará «${code.material_code}» como alias de ${target?.code ?? ""} (${target?.name ?? ""}). Sus ${code.rows} registros de consumo contarán con código de registro.`}
        irreversible="Los alias no se pueden quitar. El stock ya descontado no se recalcula."
        confirmLabel="Agregar alias"
        onConfirm={() => mutation.mutate()}
        isPending={mutation.isPending}
        error={mutation.error}
        forbiddenAction="cambiar el registro de materiales"
      />
    </tr>
  );
}

/**
 * What is not linked to the registry yet: stock items, consumption codes and
 * batch lots (GET /v1/inventory/materials/unlinked), each with its way out.
 */
export function UnlinkedPanel({ token, parts }: { token: string; parts: Part[] }) {
  const headingId = useId();
  const query = useQuery({
    queryKey: RECORD_KEYS.unlinked,
    queryFn: () => materialsAPI.unlinked(token),
    enabled: !!token,
    retry: retryRead,
  });
  const canManage = useCanManageInventory();
  const materialsQuery = useMaterialOptions(token);
  const materials = materialsQuery.data?.data ?? [];
  const candidates = materials.filter((m) => !m.inventory_item_id);

  if (query.isPending) return <LoadingRows rows={3} label="lo que falta vincular" />;
  if (query.isError)
    return (
      <ReadErrorState
        what="lo que falta vincular"
        error={query.error}
        onRetry={() => query.refetch()}
        isRetrying={query.isFetching}
      />
    );
  const data = query.data ?? { items: [], lots: [], consumption_codes: [] };
  const nothing =
    (!parts.includes("items") || data.items.length === 0) &&
    (!parts.includes("codes") || data.consumption_codes.length === 0) &&
    (!parts.includes("lots") || data.lots.length === 0);

  return (
    <section aria-labelledby={headingId} className="space-y-4">
      <h2 id={headingId} className="text-xl font-semibold">
        Sin vincular al registro
      </h2>
      {nothing && (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <CheckCircle2 className="h-4 w-4 text-green-700 dark:text-green-400" aria-hidden="true" />
          Todo está vinculado a un material del registro.
        </p>
      )}
      {parts.includes("items") && data.items.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Artículos de existencias sin material ({data.items.length})</CardTitle>
            <p className="text-sm text-muted-foreground">
              Sus entradas y conteos no se pueden registrar hasta vincularlos a un material.
            </p>
          </CardHeader>
          <CardContent className="overflow-x-auto p-0">
            <table className="w-full text-left">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th scope="col" className={th}>SKU</th>
                  <th scope="col" className={th}>Nombre</th>
                  <th scope="col" className={th}>Unidad</th>
                  <th scope="col" className={th}>Por qué no se vinculó</th>
                  <th scope="col" className={th}>Vincular a</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((item) => (
                  <LinkItemRow key={item.id} item={item} token={token} candidates={candidates} canManage={canManage} />
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
      {parts.includes("codes") && data.consumption_codes.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">
              Códigos de consumo sin material ({data.consumption_codes.length})
            </CardTitle>
            <p className="text-sm text-muted-foreground">
              Consumo registrado con un nombre que el registro no reconoce. Agrégalo como alias de un
              material existente o regístralo como material nuevo.
            </p>
          </CardHeader>
          <CardContent className="overflow-x-auto p-0">
            <table className="w-full text-left">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th scope="col" className={th}>Código</th>
                  <th scope="col" className={`${th} text-right`}>Registros</th>
                  <th scope="col" className={th}>Unidades</th>
                  <th scope="col" className={th}>Último</th>
                  <th scope="col" className={th}>Resolver</th>
                </tr>
              </thead>
              <tbody>
                {data.consumption_codes.map((code) => (
                  <CodeRow key={code.material_code} code={code} token={token} materials={materials} canManage={canManage} />
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
      {parts.includes("lots") && data.lots.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Lotes sin artículo de existencias ({data.lots.length})</CardTitle>
            <p className="text-sm text-muted-foreground">
              Un lote se vincula al registrar una entrada que lo nombra. Si ese material ya llegó y
              no se registró, registra la entrada con este número de lote.
            </p>
          </CardHeader>
          <CardContent className="overflow-x-auto p-0">
            <table className="w-full text-left">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th scope="col" className={th}>Lote</th>
                  <th scope="col" className={th}>Código de producto</th>
                  <th scope="col" className={th}>Producto</th>
                </tr>
              </thead>
              <tbody>
                {data.lots.map((lot) => (
                  <tr key={lot.id} className="border-b">
                    <td className="p-3 font-mono text-sm">{lot.lot_number}</td>
                    <td className="p-3 font-mono text-sm text-muted-foreground">{lot.product_code || "—"}</td>
                    <td className="p-3 text-sm">{lot.product_name}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
      {[data.items, data.lots, data.consumption_codes].some((l) => l.length >= 500) && (
        <p className="text-sm text-muted-foreground">
          La lista muestra hasta 500 de cada tipo; al resolver estos aparecerán los siguientes.
        </p>
      )}
    </section>
  );
}
