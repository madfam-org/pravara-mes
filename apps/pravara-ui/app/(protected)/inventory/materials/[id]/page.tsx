"use client";

import { useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { materialsAPI } from "@/lib/inventory-records-api";
import { CATEGORY_LABELS, formatDateTime, label, managerOnly, retryRead } from "@/lib/inventory-records";
import { InventorySectionNav } from "@/components/inventory/records/section-nav";
import { LoadingRows, ReadErrorState } from "@/components/inventory/records/states";
import { MaterialForm } from "@/components/inventory/records/material-form";
import { AliasesCard, ConversionsCard, StockLinkCard } from "@/components/inventory/records/material-links";
import { RECORD_KEYS, useAccessToken, useCanManageInventory } from "@/components/inventory/records/use-records";

export default function MaterialDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? "";
  const token = useAccessToken();
  const canManage = useCanManageInventory();
  const [editing, setEditing] = useState(false);

  const query = useQuery({
    queryKey: [...RECORD_KEYS.materials, "detail", id],
    queryFn: () => materialsAPI.get(token, id),
    enabled: !!token && !!id,
    retry: retryRead,
  });
  const material = query.data;

  return (
    <div className="space-y-6" lang="es-MX">
      <div className="space-y-4">
        <Link
          href="/inventory/materials"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          Materiales
        </Link>
        {material ? (
          <div>
            <h1 className="break-words text-3xl font-bold">
              <span className="font-mono">{material.code}</span>{" "}
              <span className="block text-lg font-normal">{material.name}</span>
            </h1>
            <div className="mt-2 flex flex-wrap gap-2">
              <Badge variant="outline">{label(CATEGORY_LABELS, material.category)}</Badge>
              {material.active ? <Badge variant="success">Activo</Badge> : <Badge variant="secondary">Inactivo</Badge>}
            </div>
          </div>
        ) : (
          <h1 className="text-3xl font-bold">Material</h1>
        )}
        <InventorySectionNav />
      </div>

      {query.isPending ? (
        <LoadingRows rows={4} label="el material" />
      ) : query.isError ? (
        <ReadErrorState what="el material" error={query.error} onRetry={() => query.refetch()} isRetrying={query.isFetching} />
      ) : material ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader className="flex flex-row items-start justify-between gap-2 space-y-0 pb-2">
              <CardTitle className="text-base">Datos</CardTitle>
              {canManage && !editing && (
                <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                  Editar
                </Button>
              )}
            </CardHeader>
            <CardContent>
              {editing ? (
                <MaterialForm token={token} material={material} onDone={() => setEditing(false)} onCancel={() => setEditing(false)} />
              ) : (
                <div className="space-y-3 text-sm">
                  <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                    <dt className="text-muted-foreground">Unidad base</dt>
                    <dd className="font-mono">{material.base_unit}</dd>
                    <dt className="text-muted-foreground">Notas</dt>
                    <dd className="whitespace-pre-wrap">{material.notes || "—"}</dd>
                    <dt className="text-muted-foreground">Registrado</dt>
                    <dd>{formatDateTime(material.created_at)}</dd>
                    <dt className="text-muted-foreground">Último cambio</dt>
                    <dd>{formatDateTime(material.updated_at)}</dd>
                  </dl>
                  {!canManage && <p className="text-muted-foreground">{managerOnly("editar materiales")}</p>}
                </div>
              )}
            </CardContent>
          </Card>
          <StockLinkCard material={material} token={token} canManage={canManage} />
          <AliasesCard material={material} token={token} canManage={canManage} />
          <ConversionsCard material={material} token={token} canManage={canManage} />
        </div>
      ) : null}
    </div>
  );
}
