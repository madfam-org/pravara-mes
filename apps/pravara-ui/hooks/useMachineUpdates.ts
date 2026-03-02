/**
 * Hook for real-time machine updates with React Query integration
 */
"use client";

import { useEffect, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { subscribeMachines } from "@/lib/realtime/channels";
import type {
  MachineStatusData,
  MachineHeartbeatData,
  MachineCommandAckData,
  EntityCreatedData,
  EntityUpdatedData,
  EntityDeletedData,
} from "@/lib/realtime/types";
import type { Machine, ListResponse } from "@/lib/api";
import { useRealtimeStore, selectIsConnected } from "@/stores/realtimeStore";

interface UseMachineUpdatesOptions {
  /** Called when a machine status changes */
  onStatusChange?: (data: MachineStatusData) => void;
  /** Called when a machine heartbeat is received */
  onHeartbeat?: (data: MachineHeartbeatData) => void;
  /** Called when a command acknowledgment is received */
  onCommandAck?: (data: MachineCommandAckData) => void;
  /** Called when a new machine is created */
  onCreate?: (data: EntityCreatedData) => void;
  /** Called when a machine is updated */
  onUpdate?: (data: EntityUpdatedData) => void;
  /** Called when a machine is deleted */
  onDelete?: (data: EntityDeletedData) => void;
}

export function useMachineUpdates(options: UseMachineUpdatesOptions = {}) {
  const queryClient = useQueryClient();
  const isConnected = useRealtimeStore(selectIsConnected);

  // Update machine in React Query cache
  const updateMachineInCache = useCallback(
    (machineId: string, updates: Partial<Machine>) => {
      // Update in list queries
      queryClient.setQueriesData<ListResponse<Machine>>(
        { queryKey: ["machines"] },
        (old) => {
          if (!old) return old;
          return {
            ...old,
            data: old.data.map((machine) =>
              machine.id === machineId ? { ...machine, ...updates } : machine
            ),
          };
        }
      );

      // Update individual machine query
      queryClient.setQueryData<Machine>(["machines", machineId], (old) => {
        if (!old) return old;
        return { ...old, ...updates };
      });
    },
    [queryClient]
  );

  // Handle status change event
  const handleStatusChange = useCallback(
    (data: MachineStatusData) => {
      updateMachineInCache(data.machine_id, {
        status: data.new_status as Machine["status"],
        updated_at: data.updated_at,
      });
      options.onStatusChange?.(data);
    },
    [updateMachineInCache, options]
  );

  // Handle heartbeat event
  const handleHeartbeat = useCallback(
    (data: MachineHeartbeatData) => {
      updateMachineInCache(data.machine_id, {
        last_heartbeat: data.last_heartbeat,
        status: data.is_online ? "online" : "offline",
      });
      options.onHeartbeat?.(data);
    },
    [updateMachineInCache, options]
  );

  // Handle create event - invalidate list queries to refetch
  const handleCreate = useCallback(
    (data: EntityCreatedData) => {
      queryClient.invalidateQueries({ queryKey: ["machines"] });
      options.onCreate?.(data);
    },
    [queryClient, options]
  );

  // Handle update event
  const handleUpdate = useCallback(
    (data: EntityUpdatedData) => {
      // For complex updates, invalidate to refetch
      queryClient.invalidateQueries({ queryKey: ["machines", data.entity_id] });
      options.onUpdate?.(data);
    },
    [queryClient, options]
  );

  // Handle delete event
  const handleDelete = useCallback(
    (data: EntityDeletedData) => {
      // Remove from list cache
      queryClient.setQueriesData<ListResponse<Machine>>(
        { queryKey: ["machines"] },
        (old) => {
          if (!old) return old;
          return {
            ...old,
            data: old.data.filter((machine) => machine.id !== data.entity_id),
            total: old.total - 1,
          };
        }
      );

      // Remove individual query
      queryClient.removeQueries({ queryKey: ["machines", data.entity_id] });
      options.onDelete?.(data);
    },
    [queryClient, options]
  );

  // Handle command ack event
  const handleCommandAck = useCallback(
    (data: MachineCommandAckData) => {
      // Just pass through to callback - the control panel handles display
      options.onCommandAck?.(data);
    },
    [options]
  );

  // Subscribe to machine events
  useEffect(() => {
    if (!isConnected) return;

    const unsubscribe = subscribeMachines({
      onStatusChange: handleStatusChange,
      onHeartbeat: handleHeartbeat,
      onCommandAck: handleCommandAck,
      onCreate: handleCreate,
      onUpdate: handleUpdate,
      onDelete: handleDelete,
    });

    return () => {
      unsubscribe();
    };
  }, [
    isConnected,
    handleStatusChange,
    handleHeartbeat,
    handleCommandAck,
    handleCreate,
    handleUpdate,
    handleDelete,
  ]);

  return {
    isConnected,
  };
}
