"use client";

import { Suspense, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { Layers, Plus, Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { MATERIAL_CATEGORIES, materialsAPI } from "@/lib/inventory-records-api";
import { CATEGORY_LABELS, formatQty, label, managerOnly, retryRead } from "@/lib/inventory-records";
import { RecordsHeader } from "@/components/inventory/records/section-nav";
import { EmptyState, LoadingRows, Pager, ReadErrorState } from "@/components/inventory/records/states";
import { MaterialForm } from "@/components/inventory/records/material-form";
import { UnlinkedPanel } from "@/components/inventory/records/unlinked-panel";
import {
  RECORD_KEYS,
  useAccessToken,
  useCanManageInventory,
  useDebouncedValue,
} from "@/components/inventory/records/use-records";

const PAGE = 100;
const th = "p-3 text-xs font-medium text-muted-foreground";

function MaterialsScreen() {
  const token = useAccessToken();
  const canManage = useCanManageInventory();
  const router = useRouter();
  const params = useSearchParams();
  const prefill = params?.get("nuevo") ?? "";
  const itemId = params?.get("articulo") ?? "";
  const linkItem = itemId ? { id: itemId, sku: prefill } : undefined;

  const [creating, setCreating] = useState(prefill !== "");
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("all");
  const [active, setActive] = useState<"true" | "false" | "all">("true");
  const [offset, setOffset] = useState(0);
  const debounced = useDebouncedValue(search.trim());

  const query = useQuery({
    queryKey: [...RECORD_KEYS.materials, "list", debounced, category, active, offset],
    queryFn: () =>
      materialsAPI.list(token, {
        search: debounced || undefined,
        category: category === "all" ? undefined : category,
        active: active === "all" ? undefined : active === "true",
        limit: PAGE,
        offset,
      }),
    enabled: !!token,
    retry: retryRead,
  });

  const filtered = debounced !== "" || category !== "all" || active !== "true";
  const resetFilters = () => {
    setSearch("");
    setCategory("all");
    setActive("true");
    setOffset(0);
  };

  return (
    <div className="space-y-6" lang="es-MX">
      <RecordsHeader
        title="Materiales"
        description="El registro de códigos de material: solo lo registrado aquí cuenta como existencia. Pravara guarda cantidades, nunca costos."
      />

      {creating && canManage ? (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-lg">Nuevo material</CardTitle>
          </CardHeader>
          <CardContent>
            <MaterialForm
              token={token}
              initialCode={prefill}
              linkItem={linkItem}
              onDone={(m) => router.push(`/inventory/materials/${m.id}`)}
              onCancel={() => setCreating(false)}
            />
          </CardContent>
        </Card>
      ) : canManage ? (
        <Button onClick={() => setCreating(true)}>
          <Plus aria-hidden="true" />
          Nuevo material
        </Button>
      ) : (
        <p className="text-sm text-muted-foreground">{managerOnly("registrar o cambiar materiales")}</p>
      )}

      <section aria-label="Materiales registrados" className="space-y-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
          <div className="relative w-full sm:w-72">
            <Search
              className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              type="search"
              aria-label="Buscar por código o nombre"
              placeholder="Buscar por código o nombre…"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setOffset(0);
              }}
              className="pl-9"
            />
          </div>
          <Select
            value={category}
            onValueChange={(v) => {
              setCategory(v);
              setOffset(0);
            }}
          >
            <SelectTrigger aria-label="Filtrar por categoría" className="w-full sm:w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Todas las categorías</SelectItem>
              {MATERIAL_CATEGORIES.map((c) => (
                <SelectItem key={c} value={c}>
                  {CATEGORY_LABELS[c]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={active}
            onValueChange={(v) => {
              setActive(v as typeof active);
              setOffset(0);
            }}
          >
            <SelectTrigger aria-label="Filtrar por estado" className="w-full sm:w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="true">Activos</SelectItem>
              <SelectItem value="false">Inactivos</SelectItem>
              <SelectItem value="all">Todos</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {query.isPending ? (
          <LoadingRows label="materiales" />
        ) : query.isError ? (
          <ReadErrorState
            what="los materiales"
            error={query.error}
            onRetry={() => query.refetch()}
            isRetrying={query.isFetching}
          />
        ) : (query.data?.data.length ?? 0) === 0 ? (
          <EmptyState
            icon={<Layers />}
            title={filtered ? "Ningún material coincide" : "Aún no hay materiales registrados"}
            action={
              filtered ? (
                <Button variant="outline" onClick={resetFilters}>
                  Quitar filtros
                </Button>
              ) : undefined
            }
          >
            {filtered
              ? "Prueba con otro código, nombre o categoría."
              : "Registra cada material que entra a existencias (filamento, resina, lámina, refacciones) con un código que no cambie."}
          </EmptyState>
        ) : (
          <>
            <div className="overflow-x-auto rounded-md border bg-card">
              <table className="w-full text-left">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th scope="col" className={th}>Código</th>
                    <th scope="col" className={th}>Nombre</th>
                    <th scope="col" className={th}>Categoría</th>
                    <th scope="col" className={th}>Unidad base</th>
                    <th scope="col" className={`${th} text-right`}>En existencia</th>
                    <th scope="col" className={th}>Estado</th>
                  </tr>
                </thead>
                <tbody>
                  {query.data!.data.map((m) => (
                    <tr key={m.id} className="border-b transition-colors hover:bg-muted/50">
                      <td className="whitespace-nowrap p-3 font-mono text-sm">
                        <Link
                          href={`/inventory/materials/${m.id}`}
                          className="underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          {m.code}
                        </Link>
                      </td>
                      <td className="p-3 text-sm font-medium">{m.name}</td>
                      <td className="p-3 text-sm text-muted-foreground">{label(CATEGORY_LABELS, m.category)}</td>
                      <td className="p-3 font-mono text-sm text-muted-foreground">{m.base_unit}</td>
                      <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums">
                        {m.inventory_item_id ? (
                          `${formatQty(m.quantity_on_hand)} ${m.stock_unit ?? ""}`
                        ) : (
                          <span className="text-muted-foreground">Sin artículo vinculado</span>
                        )}
                      </td>
                      <td className="p-3">
                        {m.active ? (
                          <Badge variant="success">Activo</Badge>
                        ) : (
                          <Badge variant="secondary">Inactivo</Badge>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager
              total={query.data!.total}
              limit={PAGE}
              offset={offset}
              onOffset={setOffset}
              noun="materiales"
            />
          </>
        )}
      </section>

      <UnlinkedPanel token={token} parts={["items", "codes"]} />
    </div>
  );
}

export default function MaterialsPage() {
  return (
    <Suspense fallback={<LoadingRows label="materiales" />}>
      <MaterialsScreen />
    </Suspense>
  );
}
