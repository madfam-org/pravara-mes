"use client";

import { useState } from "react";
import { useSession } from "next-auth/react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { KanbanBoard } from "@/components/kanban/board";
import { TaskDialog } from "@/components/dialogs/task-dialog";
import { tasksAPI, type Task, type TaskStatus } from "@/lib/api";

export default function KanbanPage() {
  const { data: session } = useSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedTask, setSelectedTask] = useState<Task | undefined>();

  const { data, isLoading, refetch } = useQuery({
    queryKey: ["kanban-board"],
    queryFn: () => tasksAPI.getBoard(token),
    enabled: !!token,
  });

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
        }

        return { columns: newColumns };
      });

      return { previousData };
    },
    onError: (err, variables, context) => {
      if (context?.previousData) {
        queryClient.setQueryData(["kanban-board"], context.previousData);
      }
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
