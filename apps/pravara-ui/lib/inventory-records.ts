/**
 * Pure helpers for the inventory records screens (RFC 0058 wave 1): Spanish
 * (es-MX) labels, what an API error means and what to do about it, quantity
 * and date formatting, unit rules, and the append-only consumption history.
 *
 * Quantities only: nothing here formats or implies money.
 */

import { ApiError, type ConsumptionRow, type Material } from "@/lib/inventory-records-api";

export const CATEGORY_LABELS: Record<string, string> = {
  filament: "Filamento",
  resin: "Resina",
  sheet: "Lámina",
  spare_part: "Refacción",
  consumable: "Consumible",
  packaging: "Empaque",
  other: "Otro",
};

export const DISPOSITION_LABELS: Record<string, string> = {
  good: "Bueno",
  scrap: "Merma",
  failed_print: "Impresión fallida",
};

export const DISPOSITION_HINTS: Record<string, string> = {
  good: "La pieza salió bien.",
  scrap: "Material desperdiciado: soportes, purgas, recortes.",
  failed_print: "La impresión falló y la pieza no sirve.",
};

export const RECEIPT_STATUS_LABELS: Record<string, string> = {
  posted: "En existencias",
  needs_conversion: "Falta conversión",
};

export const COUNT_LINE_RESULT_LABELS: Record<string, string> = {
  matched: "Coincide",
  adjusted: "Ajustado",
  needs_conversion: "Falta conversión",
  not_stocked: "Sin artículo de existencias",
  superseded: "Superada por un conteo más reciente",
};

export const UNLINKED_REASON_LABELS: Record<string, string> = {
  sku_is_not_a_valid_code: "El SKU no es un código válido",
  unit_is_not_a_valid_unit: "La unidad no es válida",
  sku_differs_from_another_only_in_case: "Otro SKU es igual salvo mayúsculas",
  code_registered_for_another_material: "El código ya es de otro material",
  not_linked: "No está vinculado",
};

export const GAP_SOURCE_LABELS: Record<string, string> = {
  job_consumption: "Consumo de trabajo",
  inventory_receipt: "Entrada",
  inventory_count_line: "Línea de conteo",
};

export const JOB_KIND_LABELS: Record<string, string> = {
  dispatch: "Despacho",
  print_job: "Impresión",
  task: "Tarea",
};

export const BASIS_LABELS: Record<string, string> = {
  actual: "Real",
  estimate: "Estimado",
};

export const SOURCE_LABELS: Record<string, string> = {
  printer: "impresora",
  operator: "operador",
  slicer: "laminador",
};

export function label(map: Record<string, string>, key: string | undefined | null): string {
  if (!key) return "—";
  return map[key] ?? key;
}

// ---------- Roles ----------

/** Roles pravara-api requires for receipts, counts and registry changes. */
export const INVENTORY_MANAGER_ROLES = ["admin", "inventory_manager"];

/**
 * Whether to offer the role-gated inventory writes. When the token names no
 * roles at all the UI cannot know, so it offers them and the API decides
 * (a 403 is then shown as its own state).
 */
export function canManageInventory(user?: { roles?: string[]; rolesKnown?: boolean } | null): boolean {
  if (!user || !user.rolesKnown) return true;
  return (user.roles ?? []).some((r) => INVENTORY_MANAGER_ROLES.includes(r));
}

/** The one sentence shown instead of a gated action. */
export function managerOnly(action: string): string {
  return `Solo una persona con rol de administrador o de responsable de inventario puede ${action}.`;
}

// ---------- Errors ----------

