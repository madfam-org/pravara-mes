"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { tasksAPI, type Task, type CreateTaskRequest, type UpdateTaskRequest } from "@/lib/api";
import { toast } from "@/lib/hooks/use-toast";

export function useCreateTask() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, data }: { token: string; data: CreateTaskRequest }) =>
      tasksAPI.create(token, data),
    onMutate: async () => {
      await queryClient.cancelQueries({ queryKey: ["tasks"] });
      await queryClient.cancelQueries({ queryKey: ["kanban-board"] });
    },
    onError: (error: Error) => {
      toast({
        title: "Failed to create task",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Task created",
        description: "The task has been created successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
      queryClient.invalidateQueries({ queryKey: ["kanban-board"] });
    },
  });
}

export function useUpdateTask() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, id, data }: { token: string; id: string; data: UpdateTaskRequest }) =>
      tasksAPI.update(token, id, data),
    onMutate: async ({ id, data }) => {
      await queryClient.cancelQueries({ queryKey: ["tasks"] });
      await queryClient.cancelQueries({ queryKey: ["kanban-board"] });
      await queryClient.cancelQueries({ queryKey: ["tasks", id] });

      const previousTasks = queryClient.getQueryData(["tasks"]);
      const previousTask = queryClient.getQueryData(["tasks", id]);
      const previousBoard = queryClient.getQueryData(["kanban-board"]);

      // Optimistically update the task
      queryClient.setQueryData(["tasks", id], (old: Task | undefined) => {
        if (!old) return old;
        return { ...old, ...data };
      });

      return { previousTasks, previousTask, previousBoard };
    },
    onError: (error: Error, variables, context) => {
      if (context?.previousTasks) {
        queryClient.setQueryData(["tasks"], context.previousTasks);
      }
      if (context?.previousTask) {
        queryClient.setQueryData(["tasks", variables.id], context.previousTask);
      }
      if (context?.previousBoard) {
        queryClient.setQueryData(["kanban-board"], context.previousBoard);
      }
      toast({
        title: "Failed to update task",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Task updated",
        description: "The task has been updated successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
      queryClient.invalidateQueries({ queryKey: ["kanban-board"] });
    },
  });
}

export function useDeleteTask() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ token, id }: { token: string; id: string }) =>
      tasksAPI.delete(token, id),
    onMutate: async ({ id }) => {
      await queryClient.cancelQueries({ queryKey: ["tasks"] });
      await queryClient.cancelQueries({ queryKey: ["kanban-board"] });

      const previousTasks = queryClient.getQueryData(["tasks"]);
      const previousBoard = queryClient.getQueryData(["kanban-board"]);

      return { previousTasks, previousBoard };
    },
    onError: (error: Error, variables, context) => {
      if (context?.previousTasks) {
        queryClient.setQueryData(["tasks"], context.previousTasks);
      }
      if (context?.previousBoard) {
        queryClient.setQueryData(["kanban-board"], context.previousBoard);
      }
      toast({
        title: "Failed to delete task",
        description: error.message,
        variant: "destructive",
      });
    },
    onSuccess: () => {
      toast({
        title: "Task deleted",
        description: "The task has been deleted successfully.",
        variant: "success",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
      queryClient.invalidateQueries({ queryKey: ["kanban-board"] });
    },
  });
}
