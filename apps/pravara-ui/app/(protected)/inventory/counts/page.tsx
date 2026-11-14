"use client";

import { useId, useState, type FormEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClipboardList } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { countsAPI } from "@/lib/inventory-records-api";
import { formatDateTime, managerOnly, retryRead } from "@/lib/inventory-records";
import { RecordsHeader } from "@/components/inventory/records/section-nav";
import { ConfirmRecordDialog } from "@/components/inventory/records/confirm-record-dialog";
import { EmptyState, LoadingRows, Pager, ReadErrorState } from "@/components/inventory/records/states";
import { RECORD_KEYS, useAccessToken, useCanManageInventory } from "@/components/inventory/records/use-records";

const PAGE = 50;
const th = "p-3 text-xs font-medium text-muted-foreground";

function StartCountForm({ token }: { token: string }) {
  const id = useId();
  const router = useRouter();
  const queryClient = useQueryClient();
  const [isOpening, setIsOpening] = useState(false);
  const [notes, setNotes] = useState("");
  const [confirming, setConfirming] = useState(false);
  const mutation = useMutation({
    mutationFn: () => countsAPI.create(token, { is_opening: isOpening || undefined, notes: notes.trim() || undefined }),
    onSuccess: (count) => {
      queryClient.invalidateQueries({ queryKey: RECORD_KEYS.counts });
      router.push(`/inventory/counts/${count.id}`);
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    mutation.reset();
    setConfirming(true);
  };
  return (
    <form onSubmit={submit} className="space-y-4" aria-label="Iniciar conteo">
      <div className="flex items-start gap-2">
        <Checkbox id={`${id}-opening`} checked={isOpening} onCheckedChange={(v) => setIsOpening(v === true)} className="mt-0.5" />
        <div>
          <Label htmlFor={`${id}-opening`}>Es el conteo de apertura</Label>
          <p className="text-xs text-muted-foreground">
            El primer conteo, el que fija las existencias iniciales. Solo puede haber uno y no se
            puede marcar después.
          </p>
        </div>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor={`${id}-notes`}>Notas (opcional)</Label>
        <Textarea id={`${id}-notes`} rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} />
      </div>
      <Button type="submit">Iniciar conteo</Button>
      <ConfirmRecordDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={isOpening ? "Iniciar el conteo de apertura" : "Iniciar conteo"}
        description={
          isOpening
            ? "Se abrirá el conteo de apertura de la organización. Mientras esté abierto puedes registrar y reemplazar cantidades; no mueve existencias hasta cerrarlo."
            : "Se abrirá un conteo físico. Mientras esté abierto puedes registrar y reemplazar cantidades; no mueve existencias hasta cerrarlo."
        }
        irreversible={isOpening ? "Solo puede existir un conteo de apertura." : undefined}
        confirmLabel="Iniciar conteo"
        onConfirm={() => mutation.mutate()}
        isPending={mutation.isPending}
        error={mutation.error}
        forbiddenAction="iniciar conteos"
      />
    </form>
  );
}

export default function CountsPage() {
  const token = useAccessToken();
  const canManage = useCanManageInventory();
  const [status, setStatus] = useState<"open" | "closed" | "all">("all");
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: [...RECORD_KEYS.counts, "list", status, offset],
    queryFn: () => countsAPI.list(token, { status: status === "all" ? undefined : status, limit: PAGE, offset }),
    enabled: !!token,
    retry: retryRead,
  });

  return (
    <div className="space-y-6" lang="es-MX">
      <RecordsHeader
        title="Conteos físicos"
        description="Cuenta lo que hay en el almacén y compáralo con el sistema. Al cerrar un conteo, cada diferencia se registra como un ajuste propio."
      />

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-lg">Nuevo conteo</CardTitle>
        </CardHeader>
        <CardContent>
          {canManage ? (
            <StartCountForm token={token} />
          ) : (
            <p className="text-sm text-muted-foreground">{managerOnly("iniciar, registrar o cerrar conteos")}</p>
          )}
        </CardContent>
      </Card>

      <section aria-labelledby="counts-list" className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 id="counts-list" className="text-xl font-semibold">
            Conteos
          </h2>
          <div role="group" aria-label="Filtrar por estado" className="inline-flex rounded-md bg-muted p-1">
            {(
              [
                ["all", "Todos"],
                ["open", "Abiertos"],
                ["closed", "Cerrados"],
              ] as const
            ).map(([value, text]) => (
              <Button
                key={value}
                size="sm"
                variant={status === value ? "outline" : "ghost"}
                aria-pressed={status === value}
                className="h-8"
                onClick={() => {
                  setStatus(value);
                  setOffset(0);
                }}
              >
                {text}
              </Button>
            ))}
          </div>
        </div>
        {query.isPending ? (
          <LoadingRows label="conteos" />
        ) : query.isError ? (
          <ReadErrorState what="los conteos" error={query.error} onRetry={() => query.refetch()} isRetrying={query.isFetching} />
        ) : (query.data?.data.length ?? 0) === 0 ? (
          <EmptyState icon={<ClipboardList />} title={status === "all" ? "Aún no hay conteos" : "Ningún conteo en este estado"}>
            {status === "all"
              ? "El primero debería ser el conteo de apertura: fija las existencias con las que empieza el registro."
              : "Cambia el filtro para ver los demás."}
          </EmptyState>
        ) : (
          <>
            <div className="overflow-x-auto rounded-md border bg-card">
              <table className="w-full text-left">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th scope="col" className={th}>Iniciado</th>
                    <th scope="col" className={th}>Estado</th>
                    <th scope="col" className={`${th} text-right`}>Materiales contados</th>
                    <th scope="col" className={th}>Cerrado</th>
                    <th scope="col" className={th}>Notas</th>
                  </tr>
                </thead>
                <tbody>
                  {query.data!.data.map((c) => (
                    <tr key={c.id} className="border-b transition-colors hover:bg-muted/50">
                      <td className="whitespace-nowrap p-3 text-sm">
                        <Link href={`/inventory/counts/${c.id}`} className="font-medium underline-offset-4 hover:underline">
                          {formatDateTime(c.created_at)}
                        </Link>
                      </td>
                      <td className="p-3">
                        <div className="flex flex-wrap gap-1">
                          {c.status === "open" ? <Badge variant="warning">Abierto</Badge> : <Badge variant="secondary">Cerrado</Badge>}
                          {c.is_opening && <Badge variant="outline">Apertura</Badge>}
                        </div>
                      </td>
                      <td className="p-3 text-right text-sm tabular-nums">{c.line_count}</td>
                      <td className="whitespace-nowrap p-3 text-sm text-muted-foreground">{formatDateTime(c.closed_at)}</td>
                      <td className="max-w-xs truncate p-3 text-sm text-muted-foreground">{c.notes || "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager total={query.data!.total} limit={PAGE} offset={offset} onOffset={setOffset} noun="conteos" />
          </>
        )}
      </section>
    </div>
  );
}
