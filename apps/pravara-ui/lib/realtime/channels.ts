/**
 * Channel subscription helpers for PravaraMES real-time events
 */
import { realtimeClient, EventListener } from "./client";
import {
  ChannelNamespaces,
  EventTypes,
  RealtimeEvent,
  MachineStatusData,
  MachineHeartbeatData,
  TelemetryBatchData,
  TaskMoveData,
  TaskAssignData,
  OrderStatusData,
  NotificationData,
  EntityCreatedData,
  EntityUpdatedData,
  EntityDeletedData,
} from "./types";

/**
 * Subscribe to machine-related events
 */
export function subscribeMachines(
  handlers: {
    onStatusChange?: (data: MachineStatusData) => void;
    onHeartbeat?: (data: MachineHeartbeatData) => void;
    onCreate?: (data: EntityCreatedData) => void;
    onUpdate?: (data: EntityUpdatedData) => void;
    onDelete?: (data: EntityDeletedData) => void;
  }
): () => void {
  const listener: EventListener = (event: RealtimeEvent) => {
    switch (event.type) {
      case EventTypes.MACHINE_STATUS_CHANGED:
        handlers.onStatusChange?.(event.data as MachineStatusData);
        break;
      case EventTypes.MACHINE_HEARTBEAT:
        handlers.onHeartbeat?.(event.data as MachineHeartbeatData);
        break;
      case EventTypes.MACHINE_CREATED:
        handlers.onCreate?.(event.data as EntityCreatedData);
        break;
      case EventTypes.MACHINE_UPDATED:
        handlers.onUpdate?.(event.data as EntityUpdatedData);
        break;
      case EventTypes.MACHINE_DELETED:
        handlers.onDelete?.(event.data as EntityDeletedData);
        break;
    }
  };

  return realtimeClient.subscribe(ChannelNamespaces.MACHINES, listener);
}

/**
 * Subscribe to telemetry events
 */
export function subscribeTelemetry(
  onBatch: (data: TelemetryBatchData) => void
): () => void {
  const listener: EventListener = (event: RealtimeEvent) => {
    if (event.type === EventTypes.MACHINE_TELEMETRY_BATCH) {
      onBatch(event.data as TelemetryBatchData);
    }
  };

  return realtimeClient.subscribe(ChannelNamespaces.TELEMETRY, listener);
}

/**
 * Subscribe to task-related events
 */
export function subscribeTasks(
  handlers: {
    onMove?: (data: TaskMoveData) => void;
    onAssign?: (data: TaskAssignData) => void;
    onCreate?: (data: EntityCreatedData) => void;
    onUpdate?: (data: EntityUpdatedData) => void;
    onDelete?: (data: EntityDeletedData) => void;
    onComplete?: (data: EntityUpdatedData) => void;
  }
): () => void {
  const listener: EventListener = (event: RealtimeEvent) => {
    switch (event.type) {
      case EventTypes.TASK_MOVED:
        handlers.onMove?.(event.data as TaskMoveData);
        break;
      case EventTypes.TASK_ASSIGNED:
        handlers.onAssign?.(event.data as TaskAssignData);
        break;
      case EventTypes.TASK_CREATED:
        handlers.onCreate?.(event.data as EntityCreatedData);
        break;
      case EventTypes.TASK_UPDATED:
        handlers.onUpdate?.(event.data as EntityUpdatedData);
        break;
      case EventTypes.TASK_DELETED:
        handlers.onDelete?.(event.data as EntityDeletedData);
        break;
      case EventTypes.TASK_COMPLETED:
        handlers.onComplete?.(event.data as EntityUpdatedData);
        break;
    }
  };

  return realtimeClient.subscribe(ChannelNamespaces.TASKS, listener);
}

/**
 * Subscribe to order-related events
 */
export function subscribeOrders(
  handlers: {
    onStatusChange?: (data: OrderStatusData) => void;
    onCreate?: (data: EntityCreatedData) => void;
    onUpdate?: (data: EntityUpdatedData) => void;
    onDelete?: (data: EntityDeletedData) => void;
  }
): () => void {
  const listener: EventListener = (event: RealtimeEvent) => {
    switch (event.type) {
      case EventTypes.ORDER_STATUS:
        handlers.onStatusChange?.(event.data as OrderStatusData);
        break;
      case EventTypes.ORDER_CREATED:
        handlers.onCreate?.(event.data as EntityCreatedData);
        break;
      case EventTypes.ORDER_UPDATED:
        handlers.onUpdate?.(event.data as EntityUpdatedData);
        break;
      case EventTypes.ORDER_DELETED:
        handlers.onDelete?.(event.data as EntityDeletedData);
        break;
    }
  };

  return realtimeClient.subscribe(ChannelNamespaces.ORDERS, listener);
}

/**
 * Subscribe to notification events
 */
export function subscribeNotifications(
  onNotification: (data: NotificationData) => void
): () => void {
  const listener: EventListener = (event: RealtimeEvent) => {
    if (
      event.type === EventTypes.NOTIFICATION_ALERT ||
      event.type === EventTypes.NOTIFICATION_WARNING ||
      event.type === EventTypes.NOTIFICATION_INFO
    ) {
      onNotification(event.data as NotificationData);
    }
  };

  return realtimeClient.subscribe(ChannelNamespaces.NOTIFICATIONS, listener);
}
