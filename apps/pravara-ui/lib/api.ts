const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4500";

type FetchOptions = RequestInit & {
  token?: string;
};

async function fetchAPI<T>(
  endpoint: string,
  options: FetchOptions = {}
): Promise<T> {
  const { token, ...fetchOptions } = options;

  const headers: HeadersInit = {
    "Content-Type": "application/json",
    ...(token && { Authorization: `Bearer ${token}` }),
    ...fetchOptions.headers,
  };

  const response = await fetch(`${API_BASE_URL}${endpoint}`, {
    ...fetchOptions,
    headers,
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({}));
    throw new Error(error.message || `HTTP error ${response.status}`);
  }

  return response.json();
}

// Orders API
export const ordersAPI = {
  list: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<Order>>(
      `/v1/orders${params ? `?${params}` : ""}`,
      { token }
    ),

  get: (token: string, id: string) =>
    fetchAPI<Order>(`/v1/orders/${id}`, { token }),

  create: (token: string, data: CreateOrderRequest) =>
    fetchAPI<Order>("/v1/orders", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  update: (token: string, id: string, data: UpdateOrderRequest) =>
    fetchAPI<Order>(`/v1/orders/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  delete: (token: string, id: string) =>
    fetchAPI<{ message: string }>(`/v1/orders/${id}`, {
      method: "DELETE",
      token,
    }),
};

// Tasks API
export const tasksAPI = {
  list: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<Task>>(
      `/v1/tasks${params ? `?${params}` : ""}`,
      { token }
    ),

  getBoard: (token: string) =>
    fetchAPI<KanbanBoard>("/v1/tasks/board", { token }),

  get: (token: string, id: string) =>
    fetchAPI<Task>(`/v1/tasks/${id}`, { token }),

  create: (token: string, data: CreateTaskRequest) =>
    fetchAPI<Task>("/v1/tasks", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  update: (token: string, id: string, data: UpdateTaskRequest) =>
    fetchAPI<Task>(`/v1/tasks/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  move: (token: string, id: string, status: string, position: number) =>
    fetchAPI<{ message: string }>(`/v1/tasks/${id}/move`, {
      method: "POST",
      body: JSON.stringify({ status, position }),
      token,
    }),

  assign: (token: string, id: string, userId?: string, machineId?: string) =>
    fetchAPI<{ message: string }>(`/v1/tasks/${id}/assign`, {
      method: "POST",
      body: JSON.stringify({ user_id: userId, machine_id: machineId }),
      token,
    }),

  delete: (token: string, id: string) =>
    fetchAPI<{ message: string }>(`/v1/tasks/${id}`, {
      method: "DELETE",
      token,
    }),
};

// Machines API
export const machinesAPI = {
  list: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<Machine>>(
      `/v1/machines${params ? `?${params}` : ""}`,
      { token }
    ),

  get: (token: string, id: string) =>
    fetchAPI<Machine>(`/v1/machines/${id}`, { token }),

  create: (token: string, data: CreateMachineRequest) =>
    fetchAPI<Machine>("/v1/machines", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  update: (token: string, id: string, data: UpdateMachineRequest) =>
    fetchAPI<Machine>(`/v1/machines/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  getTelemetry: (token: string, id: string, params?: URLSearchParams) =>
    fetchAPI<{ machine_id: string; data: Telemetry[] }>(
      `/v1/machines/${id}/telemetry${params ? `?${params}` : ""}`,
      { token }
    ),

  sendCommand: (
    token: string,
    id: string,
    command: MachineCommand,
    parameters?: Record<string, unknown>
  ) =>
    fetchAPI<CommandResponse>(`/v1/machines/${id}/command`, {
      method: "POST",
      body: JSON.stringify({ command, parameters }),
      token,
    }),

  delete: (token: string, id: string) =>
    fetchAPI<{ message: string }>(`/v1/machines/${id}`, {
      method: "DELETE",
      token,
    }),
};

// Layouts API (proxied to viz-engine)
export const layoutsAPI = {
  getActive: (token: string) =>
    fetchAPI<FactoryLayout>("/v1/layouts/active", { token }),

  list: (token: string) =>
    fetchAPI<FactoryLayout[]>("/v1/layouts", { token }),

  get: (token: string, id: string) =>
    fetchAPI<FactoryLayout>(`/v1/layouts/${id}`, { token }),

  update: (token: string, id: string, data: Partial<FactoryLayout>) =>
    fetchAPI<FactoryLayout>(`/v1/layouts/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
      token,
    }),
};

// Models API (proxied to viz-engine)
export const modelsAPI = {
  list: (token: string) =>
    fetchAPI<MachineModel[]>("/v1/models", { token }),

  upload: (token: string, file: File) => {
    const formData = new FormData();
    formData.append("file", file);

    return fetch(`${API_BASE_URL}/v1/models/upload`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
      body: formData,
    }).then((res) => {
      if (!res.ok) throw new Error(`Upload failed: ${res.status}`);
      return res.json() as Promise<MachineModel>;
    });
  },
};

// Types
export interface FactoryLayout {
  id: string;
  tenant_id: string;
  name: string;
  description?: string;
  machine_positions: LayoutMachinePosition[];
  camera_presets: LayoutCameraPreset[];
  grid_settings: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface LayoutMachinePosition {
  machine_id: string;
  position: { x: number; y: number; z: number };
  rotation: { x: number; y: number; z: number };
  scale: number;
  visible: boolean;
}

export interface LayoutCameraPreset {
  name: string;
  position: { x: number; y: number; z: number };
  target: { x: number; y: number; z: number };
}

export interface MachineModel {
  id: string;
  machine_type: string;
  name: string;
  model_url: string;
  thumbnail_url?: string;
  bounding_box: Record<string, unknown>;
  scale: number;
  created_at: string;
  updated_at: string;
}

export interface ListResponse<T> {
  data: T[];
  total: number;
  limit: number;
  offset: number;
}

export interface Order {
  id: string;
  tenant_id: string;
  external_id?: string;
  customer_name: string;
  customer_email?: string;
  status: OrderStatus;
  priority: number;
  due_date?: string;
  total_amount?: number;
  currency: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export type OrderStatus =
  | "received"
  | "confirmed"
  | "in_production"
  | "quality_check"
  | "ready"
  | "shipped"
  | "delivered"
  | "cancelled";

export interface CreateOrderRequest {
  external_id?: string;
  customer_name: string;
  customer_email?: string;
  priority?: number;
  due_date?: string;
  total_amount?: number;
  currency?: string;
  metadata?: Record<string, unknown>;
}

export interface UpdateOrderRequest {
  customer_name?: string;
  customer_email?: string;
  status?: OrderStatus;
  priority?: number;
  due_date?: string;
  total_amount?: number;
  currency?: string;
  metadata?: Record<string, unknown>;
}

export interface Task {
  id: string;
  tenant_id: string;
  order_id?: string;
  order_item_id?: string;
  machine_id?: string;
  assigned_user_id?: string;
  title: string;
  description?: string;
  status: TaskStatus;
  priority: number;
  estimated_minutes?: number;
  actual_minutes?: number;
  kanban_position: number;
  started_at?: string;
  completed_at?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export type TaskStatus =
  | "backlog"
  | "queued"
  | "in_progress"
  | "quality_check"
  | "completed"
  | "blocked";

export interface KanbanBoard {
  columns: Record<TaskStatus, Task[]>;
}

export interface CreateTaskRequest {
  order_id?: string;
  order_item_id?: string;
  machine_id?: string;
  assigned_user_id?: string;
  title: string;
  description?: string;
  priority?: number;
  estimated_minutes?: number;
  metadata?: Record<string, unknown>;
}

export interface UpdateTaskRequest {
  order_id?: string;
  order_item_id?: string;
  machine_id?: string;
  assigned_user_id?: string;
  title?: string;
  description?: string;
  status?: TaskStatus;
  priority?: number;
  estimated_minutes?: number;
  actual_minutes?: number;
  metadata?: Record<string, unknown>;
}

export interface Machine {
  id: string;
  tenant_id: string;
  name: string;
  code: string;
  type: string;
  description?: string;
  status: MachineStatus;
  mqtt_topic?: string;
  location?: string;
  specifications?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
  last_heartbeat?: string;
  created_at: string;
  updated_at: string;
}

export type MachineStatus =
  | "offline"
  | "online"
  | "idle"
  | "running"
  | "maintenance"
  | "error";

export interface CreateMachineRequest {
  name: string;
  code: string;
  type: string;
  description?: string;
  mqtt_topic?: string;
  location?: string;
  specifications?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface UpdateMachineRequest {
  name?: string;
  code?: string;
  type?: string;
  description?: string;
  status?: MachineStatus;
  mqtt_topic?: string;
  location?: string;
  specifications?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface Telemetry {
  id: string;
  tenant_id: string;
  machine_id: string;
  timestamp: string;
  metric_type: string;
  value: number;
  unit: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

// Machine command types
export type MachineCommand =
  | "start_job"
  | "pause"
  | "resume"
  | "stop"
  | "home"
  | "calibrate"
  | "emergency_stop"
  | "preheat"
  | "cooldown"
  | "load_file"
  | "unload_file"
  | "set_origin"
  | "probe";

export interface CommandResponse {
  command_id: string;
  machine_id: string;
  command: MachineCommand;
  status: "pending" | "sent" | "acknowledged" | "failed";
  issued_at: string;
  message?: string;
}

// ============ OEE / Analytics ============

export interface OEESnapshot {
  id: string;
  tenant_id: string;
  machine_id: string;
  snapshot_date: string;
  planned_minutes: number;
  downtime_minutes: number;
  run_minutes: number;
  tasks_completed: number;
  tasks_failed: number;
  tasks_total: number;
  availability: number;
  performance: number;
  quality: number;
  oee: number;
  created_at: string;
  updated_at: string;
}

export const analyticsAPI = {
  getOEE: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<OEESnapshot>>(
      `/v1/analytics/oee${params ? `?${params}` : ""}`,
      { token }
    ),

  getOEESummary: (token: string, params?: URLSearchParams) =>
    fetchAPI<OEESnapshot[]>(
      `/v1/analytics/oee/summary${params ? `?${params}` : ""}`,
      { token }
    ),

  computeOEE: (token: string, data: { machine_id?: string; date?: string }) =>
    fetchAPI<OEESnapshot[]>("/v1/analytics/oee/compute", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),
};

// ============ Maintenance ============

export interface MaintenanceSchedule {
  id: string;
  tenant_id: string;
  machine_id: string;
  name: string;
  description?: string;
  trigger_type: "calendar" | "runtime_hours" | "cycle_count" | "condition";
  priority: number;
  interval_days?: number;
  interval_hours?: number;
  interval_cycles?: number;
  condition_metric?: string;
  condition_threshold?: number;
  last_done_at?: string;
  next_due_at?: string;
  next_due_hours?: number;
  assigned_to?: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface MaintenanceWorkOrder {
  id: string;
  tenant_id: string;
  schedule_id?: string;
  machine_id: string;
  work_order_number: string;
  title: string;
  description?: string;
  status: "scheduled" | "overdue" | "in_progress" | "completed" | "cancelled";
  priority: number;
  assigned_to?: string;
  checklist?: unknown[];
  scheduled_at?: string;
  started_at?: string;
  completed_at?: string;
  due_at?: string;
  notes?: string;
  created_at: string;
  updated_at: string;
}

export const maintenanceAPI = {
  listSchedules: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<MaintenanceSchedule>>(
      `/v1/maintenance/schedules${params ? `?${params}` : ""}`,
      { token }
    ),

  createSchedule: (token: string, data: Partial<MaintenanceSchedule>) =>
    fetchAPI<MaintenanceSchedule>("/v1/maintenance/schedules", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  updateSchedule: (token: string, id: string, data: Partial<MaintenanceSchedule>) =>
    fetchAPI<MaintenanceSchedule>(`/v1/maintenance/schedules/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  deleteSchedule: (token: string, id: string) =>
    fetchAPI<{ message: string }>(`/v1/maintenance/schedules/${id}`, {
      method: "DELETE",
      token,
    }),

  listWorkOrders: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<MaintenanceWorkOrder>>(
      `/v1/maintenance/work-orders${params ? `?${params}` : ""}`,
      { token }
    ),

  createWorkOrder: (token: string, data: Partial<MaintenanceWorkOrder>) =>
    fetchAPI<MaintenanceWorkOrder>("/v1/maintenance/work-orders", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  updateWorkOrder: (token: string, id: string, data: Partial<MaintenanceWorkOrder>) =>
    fetchAPI<MaintenanceWorkOrder>(`/v1/maintenance/work-orders/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  completeWorkOrder: (token: string, id: string, notes?: string) =>
    fetchAPI<MaintenanceWorkOrder>(`/v1/maintenance/work-orders/${id}/complete`, {
      method: "POST",
      body: JSON.stringify({ notes }),
      token,
    }),
};

// ============ Products ============

export interface ProductDefinition {
  id: string;
  tenant_id: string;
  sku: string;
  name: string;
  version: string;
  category: "3d_print" | "cnc_part" | "laser_cut" | "assembly" | "other";
  description?: string;
  cad_file_url?: string;
  parametric_specs?: Record<string, unknown>;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface BOMItem {
  id: string;
  tenant_id: string;
  product_definition_id: string;
  material_name: string;
  material_code?: string;
  quantity: number;
  unit: string;
  estimated_cost?: number;
  currency: string;
  supplier?: string;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export const productsAPI = {
  list: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<ProductDefinition>>(
      `/v1/products${params ? `?${params}` : ""}`,
      { token }
    ),

  get: (token: string, id: string) =>
    fetchAPI<ProductDefinition>(`/v1/products/${id}`, { token }),

  create: (token: string, data: Partial<ProductDefinition>) =>
    fetchAPI<ProductDefinition>("/v1/products", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  update: (token: string, id: string, data: Partial<ProductDefinition>) =>
    fetchAPI<ProductDefinition>(`/v1/products/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  delete: (token: string, id: string) =>
    fetchAPI<{ message: string }>(`/v1/products/${id}`, {
      method: "DELETE",
      token,
    }),

  getBOM: (token: string, id: string) =>
    fetchAPI<BOMItem[]>(`/v1/products/${id}/bom`, { token }),

  addBOMItem: (token: string, id: string, data: Partial<BOMItem>) =>
    fetchAPI<BOMItem>(`/v1/products/${id}/bom/items`, {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  deleteBOMItem: (token: string, productId: string, itemId: string) =>
    fetchAPI<{ message: string }>(`/v1/products/${productId}/bom/items/${itemId}`, {
      method: "DELETE",
      token,
    }),
};

// ============ Genealogy ============

export interface ProductGenealogy {
  id: string;
  tenant_id: string;
  product_definition_id?: string;
  product_name?: string;
  product_sku?: string;
  serial_number?: string;
  lot_number?: string;
  quantity?: number;
  order_id?: string;
  order_item_id?: string;
  task_id?: string;
  machine_id?: string;
  inspection_id?: string;
  certificate_id?: string;
  quality_result?: string;
  status: "draft" | "in_progress" | "completed" | "sealed";
  sealed_at?: string;
  sealed_by?: string;
  birth_cert_hash?: string;
  birth_cert_url?: string;
  created_at: string;
  updated_at: string;
}

export const genealogyAPI = {
  list: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<ProductGenealogy>>(
      `/v1/genealogy${params ? `?${params}` : ""}`,
      { token }
    ),

  get: (token: string, id: string) =>
    fetchAPI<ProductGenealogy>(`/v1/genealogy/${id}`, { token }),

  create: (token: string, data: Partial<ProductGenealogy>) =>
    fetchAPI<ProductGenealogy>("/v1/genealogy", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  update: (token: string, id: string, data: Partial<ProductGenealogy>) =>
    fetchAPI<ProductGenealogy>(`/v1/genealogy/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  seal: (token: string, id: string) =>
    fetchAPI<ProductGenealogy>(`/v1/genealogy/${id}/seal`, {
      method: "POST",
      token,
    }),

  getTree: (token: string, id: string) =>
    fetchAPI<Record<string, unknown>>(`/v1/genealogy/${id}/tree`, { token }),
};

// ============ Work Instructions ============

export interface WorkInstruction {
  id: string;
  tenant_id: string;
  title: string;
  version: string;
  category: "setup" | "operation" | "safety" | "maintenance";
  description?: string;
  product_definition_id?: string;
  machine_type?: string;
  steps: WorkInstructionStep[];
  tools_required: string[];
  ppe_required: string[];
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface WorkInstructionStep {
  step_number: number;
  title: string;
  description?: string;
  media_url?: string;
  warning?: string;
  duration_minutes?: number;
}

export interface TaskWorkInstruction {
  id: string;
  task_id: string;
  work_instruction_id: string;
  step_acknowledgements: Record<string, { acknowledged_at: string; acknowledged_by: string }>;
  all_acknowledged: boolean;
  work_instruction?: WorkInstruction;
  created_at: string;
  updated_at: string;
}

export const workInstructionsAPI = {
  list: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<WorkInstruction>>(
      `/v1/work-instructions${params ? `?${params}` : ""}`,
      { token }
    ),

  get: (token: string, id: string) =>
    fetchAPI<WorkInstruction>(`/v1/work-instructions/${id}`, { token }),

  create: (token: string, data: Partial<WorkInstruction>) =>
    fetchAPI<WorkInstruction>("/v1/work-instructions", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  update: (token: string, id: string, data: Partial<WorkInstruction>) =>
    fetchAPI<WorkInstruction>(`/v1/work-instructions/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  delete: (token: string, id: string) =>
    fetchAPI<{ message: string }>(`/v1/work-instructions/${id}`, {
      method: "DELETE",
      token,
    }),

  getForTask: (token: string, taskId: string) =>
    fetchAPI<TaskWorkInstruction[]>(`/v1/tasks/${taskId}/work-instructions`, { token }),

  acknowledgeStep: (token: string, taskId: string, wiId: string, stepNumber: number) =>
    fetchAPI<{ message: string }>(
      `/v1/tasks/${taskId}/work-instructions/${wiId}/acknowledge`,
      {
        method: "POST",
        body: JSON.stringify({ step_number: stepNumber }),
        token,
      }
    ),
};

// ============ Inventory ============

export interface InventoryItem {
  id: string;
  tenant_id: string;
  sku: string;
  name: string;
  category?: string;
  description?: string;
  unit: string;
  quantity_on_hand: number;
  quantity_reserved: number;
  quantity_available: number;
  reorder_point: number;
  reorder_quantity: number;
  forgesight_id?: string;
  unit_cost?: number;
  currency: string;
  created_at: string;
  updated_at: string;
}

export const inventoryAPI = {
  list: (token: string, params?: URLSearchParams) =>
    fetchAPI<ListResponse<InventoryItem>>(
      `/v1/inventory${params ? `?${params}` : ""}`,
      { token }
    ),

  get: (token: string, id: string) =>
    fetchAPI<InventoryItem>(`/v1/inventory/${id}`, { token }),

  create: (token: string, data: Partial<InventoryItem>) =>
    fetchAPI<InventoryItem>("/v1/inventory", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  update: (token: string, id: string, data: Partial<InventoryItem>) =>
    fetchAPI<InventoryItem>(`/v1/inventory/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
      token,
    }),

  adjust: (token: string, id: string, data: { quantity: number; transaction_type: string; notes?: string }) =>
    fetchAPI<InventoryItem>(`/v1/inventory/${id}/adjust`, {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  getLowStock: (token: string) =>
    fetchAPI<InventoryItem[]>("/v1/inventory/low-stock", { token }),
};

// ============ SPC ============

export interface SPCControlLimit {
  id: string;
  machine_id: string;
  metric_type: string;
  mean: number;
  stddev: number;
  ucl: number;
  lcl: number;
  usl?: number;
  lsl?: number;
  sample_count: number;
  is_active: boolean;
}

export interface SPCViolation {
  id: string;
  machine_id: string;
  violation_type: "above_ucl" | "below_lcl" | "run_of_7" | "trend";
  metric_type: string;
  value: number;
  limit_value: number;
  detected_at: string;
  acknowledged: boolean;
  acknowledged_by?: string;
  notes?: string;
}

export const spcAPI = {
  getLimits: (token: string, params?: URLSearchParams) =>
    fetchAPI<SPCControlLimit[]>(
      `/v1/analytics/spc/limits${params ? `?${params}` : ""}`,
      { token }
    ),

  computeLimits: (token: string, data: { machine_id: string; metric_type: string; sample_days?: number }) =>
    fetchAPI<SPCControlLimit>("/v1/analytics/spc/limits/compute", {
      method: "POST",
      body: JSON.stringify(data),
      token,
    }),

  getChart: (token: string, params?: URLSearchParams) =>
    fetchAPI<{ data: Telemetry[]; limits?: SPCControlLimit }>(
      `/v1/analytics/spc/chart${params ? `?${params}` : ""}`,
      { token }
    ),

  getViolations: (token: string, params?: URLSearchParams) =>
    fetchAPI<SPCViolation[]>(
      `/v1/analytics/spc/violations${params ? `?${params}` : ""}`,
      { token }
    ),

  acknowledgeViolation: (token: string, id: string, notes?: string) =>
    fetchAPI<{ message: string }>(`/v1/analytics/spc/violations/${id}/acknowledge`, {
      method: "POST",
      body: JSON.stringify({ notes }),
      token,
    }),
};
