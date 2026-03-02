"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ordersAPI, type Order, type CreateOrderRequest, type UpdateOrderRequest } from "@/lib/api";
import { toast } from "@/lib/hooks/use-toast";

export function useCreateOrder() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, data }: { token: string; data: CreateOrderRequest }) =>
      ordersAPI.create(token, data),
    onMutate: async () => {
      await queryClient.cancelQueries({ queryKey: ["orders"] });
    },
    onError: (error: Error) => {
      toast({
        title: "Failed to create order",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Order created",
        description: "The order has been created successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["orders"] });
    },
  });
}

export function useUpdateOrder() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, id, data }: { token: string; id: string; data: UpdateOrderRequest }) =>
      ordersAPI.update(token, id, data),
    onMutate: async ({ id, data }) => {
      await queryClient.cancelQueries({ queryKey: ["orders"] });
      await queryClient.cancelQueries({ queryKey: ["orders", id] });

      const previousOrders = queryClient.getQueryData(["orders"]);
      const previousOrder = queryClient.getQueryData(["orders", id]);

      // Optimistically update the order
      queryClient.setQueryData(["orders", id], (old: Order | undefined) => {
        if (!old) return old;
        return { ...old, ...data };
      });

      return { previousOrders, previousOrder };
    },
    onError: (error: Error, variables, context) => {
      if (context?.previousOrders) {
        queryClient.setQueryData(["orders"], context.previousOrders);
      }
      if (context?.previousOrder) {
        queryClient.setQueryData(["orders", variables.id], context.previousOrder);
      }
      toast({
        title: "Failed to update order",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Order updated",
        description: "The order has been updated successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["orders"] });
    },
  });
}

export function useDeleteOrder() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, id }: { token: string; id: string }) =>
      ordersAPI.delete(token, id),
    onMutate: async ({ id }) => {
      await queryClient.cancelQueries({ queryKey: ["orders"] });

      const previousOrders = queryClient.getQueryData(["orders"]);

      return { previousOrders };
    },
    onError: (error: Error, variables, context) => {
      if (context?.previousOrders) {
        queryClient.setQueryData(["orders"], context.previousOrders);
      }
      toast({
        title: "Failed to delete order",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Order deleted",
        description: "The order has been deleted successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["orders"] });
    },
  });
}
