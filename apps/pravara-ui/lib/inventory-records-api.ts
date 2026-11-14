/**
 * Client for the RFC 0058 wave 1 inventory records API: the material
 * registry, receipts, physical counts, conversion gaps, consumption coverage,
 * consumption corrections and print-job completion.
 *
 * Contract: docs/operations/inventory.md and docs/operations/job-consumption.md.
 * Quantities only: nothing here reads or sends a cost, price or currency.
 *
 * Every route these screens use lives in this one module so a contract change
 * is one edit. Errors keep the HTTP status and the API's error code so the
 * screens can say what happened (ApiError). Writes are never retried here.
 */

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4500";

/** A failed request: HTTP status (0 when the server was not reached), the
 * API's error code (`material_code_taken`, `count_closed`, ...) and message,
 * and the whole body for codes that carry more (`superseded_by`). */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly body: Record<string, unknown>;

  constructor(status: number, code: string, message: string, body: Record<string, unknown> = {}) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.body = body;
  }
}

type Query = Record<string, string | number | boolean | undefined | null>;

function withQuery(path: string, query?: Query): string {
  if (!query) return path;
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === "") continue;
    params.set(key, String(value));
  }
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

async function request<T>(
  token: string,
  path: string,
  init: { method?: string; body?: unknown } = {}
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${API_BASE_URL}${path}`, {
      method: init.method ?? "GET",
      headers: {
        "Content-Type": "application/json",
        ...(token && { Authorization: `Bearer ${token}` }),
      },
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    });
  } catch (cause) {
    throw new ApiError(
      0,
      "network_error",
      cause instanceof Error ? cause.message : "The server could not be reached"
    );
  }
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as Record<string, unknown>;
    const code = typeof body.error === "string" ? body.error : `http_${response.status}`;
    const message =
      typeof body.message === "string" ? body.message : `HTTP error ${response.status}`;
    throw new ApiError(response.status, code, message, body);
  }
  return response.json() as Promise<T>;
}

// ---------- Shapes (field names as the API returns them) ----------

export interface ListPage<T> {
  data: T[];
  total: number;
  limit: number;
  offset: number;
}

export const MATERIAL_CATEGORIES = [
  "filament",
  "resin",
  "sheet",
  "spare_part",
  "consumable",
  "packaging",
  "other",
] as const;
export type MaterialCategory = (typeof MATERIAL_CATEGORIES)[number];

export interface Material {
  id: string;
  code: string;
  name: string;
  category: MaterialCategory;
  base_unit: string;
  active: boolean;
  notes?: string;
  /** The four stock fields are absent until a stock item is linked. */
  inventory_item_id?: string;
  stock_sku?: string;
  stock_unit?: string;
  quantity_on_hand?: number;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface MaterialAlias {
  id: string;
  material_id: string;
  alias: string;
  created_by?: string;
  created_at: string;
}

export interface UnitConversion {
  id: string;
  material_id: string;
  from_unit: string;
  to_unit: string;
  factor: number;
  note?: string;
  created_by?: string;
  created_at: string;
}

export interface MaterialDetail extends Material {
  aliases: MaterialAlias[];
  conversions: UnitConversion[];
}

export interface MaterialInput {
  code: string;
  name: string;
  category: MaterialCategory;
  base_unit: string;
  active?: boolean;
  notes?: string;
  /** Registers and links an unlinked stock item in one transaction. */
  inventory_item_id?: string;
}

export type MaterialPatch = Partial<Omit<MaterialInput, "code" | "inventory_item_id">>;

export interface AddConversionResult {
  conversion: UnitConversion;
  gaps_resolved: number;
  stock_movements: unknown[] | null;
}

export type UnlinkedReason =
  | "sku_is_not_a_valid_code"
  | "unit_is_not_a_valid_unit"
  | "sku_differs_from_another_only_in_case"
  | "code_registered_for_another_material"
  | "not_linked";

export interface UnlinkedItem {
  id: string;
  sku: string;
  name: string;
  unit: string;
  reason: UnlinkedReason | string;
}

export interface UnlinkedLot {
  id: string;
  lot_number: string;
  product_code?: string;
  product_name: string;
}

export interface UnresolvedCode {
  material_code: string;
  rows: number;
  units: string[];
  last_occurred_at: string;
}

export interface Unlinked {
  items: UnlinkedItem[];
  lots: UnlinkedLot[];
  consumption_codes: UnresolvedCode[];
}

export type GapStatus = "open" | "resolved" | "superseded";
export type GapSource = "job_consumption" | "inventory_receipt" | "inventory_count_line";

export interface ConversionGap {
  id: string;
  inventory_item_id: string;
  sku: string;
  material_id?: string;
  material_code?: string;
  source_type: GapSource | string;
  source_id: string;
  job_kind?: string;
  job_id?: string;
  quantity: number;
  from_unit: string;
  to_unit: string;
  status: GapStatus | string;
  resolved_at?: string | null;
  created_at: string;
}

export type InvoiceStatus = "invoiced" | "no_invoice";
export type ReceiptStatus = "posted" | "needs_conversion";

export interface Receipt {
  id: string;
  material_id: string;
  material_code: string;
  inventory_item_id: string;
  quantity: number;
  unit: string;
  received_at: string;
  received_by?: string;
  batch_lot_id?: string;
  lot_number?: string;
  invoice_status: InvoiceStatus;
  cfdi_uuid?: string;
  cfdi_line?: number;
  status: ReceiptStatus;
  stock_quantity?: number;
  transaction_id?: string;
  posted_at?: string;
  notes?: string;
  idempotency_key?: string;
  created_at: string;
}

export interface ReceiptInput {
  material_id: string;
  quantity: number;
  unit: string;
  received_at?: string;
  invoice_status: InvoiceStatus;
  cfdi_uuid?: string;
  cfdi_line?: number;
  lot_number?: string;
  notes?: string;
  /** Required: one per receipt the person submits, reused on a retry. */
  idempotency_key: string;
}

export interface CreateReceiptResult {
  receipt: Receipt;
  stock_movement: unknown | null;
  replayed: boolean;
}

export type CountLineResult = "matched" | "adjusted" | "needs_conversion" | "not_stocked" | "superseded";

export interface CountLine {
  id: string;
  count_id: string;
  material_id: string;
  material_code: string;
  counted_quantity: number;
  unit: string;
  counted_at: string;
  counted_by?: string;
  result?: CountLineResult | string;
  inventory_item_id?: string;
  expected_quantity?: number;
  counted_stock_quantity?: number;
  difference?: number;
  transaction_id?: string;
  /** Stock movements recorded between counted_at and the close (applied lines). */
  movements_since_counted?: number;
  quantity_since_counted?: number;
  updated_at: string;
}

/** 409 newer_count_exists: the materials a newer closed count already covers. */
export interface NewerCountMaterial {
  material_id: string;
  material_code: string;
  newer_count_id: string;
}

export interface InventoryCount {
  id: string;
  status: "open" | "closed";
  is_opening: boolean;
  notes?: string;
  created_by?: string;
  created_at: string;
  closed_by?: string;
  closed_at?: string;
  line_count: number;
  lines?: CountLine[];
}

export interface CountLineInput {
  material_id: string;
  counted_quantity: number;
  unit: string;
  counted_at?: string;
}

export interface CoverageResult {
  material_rows: number;
  with_registry_code: number;
  without_registry_code: number;
  unresolved_codes: UnresolvedCode[];
  from?: string;
  to?: string;
}

export type Disposition = "good" | "scrap" | "failed_print";

export interface ConsumptionRow {
  id: string;
  job_kind: "dispatch" | "print_job" | "task" | string;
  job_id: string;
  dispatch_id?: string;
  print_job_id?: string;
  task_id?: string;
  order_id?: string;
  engagement_slug?: string;
  machine_id?: string;
  kind: "material" | "machine_time" | string;
  material_code?: string;
  registry_code?: string;
  material_lot?: string;
  quantity: number;
  unit: string;
  basis: "actual" | "estimate" | string;
  source: "printer" | "operator" | "slicer" | string;
  note?: string;
  recorded_by?: string;
  occurred_at: string;
  created_at: string;
  disposition: Disposition | string;
  supersedes_id?: string;
  superseded_by?: string;
}

export interface CorrectionInput {
  quantity: number;
  unit?: string;
  disposition?: Disposition;
  reason?: string;
}

export interface CorrectionResult {
  consumption: ConsumptionRow;
  superseded: ConsumptionRow;
  stock_movements: unknown[] | null;
}

export interface PrintJobCompletionInput {
  material_quantity?: number;
  material_unit?: string;
  material_code?: string;
  disposition?: Disposition;
}

export interface PrintJobCompletionResult {
  print_job_id: string;
  marked_completed: boolean;
  consumption: ConsumptionRow[] | null;
  created: number;
  stock_movements: unknown[] | null;
  material_recorded: boolean;
}

// ---------- Routes ----------

export const materialsAPI = {
  list: (
    token: string,
    q: { active?: boolean; category?: string; search?: string; limit?: number; offset?: number } = {}
  ) => request<ListPage<Material>>(token, withQuery("/v1/inventory/materials", q)),
  get: (token: string, id: string) =>
    request<MaterialDetail>(token, `/v1/inventory/materials/${encodeURIComponent(id)}`),
  create: (token: string, body: MaterialInput) =>
    request<Material>(token, "/v1/inventory/materials", { method: "POST", body }),
  update: (token: string, id: string, body: MaterialPatch) =>
    request<Material>(token, `/v1/inventory/materials/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body,
    }),
  addAlias: (token: string, id: string, alias: string) =>
    request<MaterialAlias>(token, `/v1/inventory/materials/${encodeURIComponent(id)}/aliases`, {
      method: "POST",
      body: { alias },
    }),
  addConversion: (
    token: string,
    id: string,
    body: { from_unit: string; to_unit: string; factor: number; note?: string }
  ) =>
    request<AddConversionResult>(
      token,
      `/v1/inventory/materials/${encodeURIComponent(id)}/conversions`,
      { method: "POST", body }
    ),
  unlinked: (token: string) => request<Unlinked>(token, "/v1/inventory/materials/unlinked"),
  /** Links a stock item to a registry material (PATCH /v1/inventory/{id}). */
  linkStockItem: (token: string, itemId: string, materialId: string) =>
    request<unknown>(token, `/v1/inventory/${encodeURIComponent(itemId)}`, {
      method: "PATCH",
      body: { material_id: materialId },
    }),
};

