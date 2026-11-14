"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { PackageOpen } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { receiptsAPI, type InvoiceStatus, type ReceiptStatus } from "@/lib/inventory-records-api";
import {
  RECEIPT_STATUS_LABELS,
  formatDateTime,
  formatQty,
  label,
  managerOnly,
  retryRead,
} from "@/lib/inventory-records";
import { RecordsHeader } from "@/components/inventory/records/section-nav";
import { EmptyState, LoadingRows, Pager, ReadErrorState } from "@/components/inventory/records/states";
import { ReceiptForm } from "@/components/inventory/records/receipt-form";
import {
  RECORD_KEYS,
  useAccessToken,
  useCanManageInventory,
  useMaterialOptions,
} from "@/components/inventory/records/use-records";

const PAGE = 50;
const th = "p-3 text-xs font-medium text-muted-foreground";

export default function ReceiptsPage() {
  const token = useAccessToken();
  const canManage = useCanManageInventory();
  const materialsQuery = useMaterialOptions(token, { activeOnly: true });
  const [materialId, setMaterialId] = useState("all");
  const [invoice, setInvoice] = useState<InvoiceStatus | "all">("all");
  const [status, setStatus] = useState<ReceiptStatus | "all">("all");
  const [offset, setOffset] = useState(0);

  const query = useQuery({
    queryKey: [...RECORD_KEYS.receipts, materialId, invoice, status, offset],
    queryFn: () =>
      receiptsAPI.list(token, {
        material_id: materialId === "all" ? undefined : materialId,
        invoice_status: invoice === "all" ? undefined : invoice,
        status: status === "all" ? undefined : status,
        limit: PAGE,
        offset,
      }),
    enabled: !!token,
    retry: retryRead,
  });

  const filtered = materialId !== "all" || invoice !== "all" || status !== "all";
  const materials = materialsQuery.data?.data ?? [];

  return (
    <div className="space-y-6" lang="es-MX">
      <RecordsHeader
        title="Entradas"
        description="Cada material que llega físicamente, con su lote y su factura (o la constancia de que no la tiene). Una entrada no se edita ni se anula."
      />

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-lg">Registrar entrada</CardTitle>
        </CardHeader>
        <CardContent>
          {!canManage ? (
            <p className="text-sm text-muted-foreground">{managerOnly("registrar entradas")}</p>
          ) : materialsQuery.isPending ? (
            <p className="text-sm text-muted-foreground">Cargando materiales…</p>
          ) : materialsQuery.isError ? (
            <ReadErrorState what="los materiales" error={materialsQuery.error} onRetry={() => materialsQuery.refetch()} isRetrying={materialsQuery.isFetching} />
          ) : materials.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No hay materiales activos. Registra el material primero en Materiales.
            </p>
          ) : (
            <ReceiptForm token={token} materials={materials} />
          )}
        </CardContent>
      </Card>

      <section aria-labelledby="receipts-list" className="space-y-4">
        <h2 id="receipts-list" className="text-xl font-semibold">
          Entradas registradas
        </h2>
        <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap">
          <Select value={materialId} onValueChange={(v) => { setMaterialId(v); setOffset(0); }}>
            <SelectTrigger aria-label="Filtrar por material" className="w-full sm:w-64">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Todos los materiales</SelectItem>
              {materials.map((m) => (
                <SelectItem key={m.id} value={m.id}>
                  {m.code} · {m.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={invoice} onValueChange={(v) => { setInvoice(v as typeof invoice); setOffset(0); }}>
            <SelectTrigger aria-label="Filtrar por factura" className="w-full sm:w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Con y sin factura</SelectItem>
              <SelectItem value="invoiced">Con factura</SelectItem>
              <SelectItem value="no_invoice">Sin factura</SelectItem>
            </SelectContent>
          </Select>
          <Select value={status} onValueChange={(v) => { setStatus(v as typeof status); setOffset(0); }}>
            <SelectTrigger aria-label="Filtrar por estado" className="w-full sm:w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Todos los estados</SelectItem>
              <SelectItem value="posted">En existencias</SelectItem>
              <SelectItem value="needs_conversion">Falta conversión</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {query.isPending ? (
          <LoadingRows label="entradas" />
        ) : query.isError ? (
          <ReadErrorState what="las entradas" error={query.error} onRetry={() => query.refetch()} isRetrying={query.isFetching} />
        ) : (query.data?.data.length ?? 0) === 0 ? (
          <EmptyState
            icon={<PackageOpen />}
            title={filtered ? "Ninguna entrada coincide" : "Aún no hay entradas"}
            action={
              filtered ? (
                <Button variant="outline" onClick={() => { setMaterialId("all"); setInvoice("all"); setStatus("all"); setOffset(0); }}>
                  Quitar filtros
                </Button>
              ) : undefined
            }
          >
            {filtered
              ? "Cambia los filtros para ver otras entradas."
              : "Cada material que llegue se registra aquí con su cantidad, lote y factura."}
          </EmptyState>
        ) : (
          <>
            <div className="overflow-x-auto rounded-md border bg-card">
              <table className="w-full text-left">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th scope="col" className={th}>Recibida</th>
                    <th scope="col" className={th}>Material</th>
                    <th scope="col" className={`${th} text-right`}>Cantidad</th>
                    <th scope="col" className={th}>Lote</th>
                    <th scope="col" className={th}>Factura</th>
                    <th scope="col" className={th}>Estado</th>
                  </tr>
                </thead>
                <tbody>
                  {query.data!.data.map((r) => (
                    <tr key={r.id} className="border-b align-top transition-colors hover:bg-muted/50">
                      <td className="whitespace-nowrap p-3 text-sm">{formatDateTime(r.received_at)}</td>
                      <td className="whitespace-nowrap p-3 font-mono text-sm">{r.material_code}</td>
                      <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums">
                        {formatQty(r.quantity)} {r.unit}
                      </td>
                      <td className="p-3 font-mono text-sm text-muted-foreground">{r.lot_number || "—"}</td>
                      <td className="p-3 text-sm">
                        {r.invoice_status === "invoiced" ? (
                          <span className="font-mono text-xs break-all" title={r.cfdi_uuid}>
                            {r.cfdi_uuid} · línea {r.cfdi_line}
                          </span>
                        ) : (
                          <span>Sin factura</span>
                        )}
                      </td>
                      <td className="p-3">
                        <Badge variant={r.status === "posted" ? "success" : "warning"} className="whitespace-nowrap">
                          {label(RECEIPT_STATUS_LABELS, r.status)}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager total={query.data!.total} limit={PAGE} offset={offset} onOffset={setOffset} noun="entradas" />
          </>
        )}
      </section>
    </div>
  );
}
