"use client";

import { useState, useMemo, useCallback, useEffect } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { KanbanBoard } from "@/components/kanban/board";
import { TaskDialog } from "@/components/dialogs/task-dialog";
import { tasksAPI, machinesAPI, type Task, type TaskStatus, type Machine } from "@/lib/api";
import { useRealtime } from "@/lib/realtime/context";
import { EventTypes, type TaskJobData, type TaskMoveData } from "@/lib/realtime/types";
import { useToast } from "@/lib/hooks/use-toast";
import type { CommandStatus } from "@/components/kanban/card";

export default function KanbanPage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedTask, setSelectedTask] = useState<Task | undefined>();
  const [commandStatuses, setCommandStatuses] = useState<Map<string, CommandStatus>>(new Map());
  const { toast } = useToast();

  // Real-time connection
  const { subscribe, unsubscribe, isConnected } = useRealtime();

  const { data, isLoading, refetch } = useQuery({
    queryKey: ["kanban-board"],
    queryFn: () => tasksAPI.getBoard(token),
    enabled: !!token,
  });

  // Load machines to enrich task cards
  const { data: machinesData } = useQuery({
    queryKey: ["machines"],
    queryFn: () => machinesAPI.list(token),
    enabled: !!token,
  });

  // Create a map of machine ID to machine for quick lookup
  const machineMap = useMemo(() => {
    if (!machinesData?.data) return new Map<string, Machine>();
    return new Map(machinesData.data.map((m) => [m.id, m]));
  }, [machinesData]);

  // Handle real-time events
  const handleTaskJobStarted = useCallback((event: { data: TaskJobData }) => {
    const { task_id, task_title, machine_name } = event.data;
    setCommandStatuses((prev) => {
      const next = new Map(prev);
      next.set(task_id, "sent");
      return next;
    });
    toast({
      title: `Job started: ${task_title}`,
      description: `Running on ${machine_name}`,
    });
  }, [toast]);

  const handleTaskJobCompleted = useCallback((event: { data: TaskJobData }) => {
    const { task_id, task_title, machine_name, actual_minutes } = event.data;
    setCommandStatuses((prev) => {
      const next = new Map(prev);
      next.set(task_id, "completed");
      return next;
    });
    toast({
      title: `Job completed: ${task_title}`,
      description: actual_minutes
        ? `Completed on ${machine_name} in ${actual_minutes}m`
        : `Completed on ${machine_name}`,
      variant: "success",
    });
    // Refresh the board to get updated task status
    queryClient.invalidateQueries({ queryKey: ["kanban-board"] });
  }, [queryClient, toast]);

  const handleTaskJobFailed = useCallback((event: { data: TaskJobData }) => {
    const { task_id, task_title, machine_name, error_message } = event.data;
    setCommandStatuses((prev) => {
      const next = new Map(prev);
      next.set(task_id, "failed");
      return next;
    });
    toast({
      title: `Job failed: ${task_title}`,
      description: error_message || `Failed on ${machine_name}`,
      variant: "destructive",
    });
    // Refresh the board to get updated task status
    queryClient.invalidateQueries({ queryKey: ["kanban-board"] });
  }, [queryClient, toast]);

  const handleTaskMoved = useCallback((event: { data: TaskMoveData }) => {
    // Refresh board when another user moves a task
    queryClient.invalidateQueries({ queryKey: ["kanban-board"] });
  }, [queryClient]);

  // Subscribe to real-time events
  useEffect(() => {
    if (!isConnected) return;

    // Subscribe to task events
    subscribe(EventTypes.TASK_JOB_STARTED, handleTaskJobStarted);
    subscribe(EventTypes.TASK_JOB_COMPLETED, handleTaskJobCompleted);
    subscribe(EventTypes.TASK_JOB_FAILED, handleTaskJobFailed);
    subscribe(EventTypes.TASK_MOVED, handleTaskMoved);

    return () => {
      unsubscribe(EventTypes.TASK_JOB_STARTED, handleTaskJobStarted);
      unsubscribe(EventTypes.TASK_JOB_COMPLETED, handleTaskJobCompleted);
      unsubscribe(EventTypes.TASK_JOB_FAILED, handleTaskJobFailed);
      unsubscribe(EventTypes.TASK_MOVED, handleTaskMoved);
    };
  }, [isConnected, subscribe, unsubscribe, handleTaskJobStarted, handleTaskJobCompleted, handleTaskJobFailed, handleTaskMoved]);

  const moveMutation = useMutation({
    mutationFn: ({
      taskId,
      status,
      position,
    }: {
      taskId: string;
      status: TaskStatus;
      position: number;
    }) => tasksAPI.move(token, taskId, status, position),
    onMutate: async ({ taskId, status, position }) => {
      await queryClient.cancelQueries({ queryKey: ["kanban-board"] });

      const previousData = queryClient.getQueryData(["kanban-board"]);

      // Optimistically update the UI
      queryClient.setQueryData(["kanban-board"], (old: any) => {
        if (!old?.columns) return old;

        const newColumns = { ...old.columns };

        // Find and remove task from current column
        let task: Task | undefined;
        for (const columnId of Object.keys(newColumns)) {
          const columnTasks = newColumns[columnId] as Task[];
          const taskIndex = columnTasks.findIndex((t) => t.id === taskId);
          if (taskIndex !== -1) {
            [task] = columnTasks.splice(taskIndex, 1);
            break;
          }
        }

        if (task) {
          // Add task to new column at position
          task.status = status;
          task.kanban_position = position;
          if (!newColumns[status]) {
            newColumns[status] = [];
          }
          newColumns[status].splice(position - 1, 0, task);

          // If moving to in_progress with a machine, set pending status
          if (status === "in_progress" && task.machine_id) {
            setCommandStatuses((prev) => {
              const next = new Map(prev);
              next.set(taskId, "pending");
              return next;
            });
          }
        }

        return { columns: newColumns };
      });

      return { previousData };
    },
    onError: (err, variables, context) => {
      if (context?.previousData) {
        queryClient.setQueryData(["kanban-board"], context.previousData);
      }
      // Clear command status on error
      setCommandStatuses((prev) => {
        const next = new Map(prev);
        next.delete(variables.taskId);
        return next;
      });
      toast({
        title: "Failed to move task",
        variant: "destructive",
      });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["kanban-board"] });
    },
  });

  const handleTaskMove = (taskId: string, status: TaskStatus, position: number) => {
    moveMutation.mutate({ taskId, status, position });
  };

  const handleTaskClick = (task: Task) => {
    setSelectedTask(task);
    setDialogOpen(true);
  };

  const handleNewTask = () => {
    setSelectedTask(undefined);
    setDialogOpen(true);
  };

  if (isLoading) {
    return (
      <div className="flex h-[calc(100vh-8rem)] items-center justify-center">
        <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const columns = data?.columns || {
    backlog: [],
    queued: [],
    in_progress: [],
    quality_check: [],
    completed: [],
    blocked: [],
  };

  return (
    <div className="flex h-[calc(100vh-8rem)] flex-col">
      <div className="flex items-center justify-between pb-4">
        <div>
          <h1 className="text-3xl font-bold">Kanban Board</h1>
          <p className="text-muted-foreground">
            Drag and drop tasks to update their status
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
          <Button size="sm" onClick={handleNewTask}>
            <Plus className="mr-2 h-4 w-4" />
            New Task
          </Button>
        </div>
      </div>

      <div className="flex-1 overflow-hidden">
        <KanbanBoard
          tasks={columns}
          machines={machineMap}
          commandStatuses={commandStatuses}
          onTaskMove={handleTaskMove}
          onTaskClick={handleTaskClick}
        />
      </div>

      <TaskDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        task={selectedTask}
      />
    </div>
  );
}
