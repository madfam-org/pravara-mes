"use client";

import { useId, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Gauge } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { consumptionAPI } from "@/lib/inventory-records-api";
import { buildConsumptionChains, retryRead } from "@/lib/inventory-records";
import { RecordsHeader } from "@/components/inventory/records/section-nav";
import { EmptyState, LoadingRows, Pager, ReadErrorState } from "@/components/inventory/records/states";
import { PrintJobCompleteForm } from "@/components/inventory/records/print-job-complete-form";
import { ConsumptionChains } from "@/components/inventory/records/consumption-chains";
import { RECORD_KEYS, useAccessToken } from "@/components/inventory/records/use-records";

const PAGE = 200;

function isoDay(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Local midnight of a yyyy-mm-dd as RFC 3339, or undefined. */
function dayStart(day: string, plusDays = 0): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return undefined;
  const d = new Date(`${day}T00:00:00`);
  if (Number.isNaN(d.getTime())) return undefined;
  d.setDate(d.getDate() + plusDays);
  return d.toISOString();
}

export default function ConsumptionPage() {
  const id = useId();
  const token = useAccessToken();
  const [from, setFrom] = useState(() => isoDay(new Date(Date.now() - 30 * 86_400_000)));
  const [to, setTo] = useState(() => isoDay(new Date()));
  const [offset, setOffset] = useState(0);

  const range = { from: dayStart(from), to: dayStart(to, 1) };
  const query = useQuery({
    queryKey: [...RECORD_KEYS.consumption, range.from, range.to, offset],
    queryFn: () => consumptionAPI.list(token, { ...range, limit: PAGE, offset }),
    enabled: !!token,
    retry: retryRead,
  });
  const chains = useMemo(() => buildConsumptionChains(query.data?.data ?? []), [query.data]);

  return (
    <div className="space-y-6" lang="es-MX">
      <RecordsHeader
        title="Consumo"
        description="Lo que cada trabajo usó, en cantidades. Al terminar una impresión se registra su consumo real; un error se corrige con un registro nuevo, nunca borrando el anterior."
      />

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-lg">Marcar impresión terminada</CardTitle>
          <p className="text-sm text-muted-foreground">
            Solo registra: no envía nada a ninguna impresora.
          </p>
        </CardHeader>
        <CardContent>
          <PrintJobCompleteForm token={token} />
        </CardContent>
      </Card>

      <section aria-labelledby={`${id}-list`} className="space-y-4">
        <h2 id={`${id}-list`} className="text-xl font-semibold">
          Consumo registrado
        </h2>
        <div className="flex flex-wrap items-end gap-3">
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-from`}>Desde</Label>
            <Input id={`${id}-from`} type="date" value={from} max={to} onChange={(e) => { setFrom(e.target.value); setOffset(0); }} className="w-44" />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-to`}>Hasta (incluido)</Label>
            <Input id={`${id}-to`} type="date" value={to} min={from} onChange={(e) => { setTo(e.target.value); setOffset(0); }} className="w-44" />
          </div>
        </div>
        {query.isPending ? (
          <LoadingRows label="el consumo" />
        ) : query.isError ? (
          <ReadErrorState what="el consumo" error={query.error} onRetry={() => query.refetch()} isRetrying={query.isFetching} />
        ) : chains.length === 0 ? (
          <EmptyState icon={<Gauge />} title="Sin consumo en estas fechas">
            El consumo aparece cuando un trabajo termina: al marcar una impresión terminada, al
            completar un despacho o cuando un operador lo reporta.
          </EmptyState>
        ) : (
          <>
            <ConsumptionChains token={token} chains={chains} />
            <Pager total={query.data!.total} limit={PAGE} offset={offset} onOffset={setOffset} noun="registros" />
          </>
        )}
      </section>
    </div>
  );
}
