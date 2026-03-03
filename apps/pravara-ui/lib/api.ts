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