export const conversionGapsAPI = {
  list: (token: string, q: { status?: GapStatus | "all"; limit?: number; offset?: number } = {}) =>
    request<ListPage<ConversionGap>>(token, withQuery("/v1/inventory/conversion-gaps", q)),
};

export const receiptsAPI = {
  list: (
    token: string,
    q: {
      material_id?: string;
      invoice_status?: InvoiceStatus;
      status?: ReceiptStatus;
      from?: string;
      to?: string;
      limit?: number;
      offset?: number;
    } = {}
  ) => request<ListPage<Receipt>>(token, withQuery("/v1/inventory/receipts", q)),
  create: (token: string, body: ReceiptInput) =>
    request<CreateReceiptResult>(token, "/v1/inventory/receipts", { method: "POST", body }),
};

export const countsAPI = {
  list: (token: string, q: { status?: "open" | "closed"; limit?: number; offset?: number } = {}) =>
    request<ListPage<InventoryCount>>(token, withQuery("/v1/inventory/counts", q)),
  get: (token: string, id: string) =>
    request<InventoryCount>(token, `/v1/inventory/counts/${encodeURIComponent(id)}`),
  create: (token: string, body: { is_opening?: boolean; notes?: string }) =>
    request<InventoryCount>(token, "/v1/inventory/counts", { method: "POST", body }),
  recordLine: (token: string, id: string, body: CountLineInput) =>
    request<CountLine>(token, `/v1/inventory/counts/${encodeURIComponent(id)}/lines`, {
      method: "POST",
      body,
    }),
  close: (token: string, id: string) =>
    request<{ count: InventoryCount; already_closed: boolean }>(
      token,
      `/v1/inventory/counts/${encodeURIComponent(id)}/close`,
      { method: "POST" }
    ),
};

export const consumptionAPI = {
  list: (token: string, q: { from?: string; to?: string; limit?: number; offset?: number } = {}) =>
    request<ListPage<ConsumptionRow>>(token, withQuery("/v1/jobs/consumption", q)),
  coverage: (token: string, q: { from?: string; to?: string } = {}) =>
    request<CoverageResult>(token, withQuery("/v1/jobs/consumption/coverage", q)),
  correct: (token: string, rowId: string, body: CorrectionInput) =>
    request<CorrectionResult>(
      token,
      `/v1/jobs/consumption/${encodeURIComponent(rowId)}/corrections`,
      { method: "POST", body }
    ),
  /** A record only: marks the print job finished. Nothing reaches a machine. */
  completePrintJob: (token: string, printJobId: string, body: PrintJobCompletionInput) =>
    request<PrintJobCompletionResult>(
      token,
      `/v1/print-jobs/${encodeURIComponent(printJobId)}/complete`,
      { method: "POST", body }
    ),
};
