"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError, countsAPI, type NewerCountMaterial } from "@/lib/inventory-records-api";
import { formatDateTime, managerOnly, retryRead } from "@/lib/inventory-records";
import { InventorySectionNav } from "@/components/inventory/records/section-nav";
import { ConfirmRecordDialog } from "@/components/inventory/records/confirm-record-dialog";
import { LoadingRows, ReadErrorState } from "@/components/inventory/records/states";
import { CountLineForm } from "@/components/inventory/records/count-line-form";
import { CountLinesTable, describeClose } from "@/components/inventory/records/count-lines";
import {
  RECORD_KEYS,
  useAccessToken,
  useCanManageInventory,
  useMaterialOptions,
} from "@/components/inventory/records/use-records";

export default function CountDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? "";
  const token = useAccessToken();
  const canManage = useCanManageInventory();
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [alreadyClosed, setAlreadyClosed] = useState(false);

  const query = useQuery({
    queryKey: [...RECORD_KEYS.counts, "detail", id],
    queryFn: () => countsAPI.get(token, id),
    enabled: !!token && !!id,
    retry: retryRead,
  });
  const materialsQuery = useMaterialOptions(token, { activeOnly: true });
  const materials = useMemo(() => materialsQuery.data?.data ?? [], [materialsQuery.data]);
  const materialsById = useMemo(() => new Map(materials.map((m) => [m.id, m])), [materials]);

  const count = query.data;
  const lines = count?.lines ?? [];
  const open = count?.status === "open";
  const countedIds = new Set(lines.map((l) => l.material_id));
  const uncounted = materials.filter((m) => m.inventory_item_id && !countedIds.has(m.id));

  const close = useMutation({
    mutationFn: () => countsAPI.close(token, id),
    onSuccess: (res) => {
      setConfirming(false);
      setAlreadyClosed(res.already_closed);
      for (const key of [RECORD_KEYS.counts, RECORD_KEYS.materials, RECORD_KEYS.gaps, RECORD_KEYS.stock]) {
        queryClient.invalidateQueries({ queryKey: key });
      }
    },
  });

  const newer: NewerCountMaterial[] =
    close.error instanceof ApiError && close.error.code === "newer_count_exists" && Array.isArray(close.error.body.materials)
      ? (close.error.body.materials as NewerCountMaterial[])
      : [];

  return (
    <div className="space-y-6" lang="es-MX">
      <div className="space-y-4">
        <Link
          href="/inventory/counts"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          Conteos físicos
        </Link>
        <div>
          <h1 className="text-3xl font-bold">{count?.is_opening ? "Conteo de apertura" : "Conteo físico"}</h1>
          {count && (
            <div className="mt-2 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              {open ? <Badge variant="warning">Abierto</Badge> : <Badge variant="secondary">Cerrado</Badge>}
              <span>Iniciado el {formatDateTime(count.created_at)}</span>
              {count.closed_at && <span>· Cerrado el {formatDateTime(count.closed_at)}</span>}
            </div>
          )}
          {count?.notes && <p className="mt-2 max-w-prose text-sm">{count.notes}</p>}
        </div>
        <InventorySectionNav />
      </div>

      {query.isPending ? (
        <LoadingRows rows={4} label="el conteo" />
      ) : query.isError ? (
        <ReadErrorState what="el conteo" error={query.error} onRetry={() => query.refetch()} isRetrying={query.isFetching} />
      ) : count ? (
        <>
          {alreadyClosed && (
            <p role="status" className="rounded-md border p-3 text-sm">
              Este conteo ya estaba cerrado; no se registró nada nuevo.
            </p>
          )}
          {open && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-base">Registrar cantidad contada</CardTitle>
              </CardHeader>
              <CardContent>
                {!canManage ? (
                  <p className="text-sm text-muted-foreground">{managerOnly("registrar o cerrar conteos")}</p>
                ) : materialsQuery.isError ? (
                  <ReadErrorState what="los materiales" error={materialsQuery.error} onRetry={() => materialsQuery.refetch()} />
                ) : (
                  <CountLineForm token={token} countId={count.id} materials={materials} lines={lines} />
                )}
              </CardContent>
            </Card>
          )}

          <section aria-labelledby="count-lines" className="space-y-3">
            <h2 id="count-lines" className="text-xl font-semibold">
              {open ? "Revisión antes de cerrar" : "Resultado"}
            </h2>
            {open && (
              <p className="max-w-prose text-sm text-muted-foreground">
                La diferencia aproximada compara lo contado con la existencia de hoy. Al cerrar, el
                sistema usa la existencia en el momento de cada conteo, que puede variar si hubo
                movimientos después.
              </p>
            )}
            {lines.length === 0 ? (
              <p className="text-sm text-muted-foreground">Aún no hay materiales contados en este conteo.</p>
            ) : (
              <CountLinesTable lines={lines} closed={!open} materialsById={materialsById} />
            )}
            {lines.some((l) => l.movements_since_counted) && (
              <p className="max-w-prose text-sm text-muted-foreground">
                «Movido después de contar» son entradas, consumos o ajustes registrados entre el momento
                del conteo y el cierre. No forman parte de la diferencia: revisa si alguno ya estaba en el
                estante cuando se contó.
              </p>
            )}
            {open && uncounted.length > 0 && (
              <details className="rounded-md border bg-card p-3 text-sm">
                <summary className="cursor-pointer font-medium">
                  {uncounted.length} {uncounted.length === 1 ? "material con existencias sin contar" : "materiales con existencias sin contar"}
                </summary>
                <ul className="mt-2 grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
                  {uncounted.map((m) => (
                    <li key={m.id}>
                      <span className="font-mono">{m.code}</span> <span className="text-muted-foreground">{m.name}</span>
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </section>

          {open && canManage && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-base">Cerrar conteo</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm">
                <p className="max-w-prose">
                  Cerrar registra un ajuste de existencias por cada diferencia y deja el conteo
                  cerrado. <strong>No se puede deshacer.</strong>
                </p>
                <Button
                  onClick={() => {
                    close.reset();
                    setConfirming(true);
                  }}
                  disabled={lines.length === 0}
                >
                  Cerrar conteo…
                </Button>
                {lines.length === 0 && <p className="text-muted-foreground">Registra al menos un material antes de cerrar.</p>}
              </CardContent>
            </Card>
          )}
          <ConfirmRecordDialog
            open={confirming}
            onOpenChange={setConfirming}
            title="Cerrar conteo"
            description={describeClose(lines, materialsById, uncounted.length)}
            irreversible="Cerrar no se puede deshacer: los ajustes quedan registrados y el conteo ya no acepta cambios."
            confirmLabel="Cerrar y registrar ajustes"
            onConfirm={() => close.mutate()}
            isPending={close.isPending}
            error={close.error}
            forbiddenAction="cerrar conteos"
            errorExtra={
              newer.length > 0 ? (
                <ul className="mt-2 space-y-1">
                  {newer.map((n) => (
                    <li key={n.material_id}>
                      <span className="font-mono">{n.material_code}</span>: superado por{" "}
                      <Link className="underline underline-offset-4" href={`/inventory/counts/${n.newer_count_id}`}>
                        el conteo más reciente
                      </Link>
                    </li>
                  ))}
                </ul>
              ) : undefined
            }
          />
        </>
      ) : null}
    </div>
  );
}