/** Spanish meaning of each error code the contract documents. */
const ERROR_MEANINGS: Record<string, string> = {
  material_code_taken:
    "Ese código ya existe: es el código, un alias o el SKU de otro material, o el SKU de un artículo sin vincular (regístralo desde ese artículo para vincularlo). No distingue mayúsculas.",
  idempotency_key_reused:
    "Este formulario ya envió una entrada con otros datos. No se registró esta versión.",
  newer_count_exists:
    "Un conteo más reciente ya cerró con algunos de estos materiales: este conteo quedó superado para ellos. Vuelve a contarlos en un conteo nuevo.",
  material_not_found: "El material ya no existe o no es de tu organización.",
  alias_taken:
    "Ese alias ya está en uso: es el código, un alias o el SKU de un artículo (vinculado o no). El SKU de un artículo sin vincular se registra como material nuevo vinculado a ese artículo.",
  item_not_linkable: "Ese artículo de existencias no existe o ya está vinculado a otro material.",
  conversion_exists:
    "Este material ya tiene una conversión entre esas dos unidades (en cualquier dirección). Las conversiones no se cambian.",
  conversion_builtin:
    "Esa conversión ya viene incluida (misma unidad, kg↔g, l↔ml, m↔mm, cm↔mm). No hace falta agregarla.",
  material_inactive: "El material está inactivo. Actívalo antes de registrar una entrada.",
  material_not_stocked:
    "El material no tiene un artículo de existencias vinculado. Vincúlalo desde el material.",
  material_already_stocked: "Ese material ya tiene un artículo de existencias vinculado.",
  lot_belongs_to_other_item: "Ese lote pertenece a otro artículo de existencias.",
  batch_lot_not_found: "No se encontró el lote.",
  opening_count_exists:
    "Ya existe un conteo de apertura. Solo puede haber uno; inicia este como conteo normal.",
  count_not_found: "El conteo ya no existe o no es de tu organización.",
  count_closed: "El conteo ya está cerrado. Inicia uno nuevo para contar otra vez.",
  receipt_not_found: "No se encontró la entrada.",
  consumption_not_found: "No se encontró ese registro de consumo.",
  already_superseded:
    "Ese registro ya fue corregido. Corrige la corrección más reciente (aparece arriba en el historial).",
  print_job_not_found: "No existe una impresión con ese identificador en tu organización.",
  not_effective:
    "Ese registro ya no es el que cuenta para este trabajo: hay uno más reciente. Corrige el registro vigente (el que muestra «Corregir»).",
  print_job_closed: "Esa impresión está cancelada o marcada como fallida; no se puede marcar como terminada.",
  print_job_not_started:
    "Esa impresión sigue en cola (pendiente o en preparación): no ha impreso. Márcala cuando termine.",
  disposition_differs:
    "Esa impresión ya está terminada con otro resultado. Para cambiarlo, corrige su consumo en la lista «Consumo registrado».",
  already_recorded:
    "Esa impresión ya tiene registrada otra cantidad real. Para cambiarla, corrige su consumo en la lista «Consumo registrado».",
  validation_error: "El servidor rechazó un dato.",
  forbidden: "No tienes permiso para esta acción.",
  network_error: "No se pudo contactar al servidor. Revisa la conexión.",
};

export interface ExplainedError {
  /** What happened, in Spanish. */
  meaning: string;
  /** The server's own words, shown as detail so nothing is hidden. */
  serverMessage?: string;
  status: number;
  code: string;
}

/** What an error means, keeping the server's message as detail. */
export function explainError(error: unknown): ExplainedError {
  if (error instanceof ApiError) {
    let meaning = ERROR_MEANINGS[error.code];
    if (!meaning) {
      if (error.status === 401) meaning = "Tu sesión expiró. Vuelve a iniciar sesión.";
      else if (error.status === 403) meaning = ERROR_MEANINGS.forbidden;
      else if (error.status >= 500) meaning = "El servidor tuvo un error.";
      else meaning = "El servidor rechazó la solicitud.";
    }
    return { meaning, serverMessage: error.message, status: error.status, code: error.code };
  }
  return {
    meaning: "Ocurrió un error inesperado.",
    serverMessage: error instanceof Error ? error.message : undefined,
    status: 0,
    code: "unknown",
  };
}

export function isForbidden(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 401 || error.status === 403);
}

/** Reads retry on network and server failures only; never on 4xx. */
export function retryRead(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError && error.status >= 400 && error.status < 500) return false;
  return failureCount < 2;
}

// ---------- Numbers, dates, units ----------

const qtyFormat = new Intl.NumberFormat("es-MX", { maximumFractionDigits: 4 });

export function formatQty(value: number | undefined | null): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return "—";
  return qtyFormat.format(value);
}

export function formatSigned(value: number): string {
  if (value > 0) return `+${formatQty(value)}`;
  if (value < 0) return `−${formatQty(Math.abs(value))}`;
  return "0";
}

const dateTimeFormat = new Intl.DateTimeFormat("es-MX", {
  dateStyle: "medium",
  timeStyle: "short",
});

export function formatDateTime(iso: string | undefined | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : dateTimeFormat.format(d);
}

