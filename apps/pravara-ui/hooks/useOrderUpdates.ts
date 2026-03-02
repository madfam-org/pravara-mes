/**
 * Hook for real-time order updates with React Query integration
 */
"use client";

import { useEffect, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { subscribeOrders } from "@/lib/realtime/channels";
import type {
  OrderStatusData,
  EntityCreatedData,
  EntityUpdatedData,
  EntityDeletedData,
} from "@/lib/realtime/types";
import type { Order, ListResponse, OrderStatus } from "@/lib/api";
import { useRealtimeStore, selectIsConnected } from "@/stores/realtimeStore";

interface UseOrderUpdatesOptions {
  /** Called when an order status changes */
  onStatusChange?: (data: OrderStatusData) => void;
  /** Called when a new order is created */
  onCreate?: (data: EntityCreatedData) => void;
  /** Called when an order is updated */
  onUpdate?: (data: EntityUpdatedData) => void;
  /** Called when an order is deleted */
  onDelete?: (data: EntityDeletedData) => void;
}

export function useOrderUpdates(options: UseOrderUpdatesOptions = {}) {
  const queryClient = useQueryClient();
  const isConnected = useRealtimeStore(selectIsConnected);

  // Update order in React Query cache
  const updateOrderInCache = useCallback(
    (orderId: string, updates: Partial<Order>) => {
      // Update in list queries
      queryClient.setQueriesData<ListResponse<Order>>(
        { queryKey: ["orders"] },
        (old) => {
          if (!old) return old;
          return {
            ...old,
            data: old.data.map((order) =>
              order.id === orderId ? { ...order, ...updates } : order
            ),
          };
        }
      );

      // Update individual order query
      queryClient.setQueryData<Order>(["orders", orderId], (old) => {
        if (!old) return old;
        return { ...old, ...updates };
      });
    },
    [queryClient]
  );

  // Handle status change event
  const handleStatusChange = useCallback(
    (data: OrderStatusData) => {
      updateOrderInCache(data.order_id, {
        status: data.new_status as OrderStatus,
        updated_at: data.updated_at,
      });
      options.onStatusChange?.(data);
    },
    [updateOrderInCache, options]
  );

  // Handle create event - invalidate list queries to refetch
  const handleCreate = useCallback(
    (data: EntityCreatedData) => {
      queryClient.invalidateQueries({ queryKey: ["orders"] });
      options.onCreate?.(data);
    },
    [queryClient, options]
  );

  // Handle update event
  const handleUpdate = useCallback(
    (data: EntityUpdatedData) => {
      // For complex updates, invalidate to refetch
      queryClient.invalidateQueries({ queryKey: ["orders", data.entity_id] });
      options.onUpdate?.(data);
    },
    [queryClient, options]
  );

  // Handle delete event
  const handleDelete = useCallback(
    (data: EntityDeletedData) => {
      // Remove from list cache
      queryClient.setQueriesData<ListResponse<Order>>(
        { queryKey: ["orders"] },
        (old) => {
          if (!old) return old;
          return {
            ...old,
            data: old.data.filter((order) => order.id !== data.entity_id),
            total: old.total - 1,
          };
        }
      );

      // Remove individual query
      queryClient.removeQueries({ queryKey: ["orders", data.entity_id] });
      options.onDelete?.(data);
    },
    [queryClient, options]
  );

  // Subscribe to order events
  useEffect(() => {
    if (!isConnected) return;

    const unsubscribe = subscribeOrders({
      onStatusChange: handleStatusChange,
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
    handleCreate,
    handleUpdate,
    handleDelete,
  ]);

  return {
    isConnected,
  };
}
