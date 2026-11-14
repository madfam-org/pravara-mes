"use client";

import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2 } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { consumptionAPI, conversionGapsAPI } from "@/lib/inventory-records-api";
import {
  GAP_SOURCE_LABELS,
  JOB_KIND_LABELS,
  formatDateTime,
  formatQty,
  label,
  retryRead,
  shortId,
} from "@/lib/inventory-records";
import { RecordsHeader } from "@/components/inventory/records/section-nav";
import { LoadingRows, Pager, ReadErrorState } from "@/components/inventory/records/states";
import { UnlinkedPanel } from "@/components/inventory/records/unlinked-panel";
import { RECORD_KEYS, useAccessToken } from "@/components/inventory/records/use-records";

const PAGE = 100;
const th = "p-3 text-xs font-medium text-muted-foreground";

function CoverageCard({ token }: { token: string }) {
  const query = useQuery({
    queryKey: [...RECORD_KEYS.coverage, "all-time"],
    queryFn: () => consumptionAPI.coverage(token),
    enabled: !!token,
    retry: retryRead,
  });
  if (query.isPending) return <LoadingRows rows={2} label="la cobertura" />;
  if (query.isError)
    return <ReadErrorState what="la cobertura del consumo" error={query.error} onRetry={() => query.refetch()} isRetrying={query.isFetching} />;
  const c = query.data!;
  const met = c.without_registry_code === 0;
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Consumo con código de registro</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <p className="text-base">
          <span className="font-semibold tabular-nums">{formatQty(c.with_registry_code)}</span> de{" "}
          <span className="tabular-nums">{formatQty(c.material_rows)}</span> registros de material tienen código de
          registro;{" "}
          <span className={met ? "" : "font-semibold text-red-700 dark:text-red-400"}>
            {formatQty(c.without_registry_code)} no lo tienen
          </span>
          .
        </p>
        {met ? (
          <p className="flex items-center gap-2 text-green-800 dark:text-green-300">
            <CheckCircle2 className="h-4 w-4" aria-hidden="true" />
            Se cumple esta parte del criterio de la etapa 1: todo el consumo tiene código.
          </p>
        ) : (
          <p className="text-muted-foreground">
            La etapa 1 se cumple cuando no queda ninguno sin código. Resuelve cada código abajo, en
            «Códigos de consumo sin material».
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function GapsCard({ token }: { token: string }) {
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: [...RECORD_KEYS.gaps, "open", offset],
    queryFn: () => conversionGapsAPI.list(token, { status: "open", limit: PAGE, offset }),
    enabled: !!token,
    retry: retryRead,
  });
  if (query.isPending) return <LoadingRows rows={3} label="los pendientes de conversión" />;
  if (query.isError)
    return <ReadErrorState what="los pendientes de conversión" error={query.error} onRetry={() => query.refetch()} isRetrying={query.isFetching} />;
  const gaps = query.data!.data;
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Falta conversión ({query.data!.total})</CardTitle>
        <p className="text-sm text-muted-foreground">
          Consumo, entradas o conteos registrados en una unidad que no se puede convertir a la del
          artículo. No movieron existencias. Al agregar la conversión al material se registran en ese
          momento.
        </p>
      </CardHeader>
      <CardContent className="space-y-3 p-0 pb-4">
        {gaps.length === 0 ? (
          <p className="flex items-center gap-2 px-6 text-sm text-muted-foreground">
            <CheckCircle2 className="h-4 w-4 text-green-700 dark:text-green-400" aria-hidden="true" />
            No hay nada esperando una conversión.
          </p>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-left">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th scope="col" className={th}>Origen</th>
                    <th scope="col" className={th}>Material</th>
                    <th scope="col" className={`${th} text-right`}>Cantidad</th>
                    <th scope="col" className={th}>Conversión que falta</th>
                    <th scope="col" className={th}>Desde</th>
                  </tr>
                </thead>
                <tbody>
                  {gaps.map((g) => (
                    <tr key={g.id} className="border-b">
                      <td className="p-3 text-sm">
                        {label(GAP_SOURCE_LABELS, g.source_type)}
                        {g.job_kind && (
                          <span className="block text-xs text-muted-foreground">
                            {label(JOB_KIND_LABELS, g.job_kind)} <span className="font-mono">{shortId(g.job_id)}</span>
                          </span>
                        )}
                      </td>
                      <td className="p-3 font-mono text-sm">
                        {g.material_id ? (
                          <Link href={`/inventory/materials/${g.material_id}`} className="underline underline-offset-4">
                            {g.material_code ?? g.sku}
                          </Link>
                        ) : (
                          g.sku
                        )}
                      </td>
                      <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums">
                        {formatQty(g.quantity)} {g.from_unit}
                      </td>
                      <td className="whitespace-nowrap p-3 font-mono text-sm">
                        {g.from_unit} → {g.to_unit}
                      </td>
                      <td className="whitespace-nowrap p-3 text-sm text-muted-foreground">{formatDateTime(g.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="px-6">
              <Pager total={query.data!.total} limit={PAGE} offset={offset} onOffset={setOffset} noun="pendientes" />
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

export default function AttentionPage() {
  const token = useAccessToken();
  return (
    <div className="space-y-6" lang="es-MX">
      <RecordsHeader
        title="Pendientes"
        description="Lo que impide confiar en las cantidades: consumo sin código de registro, registros que esperan una conversión de unidad y lo que aún no está vinculado."
      />
      <CoverageCard token={token} />
      <GapsCard token={token} />
      <UnlinkedPanel token={token} parts={["codes", "items", "lots"]} />
    </div>
  );
}