/** A `<input type="datetime-local">` value for now, in local time. */
export function nowLocalInput(now: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}T${pad(
    now.getHours()
  )}:${pad(now.getMinutes())}`;
}

/** RFC 3339 with the browser's offset for a datetime-local value, or null. */
export function localInputToRfc3339(value: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(value)) return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  const pad = (n: number) => String(Math.trunc(Math.abs(n))).padStart(2, "0");
  const offset = -d.getTimezoneOffset();
  const sign = offset >= 0 ? "+" : "-";
  const base = value.length === 16 ? `${value}:00` : value;
  return `${base}${sign}${pad(offset / 60)}:${pad(offset % 60)}`;
}

/** Parses a typed quantity: a finite number with up to 4 decimals, or null. */
export function parseQuantity(raw: string): number | null {
  const text = raw.trim().replace(",", ".");
  if (text === "" || !/^-?\d+(\.\d{1,4})?$/.test(text)) return null;
  const n = Number(text);
  return Number.isFinite(n) ? n : null;
}

const UNIT_RE = /^[a-z][a-z0-9_]{0,15}$/;
const CODE_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export const normalizeUnit = (u: string) => u.trim().toLowerCase();
export const isValidUnit = (u: string) => UNIT_RE.test(normalizeUnit(u));
export const isValidCode = (c: string) => CODE_RE.test(c.trim());
export const isUuid = (v: string) => UUID_RE.test(v.trim());

export const COMMON_UNITS = ["g", "kg", "ml", "l", "mm", "m", "sheet", "spool", "pcs"];
/** Units the API accepts for consumed material (job-consumption.md). */
export const CONSUMPTION_MATERIAL_UNITS = ["g", "mm", "ml", "sheet"];

const BUILTIN: Record<string, number> = { "kg>g": 1000, "l>ml": 1000, "m>mm": 1000, "cm>mm": 10 };

/** Converts with the conversions every material has, or returns null. */
export function convertBuiltin(qty: number, from: string, to: string): number | null {
  const f = normalizeUnit(from);
  const t = normalizeUnit(to);
  if (f === t) return qty;
  if (BUILTIN[`${f}>${t}`]) return qty * BUILTIN[`${f}>${t}`];
  if (BUILTIN[`${t}>${f}`]) return qty / BUILTIN[`${t}>${f}`];
  return null;
}

/**
 * A preview of a count line's difference against TODAY's stock on hand. The
 * API computes the real one at closing, against the stock at the moment of
 * the count; this is only a guide and is labelled as such.
 */
export function previewCountDifference(
  counted: number,
  unit: string,
  material: Pick<Material, "quantity_on_hand" | "stock_unit"> | undefined
): { onHand: number; counted: number; difference: number; unit: string } | null {
  if (!material || material.quantity_on_hand === undefined || !material.stock_unit) return null;
  const inStockUnit = convertBuiltin(counted, unit, material.stock_unit);
  if (inStockUnit === null) return null;
  const round = (n: number) => Math.round(n * 10000) / 10000;
  return {
    onHand: material.quantity_on_hand,
    counted: round(inStockUnit),
    difference: round(inStockUnit - material.quantity_on_hand),
    unit: material.stock_unit,
  };
}

// ---------- Consumption history (append-only) ----------

export interface ConsumptionChain {
  /** The row that counts today (not superseded). */
  current: ConsumptionRow;
  /** Earlier rows it replaced, newest first. Kept, never erased. */
  history: ConsumptionRow[];
  /** True when an earlier row is outside the loaded page. */
  historyIncomplete: boolean;
  /**
   * Whether `current` is the job's effective row for its material (or
   * machine time): not superseded, and the latest actual, else the latest
   * estimate. Only the effective row can be corrected.
   */
  effective: boolean;
}

function factKey(r: ConsumptionRow): string {
  const material = r.kind === "material" ? (r.registry_code ?? r.material_code ?? "").toLowerCase() : "";
  return `${r.job_kind}|${r.job_id}|${r.kind}|${material}`;
}

function isNewer(a: ConsumptionRow, b: ConsumptionRow): boolean {
  return a.created_at > b.created_at || (a.created_at === b.created_at && a.id > b.id);
}

/** Groups rows into correction chains: each current row with what it replaced. */
export function buildConsumptionChains(rows: ConsumptionRow[]): ConsumptionChain[] {
  const byId = new Map(rows.map((r) => [r.id, r]));
  const chains: ConsumptionChain[] = [];
  for (const row of rows) {
    if (row.superseded_by && byId.has(row.superseded_by)) continue;
    const history: ConsumptionRow[] = [];
    let historyIncomplete = false;
    let cursor = row.supersedes_id;
    const seen = new Set([row.id]);
    while (cursor && !seen.has(cursor)) {
      seen.add(cursor);
      const prev = byId.get(cursor);
      if (!prev) {
        historyIncomplete = true;
        break;
      }
      history.push(prev);
      cursor = prev.supersedes_id;
    }
    chains.push({ current: row, history, historyIncomplete, effective: false });
  }
  const best = new Map<string, ConsumptionRow>();
  for (const { current } of chains) {
    if (current.superseded_by) continue;
    const key = factKey(current);
    const prev = best.get(key);
    const rank = (r: ConsumptionRow) => (r.basis === "actual" ? 1 : 0);
    if (!prev || rank(current) > rank(prev) || (rank(current) === rank(prev) && isNewer(current, prev))) {
      best.set(key, current);
    }
  }
  for (const chain of chains) {
    chain.effective = best.get(factKey(chain.current))?.id === chain.current.id;
  }
  return chains;
}

export function shortId(id: string | undefined | null): string {
  if (!id) return "—";
  return id.length > 8 ? id.slice(0, 8) : id;
}
