"use client";

import { Badge } from "@/components/ui/badge";
import type { CountLine, Material } from "@/lib/inventory-records-api";
import {
  COUNT_LINE_RESULT_LABELS,
  formatDateTime,
  formatQty,
  formatSigned,
  label,
  previewCountDifference,
} from "@/lib/inventory-records";

const th = "p-3 text-xs font-medium text-muted-foreground";

function resultVariant(result?: string) {
  if (result === "matched") return "success" as const;
  if (result === "adjusted") return "secondary" as const;
  return "warning" as const;
}

/** The sentence the close confirmation shows. */
export function describeClose(lines: CountLine[], materialsById: Map<string, Material>, uncounted: number): string {
  let withDifference = 0;
  let unknown = 0;
  for (const l of lines) {
    const p = previewCountDifference(l.counted_quantity, l.unit, materialsById.get(l.material_id));
    if (!p) unknown += 1;
    else if (p.difference !== 0) withDifference += 1;
  }
  const parts = [
    `Se cerrará el conteo con ${lines.length} ${lines.length === 1 ? "material" : "materiales"}.`,
    "Para cada uno se calculará la existencia esperada al momento en que se contó y la diferencia se registrará como un ajuste de existencias.",
    `Con lo que muestra el sistema hoy, ${withDifference} ${withDifference === 1 ? "tiene" : "tienen"} diferencia${unknown ? ` y ${unknown} se ${unknown === 1 ? "calcula" : "calculan"} solo al cerrar` : ""}.`,
  ];
  if (uncounted > 0) parts.push(`${uncounted} ${uncounted === 1 ? "material con existencias no se contó y no se ajusta" : "materiales con existencias no se contaron y no se ajustan"}.`);
  return parts.join(" ");
}

/**
 * The count's lines. Open: each counted quantity next to today's stock and a
 * difference labelled as approximate (the API computes the real one at
 * closing, at the moment of each count). Closed: what closing recorded.
 */
export function CountLinesTable({
  lines,
  closed,
  materialsById,
}: {
  lines: CountLine[];
  closed: boolean;
  materialsById: Map<string, Material>;
}) {
  // Reported on applied lines: movements between counted_at and the close.
  const showSince = lines.some((l) => l.movements_since_counted !== undefined);
  return (
    <div className="overflow-x-auto rounded-md border bg-card">
      <table className="w-full text-left">
        <caption className="sr-only">
          {closed ? "Resultado de cada material contado" : "Materiales contados y diferencia aproximada contra la existencia de hoy"}
        </caption>
        <thead>
          <tr className="border-b bg-muted/50">
            <th scope="col" className={th}>Material</th>
            <th scope="col" className={`${th} text-right`}>Contado</th>
            <th scope="col" className={th}>Contado el</th>
            {closed ? (
              <>
                <th scope="col" className={`${th} text-right`}>Esperado</th>
                <th scope="col" className={`${th} text-right`}>Ajuste</th>
                <th scope="col" className={th}>Resultado</th>
                {showSince && <th scope="col" className={`${th} text-right`}>Movido después de contar</th>}
              </>
            ) : (
              <>
                <th scope="col" className={`${th} text-right`}>En sistema hoy</th>
                <th scope="col" className={`${th} text-right`}>Diferencia aprox.</th>
                {showSince && <th scope="col" className={`${th} text-right`}>Movido después de contar</th>}
              </>
            )}
          </tr>
        </thead>
        <tbody>
          {lines.map((l) => {
            const m = materialsById.get(l.material_id);
            const preview = closed ? null : previewCountDifference(l.counted_quantity, l.unit, m);
            const stockUnit = m?.stock_unit ?? "";
            return (
              <tr key={l.id} className="border-b">
                <td className="p-3 text-sm">
                  <span className="font-mono">{l.material_code}</span>
                  {m && <span className="block text-xs text-muted-foreground">{m.name}</span>}
                </td>
                <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums">
                  {formatQty(l.counted_quantity)} {l.unit}
                </td>
                <td className="whitespace-nowrap p-3 text-sm text-muted-foreground">{formatDateTime(l.counted_at)}</td>
                {closed ? (
                  <>
                    <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums">
                      {l.expected_quantity !== undefined ? `${formatQty(l.expected_quantity)} ${stockUnit}` : "—"}
                    </td>
                    <td className="whitespace-nowrap p-3 text-right text-sm font-medium tabular-nums">
                      {l.difference !== undefined && l.result === "adjusted" ? `${formatSigned(l.difference)} ${stockUnit}` : "—"}
                    </td>
                    <td className="p-3">
                      <Badge variant={resultVariant(l.result)} className="whitespace-nowrap">
                        {label(COUNT_LINE_RESULT_LABELS, l.result)}
                      </Badge>
                    </td>
                {showSince && (
                  <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums text-muted-foreground">
                    {l.movements_since_counted
                      ? `${l.movements_since_counted} mov. (${formatSigned(l.quantity_since_counted ?? 0)} ${stockUnit})`
                      : "—"}
                  </td>
                )}
                  </>
                ) : (
                  <>
                    <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums">
                      {m?.inventory_item_id ? `${formatQty(m.quantity_on_hand)} ${stockUnit}` : <span className="text-muted-foreground">Sin artículo</span>}
                    </td>
                    <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums">
                      {preview ? (
                        <span className={preview.difference === 0 ? "text-muted-foreground" : "font-medium"}>
                          {preview.difference === 0 ? "Sin diferencia" : `${formatSigned(preview.difference)} ${preview.unit}`}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">Se calcula al cerrar</span>
                      )}
                    </td>
                {showSince && (
                  <td className="whitespace-nowrap p-3 text-right text-sm tabular-nums text-muted-foreground">
                    {l.movements_since_counted
                      ? `${l.movements_since_counted} mov. (${formatSigned(l.quantity_since_counted ?? 0)} ${stockUnit})`
                      : "—"}
                  </td>
                )}
                  </>
                )}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
