/**
 * Real-time event types for PravaraMES
 */

// Event type constants
export const EventTypes = {
  // Machine events
  MACHINE_STATUS_CHANGED: "machine.status_changed",
  MACHINE_HEARTBEAT: "machine.heartbeat",
  MACHINE_TELEMETRY_BATCH: "machine.telemetry_batch",
  MACHINE_COMMAND_SENT: "machine.command_sent",
  MACHINE_COMMAND_ACK: "machine.command_ack",
  MACHINE_CREATED: "machine.created",
  MACHINE_UPDATED: "machine.updated",
  MACHINE_DELETED: "machine.deleted",

  // Task events
  TASK_CREATED: "task.created",
  TASK_UPDATED: "task.updated",
  TASK_MOVED: "task.moved",
  TASK_ASSIGNED: "task.assigned",
  TASK_DELETED: "task.deleted",
  TASK_COMPLETED: "task.completed",
  TASK_JOB_STARTED: "task.job_started",
  TASK_JOB_COMPLETED: "task.job_completed",
  TASK_JOB_FAILED: "task.job_failed",
  TASK_BLOCKED: "task.blocked",

  // Order events
  ORDER_CREATED: "order.created",
  ORDER_UPDATED: "order.updated",
  ORDER_DELETED: "order.deleted",
  ORDER_STATUS: "order.status_changed",
  ORDER_ITEM_ADD: "order.item_added",

  // Notification events
  NOTIFICATION_ALERT: "notification.alert",
  NOTIFICATION_WARNING: "notification.warning",
  NOTIFICATION_INFO: "notification.info",
} as const;

export type EventType = (typeof EventTypes)[keyof typeof EventTypes];

// Channel namespaces
export const ChannelNamespaces = {
  MACHINES: "machines",
  TASKS: "tasks",
  ORDERS: "orders",
  TELEMETRY: "telemetry",
  NOTIFICATIONS: "notifications",
} as const;

export type ChannelNamespace =
  (typeof ChannelNamespaces)[keyof typeof ChannelNamespaces];

// Base event structure
export interface RealtimeEvent<T = unknown> {
  id: string;
  type: EventType;
  tenant_id: string;
  timestamp: string;
  data: T;
}

// Machine status data
export interface MachineStatusData {
  machine_id: string;
  machine_name: string;
  old_status?: string;
  new_status: string;
  updated_at: string;
}

// Machine heartbeat data
export interface MachineHeartbeatData {
  machine_id: string;
  last_heartbeat: string;
  is_online: boolean;
  current_job_id?: string;
  current_job_name?: string;
}

// Telemetry batch data
export interface TelemetryBatchData {
  machine_id: string;
  metrics: TelemetryMetric[];
  received_at: string;
}

export interface TelemetryMetric {
  type: string;
  value: number;
  unit: string;
  timestamp: string;
}

// Task move data
export interface TaskMoveData {
  task_id: string;
  task_title: string;
  old_status: string;
  new_status: string;
  old_position: number;
  new_position: number;
  moved_by: string;
  moved_at: string;
}

// Task assign data
export interface TaskAssignData {
  task_id: string;
  task_title: string;
  old_assignee?: string;
  new_assignee?: string;
  assignee_name?: string;
  assigned_by: string;
  assigned_at: string;
}

// Task job lifecycle data
export interface TaskJobData {
  task_id: string;
  task_title: string;
  command_id: string;
  machine_id: string;
  machine_name: string;
  command_type: string;
  status: "started" | "completed" | "failed";
  error_message?: string;
  timestamp: string;
  actual_minutes?: number;
}

// Order status data
export interface OrderStatusData {
  order_id: string;
  order_external_id?: string;
  old_status?: string;
  new_status: string;
  customer_name: string;
  updated_at: string;
}

// Notification data
export interface NotificationData {
  title: string;
  message: string;
  severity: "info" | "warning" | "error" | "critical";
  source: "machine" | "order" | "task" | "system";
  source_id?: string;
  action_url?: string;
  metadata?: Record<string, unknown>;
}

// Entity created data
export interface EntityCreatedData {
  entity_id: string;
  entity_type: string;
  name: string;
  created_by: string;
  created_at: string;
  metadata?: Record<string, unknown>;
}

// Entity updated data
export interface EntityUpdatedData {
  entity_id: string;
  entity_type: string;
  name: string;
  changed_fields?: string[];
  updated_by: string;
  updated_at: string;
  metadata?: Record<string, unknown>;
}

// Entity deleted data
export interface EntityDeletedData {
  entity_id: string;
  entity_type: string;
  name: string;
  deleted_by: string;
  deleted_at: string;
}

// Machine command data
export interface MachineCommandData {
  command_id: string;
  machine_id: string;
  command: string;
  parameters?: Record<string, unknown>;
  task_id?: string;
  order_id?: string;
  issued_by: string;
  issued_at: string;
}

// Machine command acknowledgment data
export interface MachineCommandAckData {
  command_id: string;
  machine_id: string;
  success: boolean;
  message?: string;
  acknowledged_at: string;
}

// Connection state
export type ConnectionState =
  | "connecting"
  | "connected"
  | "disconnected"
  | "error";

// Token response from API
export interface RealtimeTokenResponse {
  token: string;
  expires_at: number;
  url: string;
}
