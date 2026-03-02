"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { machinesAPI, type Machine, type CreateMachineRequest, type UpdateMachineRequest } from "@/lib/api";
import { toast } from "@/lib/hooks/use-toast";

export function useCreateMachine() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, data }: { token: string; data: CreateMachineRequest }) =>
      machinesAPI.create(token, data),
    onMutate: async () => {
      await queryClient.cancelQueries({ queryKey: ["machines"] });
    },
    onError: (error: Error) => {
      toast({
        title: "Failed to create machine",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Machine created",
        description: "The machine has been created successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["machines"] });
    },
  });
}

export function useUpdateMachine() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, id, data }: { token: string; id: string; data: UpdateMachineRequest }) =>
      machinesAPI.update(token, id, data),
    onMutate: async ({ id, data }) => {
      await queryClient.cancelQueries({ queryKey: ["machines"] });
      await queryClient.cancelQueries({ queryKey: ["machines", id] });

      const previousMachines = queryClient.getQueryData(["machines"]);
      const previousMachine = queryClient.getQueryData(["machines", id]);

      // Optimistically update the machine
      queryClient.setQueryData(["machines", id], (old: Machine | undefined) => {
        if (!old) return old;
        return { ...old, ...data };
      });

      return { previousMachines, previousMachine };
    },
    onError: (error: Error, variables, context) => {
      if (context?.previousMachines) {
        queryClient.setQueryData(["machines"], context.previousMachines);
      }
      if (context?.previousMachine) {
        queryClient.setQueryData(["machines", variables.id], context.previousMachine);
      }
      toast({
        title: "Failed to update machine",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Machine updated",
        description: "The machine has been updated successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["machines"] });
    },
  });
}

export function useDeleteMachine() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, id }: { token: string; id: string }) =>
      machinesAPI.delete(token, id),
    onMutate: async ({ id }) => {
      await queryClient.cancelQueries({ queryKey: ["machines"] });

      const previousMachines = queryClient.getQueryData(["machines"]);

      return { previousMachines };
    },
    onError: (error: Error, variables, context) => {
      if (context?.previousMachines) {
        queryClient.setQueryData(["machines"], context.previousMachines);
      }
      toast({
        title: "Failed to delete machine",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Machine deleted",
        description: "The machine has been deleted successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["machines"] });
    },
  });
}
