/**
 * Hook for real-time task updates with React Query integration
 */
"use client";

import { useEffect, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { subscribeTasks } from "@/lib/realtime/channels";
import type {
  TaskMoveData,
  TaskAssignData,
  EntityCreatedData,
  EntityUpdatedData,
  EntityDeletedData,
} from "@/lib/realtime/types";
import type { Task, KanbanBoard, ListResponse, TaskStatus } from "@/lib/api";
import { useRealtimeStore, selectIsConnected } from "@/stores/realtimeStore";

interface UseTaskUpdatesOptions {
  /** Called when a task is moved on the board */
  onMove?: (data: TaskMoveData) => void;
  /** Called when a task is assigned */
  onAssign?: (data: TaskAssignData) => void;
  /** Called when a new task is created */
  onCreate?: (data: EntityCreatedData) => void;
  /** Called when a task is updated */
  onUpdate?: (data: EntityUpdatedData) => void;
  /** Called when a task is deleted */
  onDelete?: (data: EntityDeletedData) => void;
  /** Called when a task is completed */
  onComplete?: (data: EntityUpdatedData) => void;
}

export function useTaskUpdates(options: UseTaskUpdatesOptions = {}) {
  const queryClient = useQueryClient();
  const isConnected = useRealtimeStore(selectIsConnected);

  // Handle task move event - update kanban board
  const handleMove = useCallback(
    (data: TaskMoveData) => {
      // Update kanban board cache
      queryClient.setQueryData<KanbanBoard>(["tasks", "board"], (old) => {
        if (!old) return old;

        const newColumns = { ...old.columns };

        // Remove from old column
        const oldColumn = newColumns[data.old_status as TaskStatus];
        if (oldColumn) {
          newColumns[data.old_status as TaskStatus] = oldColumn.filter(
            (task) => task.id !== data.task_id
          );
        }

        // Add to new column at correct position
        const newColumn = [...(newColumns[data.new_status as TaskStatus] || [])];
        const taskToMove = oldColumn?.find((t) => t.id === data.task_id);
        if (taskToMove) {
          const updatedTask = {
            ...taskToMove,
            status: data.new_status as TaskStatus,
            kanban_position: data.new_position,
            updated_at: data.moved_at,
          };

          // Insert at the correct position
          newColumn.splice(data.new_position, 0, updatedTask);

          // Reindex positions
          newColumn.forEach((task, index) => {
            task.kanban_position = index;
          });

          newColumns[data.new_status as TaskStatus] = newColumn;
        }

        return { columns: newColumns };
      });

      // Update list cache
      queryClient.setQueriesData<ListResponse<Task>>(
        { queryKey: ["tasks"] },
        (old) => {
          if (!old) return old;
          return {
            ...old,
            data: old.data.map((task) =>
              task.id === data.task_id
                ? {
                    ...task,
                    status: data.new_status as TaskStatus,
                    kanban_position: data.new_position,
                    updated_at: data.moved_at,
                  }
                : task
            ),
          };
        }
      );

      options.onMove?.(data);
    },
    [queryClient, options]
  );

  // Handle task assign event
  const handleAssign = useCallback(
    (data: TaskAssignData) => {
      const updates: Partial<Task> = {
        assigned_user_id: data.new_assignee,
        updated_at: data.assigned_at,
      };

      // Update kanban board
      queryClient.setQueryData<KanbanBoard>(["tasks", "board"], (old) => {
        if (!old) return old;
        const newColumns = { ...old.columns };
        for (const status of Object.keys(newColumns) as TaskStatus[]) {
          newColumns[status] = newColumns[status].map((task) =>
            task.id === data.task_id ? { ...task, ...updates } : task
          );
        }
        return { columns: newColumns };
      });

      // Update list cache
      queryClient.setQueriesData<ListResponse<Task>>(
        { queryKey: ["tasks"] },
        (old) => {
          if (!old) return old;
          return {
            ...old,
            data: old.data.map((task) =>
              task.id === data.task_id ? { ...task, ...updates } : task
            ),
          };
        }
      );

      // Update individual query
      queryClient.setQueryData<Task>(["tasks", data.task_id], (old) => {
        if (!old) return old;
        return { ...old, ...updates };
      });

      options.onAssign?.(data);
    },
    [queryClient, options]
  );

  // Handle create event - invalidate to refetch
  const handleCreate = useCallback(
    (data: EntityCreatedData) => {
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
      options.onCreate?.(data);
    },
    [queryClient, options]
  );

  // Handle update event
  const handleUpdate = useCallback(
    (data: EntityUpdatedData) => {
      queryClient.invalidateQueries({ queryKey: ["tasks", data.entity_id] });
      queryClient.invalidateQueries({ queryKey: ["tasks", "board"] });
      options.onUpdate?.(data);
    },
    [queryClient, options]
  );

  // Handle delete event
  const handleDelete = useCallback(
    (data: EntityDeletedData) => {
      // Remove from kanban board
      queryClient.setQueryData<KanbanBoard>(["tasks", "board"], (old) => {
        if (!old) return old;
        const newColumns = { ...old.columns };
        for (const status of Object.keys(newColumns) as TaskStatus[]) {
          newColumns[status] = newColumns[status].filter(
            (task) => task.id !== data.entity_id
          );
        }
        return { columns: newColumns };
      });

      // Remove from list cache
      queryClient.setQueriesData<ListResponse<Task>>(
        { queryKey: ["tasks"] },
        (old) => {
          if (!old) return old;
          return {
            ...old,
            data: old.data.filter((task) => task.id !== data.entity_id),
            total: old.total - 1,
          };
        }
      );

      queryClient.removeQueries({ queryKey: ["tasks", data.entity_id] });
      options.onDelete?.(data);
    },
    [queryClient, options]
  );

  // Handle complete event
  const handleComplete = useCallback(
    (data: EntityUpdatedData) => {
      // Invalidate to get fresh data with completion timestamp
      queryClient.invalidateQueries({ queryKey: ["tasks", data.entity_id] });
      queryClient.invalidateQueries({ queryKey: ["tasks", "board"] });
      options.onComplete?.(data);
    },
    [queryClient, options]
  );

  // Subscribe to task events
  useEffect(() => {
    if (!isConnected) return;

    const unsubscribe = subscribeTasks({
      onMove: handleMove,
      onAssign: handleAssign,
      onCreate: handleCreate,
      onUpdate: handleUpdate,
      onDelete: handleDelete,
      onComplete: handleComplete,
    });

    return () => {
      unsubscribe();
    };
  }, [
    isConnected,
    handleMove,
    handleAssign,
    handleCreate,
    handleUpdate,
    handleDelete,
    handleComplete,
  ]);

  return {
    isConnected,
  };
}
