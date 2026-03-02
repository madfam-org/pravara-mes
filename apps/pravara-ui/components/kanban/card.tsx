"use client";

import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Clock, User, Factory, Loader2, CheckCircle2, XCircle, AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";
import { Card, CardContent } from "@/components/ui/card";
import type { Task, Machine, MachineStatus } from "@/lib/api";

// Command status for task-machine automation
export type CommandStatus = "pending" | "sent" | "acknowledged" | "completed" | "failed";

interface KanbanCardProps {
  task: Task;
  machine?: Machine;
  commandStatus?: CommandStatus;
  isDragging?: boolean;
  onClick?: () => void;
}

const machineStatusColors: Record<MachineStatus, string> = {
  offline: "bg-gray-400",
  online: "bg-green-500",
  idle: "bg-blue-400",
  running: "bg-green-500 animate-pulse",
  maintenance: "bg-yellow-500",
  error: "bg-red-500",
};

const machineStatusLabels: Record<MachineStatus, string> = {
  offline: "Offline",
  online: "Online",
  idle: "Idle",
  running: "Running",
  maintenance: "Maintenance",
  error: "Error",
};

function CommandStatusIcon({ status }: { status: CommandStatus }) {
  switch (status) {
    case "pending":
    case "sent":
      return <Loader2 className="h-3 w-3 animate-spin text-blue-500" />;
    case "acknowledged":
      return <CheckCircle2 className="h-3 w-3 text-blue-500" />;
    case "completed":
      return <CheckCircle2 className="h-3 w-3 text-green-500" />;
    case "failed":
      return <XCircle className="h-3 w-3 text-red-500" />;
    default:
      return null;
  }
}

export function KanbanCard({ task, machine, commandStatus, isDragging, onClick }: KanbanCardProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging: isSortableDragging,
  } = useSortable({ id: task.id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  const priorityColors: Record<number, string> = {
    1: "border-l-red-500",
    2: "border-l-orange-500",
    3: "border-l-yellow-500",
    4: "border-l-blue-500",
    5: "border-l-gray-500",
  };

  // Show warning if machine is in error or maintenance state
  const machineWarning = machine && (machine.status === "error" || machine.status === "maintenance");

  return (
    <Card
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      onClick={onClick}
      className={cn(
        "cursor-grab border-l-4 transition-shadow hover:shadow-md",
        priorityColors[task.priority] || "border-l-gray-500",
        (isDragging || isSortableDragging) && "opacity-50 shadow-lg",
        isDragging && "rotate-3",
        machineWarning && "ring-1 ring-yellow-400"
      )}
    >
      <CardContent className="p-3">
        <div className="flex items-start justify-between gap-2">
          <h4 className="font-medium leading-tight">{task.title}</h4>
          {commandStatus && <CommandStatusIcon status={commandStatus} />}
        </div>
        {task.description && (
          <p className="mt-1 text-sm text-muted-foreground line-clamp-2">
            {task.description}
          </p>
        )}
        <div className="mt-3 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
          {task.estimated_minutes && (
            <span className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {task.estimated_minutes}m
            </span>
          )}
          {task.assigned_user_id && (
            <span className="flex items-center gap-1">
              <User className="h-3 w-3" />
              Assigned
            </span>
          )}
          {machine ? (
            <span className="flex items-center gap-1">
              <Factory className="h-3 w-3" />
              <span className="max-w-[80px] truncate" title={machine.name}>
                {machine.name}
              </span>
              <span
                className={cn(
                  "h-2 w-2 rounded-full",
                  machineStatusColors[machine.status]
                )}
                title={machineStatusLabels[machine.status]}
              />
              {machineWarning && (
                <AlertTriangle className="h-3 w-3 text-yellow-500" />
              )}
            </span>
          ) : task.machine_id ? (
            <span className="flex items-center gap-1">
              <Factory className="h-3 w-3" />
              Machine
            </span>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
