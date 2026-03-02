/**
 * Centralized status configurations for consistent styling across the application
 */

import { Activity, Wifi, WifiOff } from "lucide-react";

// =============================================================================
// Machine Status
// =============================================================================

export type MachineStatusType = "offline" | "online" | "idle" | "running" | "maintenance" | "error";

export const MACHINE_STATUS_CONFIG: Record<
  MachineStatusType,
  {
    variant: "default" | "secondary" | "destructive" | "outline" | "success" | "warning" | "error";
    icon: typeof Wifi;
    label: string;
    description: string;
  }
> = {
  offline: {
    variant: "secondary",
    icon: WifiOff,
    label: "Offline",
    description: "Machine is not connected",
  },
  online: {
    variant: "success",
    icon: Wifi,
    label: "Online",
    description: "Machine is connected and ready",
  },
  idle: {
    variant: "warning",
    icon: Activity,
    label: "Idle",
    description: "Machine is connected but not producing",
  },
  running: {
    variant: "default",
    icon: Activity,
    label: "Running",
    description: "Machine is actively producing",
  },
  maintenance: {
    variant: "warning",
    icon: Activity,
    label: "Maintenance",
    description: "Machine is under maintenance",
  },
  error: {
    variant: "error",
    icon: Activity,
    label: "Error",
    description: "Machine has encountered an error",
  },
};

export const MACHINE_TYPES = [
  { value: "cnc", label: "CNC" },
  { value: "laser", label: "Laser" },
  { value: "3d_printer", label: "3D Printer" },
  { value: "injection", label: "Injection" },
  { value: "assembly", label: "Assembly" },
  { value: "other", label: "Other" },
] as const;

// =============================================================================
// Order Status
// =============================================================================

export type OrderStatusType =
  | "received"
  | "confirmed"
  | "in_production"
  | "quality_check"
  | "ready"
  | "shipped"
  | "delivered"
  | "cancelled";

export const ORDER_STATUS_CONFIG: Record<
  OrderStatusType,
  {
    label: string;
    colorClass: string;
    description: string;
  }
> = {
  received: {
    label: "Received",
    colorClass: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
    description: "Order has been received",
  },
  confirmed: {
    label: "Confirmed",
    colorClass: "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400",
    description: "Order has been confirmed",
  },
  in_production: {
    label: "In Production",
    colorClass: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400",
    description: "Order is being produced",
  },
  quality_check: {
    label: "Quality Check",
    colorClass: "bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400",
    description: "Order is being quality checked",
  },
  ready: {
    label: "Ready",
    colorClass: "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400",
    description: "Order is ready for shipment",
  },
  shipped: {
    label: "Shipped",
    colorClass: "bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400",
    description: "Order has been shipped",
  },
  delivered: {
    label: "Delivered",
    colorClass: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",
    description: "Order has been delivered",
  },
  cancelled: {
    label: "Cancelled",
    colorClass: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",
    description: "Order has been cancelled",
  },
};

export const ORDER_STATUS_OPTIONS = Object.entries(ORDER_STATUS_CONFIG).map(([value, config]) => ({
  value,
  label: config.label,
}));

// =============================================================================
// Task Status (Kanban)
// =============================================================================

export type TaskStatusType =
  | "backlog"
  | "queued"
  | "in_progress"
  | "quality_check"
  | "completed"
  | "blocked";

export const TASK_STATUS_CONFIG: Record<
  TaskStatusType,
  {
    label: string;
    colorClass: string;
    cssVar: string;
    description: string;
  }
> = {
  backlog: {
    label: "Backlog",
    colorClass: "bg-blue-500",
    cssVar: "var(--kanban-backlog)",
    description: "Task is in the backlog",
  },
  queued: {
    label: "Queued",
    colorClass: "bg-purple-500",
    cssVar: "var(--kanban-queued)",
    description: "Task is queued for work",
  },
  in_progress: {
    label: "In Progress",
    colorClass: "bg-yellow-500",
    cssVar: "var(--kanban-in-progress)",
    description: "Task is being worked on",
  },
  quality_check: {
    label: "Quality Check",
    colorClass: "bg-cyan-500",
    cssVar: "var(--kanban-quality-check)",
    description: "Task is being quality checked",
  },
  completed: {
    label: "Completed",
    colorClass: "bg-green-500",
    cssVar: "var(--kanban-completed)",
    description: "Task has been completed",
  },
  blocked: {
    label: "Blocked",
    colorClass: "bg-red-500",
    cssVar: "var(--kanban-blocked)",
    description: "Task is blocked",
  },
};

export const TASK_STATUS_OPTIONS = Object.entries(TASK_STATUS_CONFIG).map(([value, config]) => ({
  value,
  label: config.label,
}));

// =============================================================================
// Priority
// =============================================================================

export const PRIORITY_CONFIG: Record<
  number,
  {
    label: string;
    colorClass: string;
    description: string;
  }
> = {
  1: {
    label: "Low",
    colorClass: "text-gray-500",
    description: "Low priority task",
  },
  2: {
    label: "Normal",
    colorClass: "text-blue-500",
    description: "Normal priority task",
  },
  3: {
    label: "High",
    colorClass: "text-orange-500",
    description: "High priority task",
  },
  4: {
    label: "Urgent",
    colorClass: "text-red-500",
    description: "Urgent priority task",
  },
};

export const PRIORITY_OPTIONS = Object.entries(PRIORITY_CONFIG).map(([value, config]) => ({
  value,
  label: config.label,
  numValue: parseInt(value, 10),
}));

// =============================================================================
// Helper Functions
// =============================================================================

export function getMachineStatusConfig(status: string) {
  return MACHINE_STATUS_CONFIG[status as MachineStatusType] || MACHINE_STATUS_CONFIG.offline;
}

export function getOrderStatusConfig(status: string) {
  return ORDER_STATUS_CONFIG[status as OrderStatusType] || ORDER_STATUS_CONFIG.received;
}

export function getTaskStatusConfig(status: string) {
  return TASK_STATUS_CONFIG[status as TaskStatusType] || TASK_STATUS_CONFIG.backlog;
}

export function getPriorityConfig(priority: number) {
  return PRIORITY_CONFIG[priority] || PRIORITY_CONFIG[2];
}
