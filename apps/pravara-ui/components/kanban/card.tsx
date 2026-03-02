"use client";

import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Clock, User, Factory } from "lucide-react";
import { cn } from "@/lib/utils";
import { Card, CardContent } from "@/components/ui/card";
import type { Task } from "@/lib/api";

interface KanbanCardProps {
  task: Task;
  isDragging?: boolean;
  onClick?: () => void;
}

export function KanbanCard({ task, isDragging, onClick }: KanbanCardProps) {
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
        isDragging && "rotate-3"
      )}
    >
      <CardContent className="p-3">
        <h4 className="font-medium leading-tight">{task.title}</h4>
        {task.description && (
          <p className="mt-1 text-sm text-muted-foreground line-clamp-2">
            {task.description}
          </p>
        )}
        <div className="mt-3 flex items-center gap-3 text-xs text-muted-foreground">
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
          {task.machine_id && (
            <span className="flex items-center gap-1">
              <Factory className="h-3 w-3" />
              Machine
            </span>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
