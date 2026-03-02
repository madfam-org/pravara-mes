"use client";

import { useDroppable } from "@dnd-kit/core";
import { cn } from "@/lib/utils";

interface KanbanColumnProps {
  id: string;
  title: string;
  color: string;
  count: number;
  children: React.ReactNode;
}

export function KanbanColumn({
  id,
  title,
  color,
  count,
  children,
}: KanbanColumnProps) {
  const { isOver, setNodeRef } = useDroppable({
    id,
  });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "flex w-80 shrink-0 flex-col rounded-lg bg-muted/50",
        isOver && "ring-2 ring-primary ring-offset-2"
      )}
    >
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <div className={cn("h-3 w-3 rounded-full", color)} />
        <h3 className="font-semibold">{title}</h3>
        <span className="ml-auto rounded-full bg-muted px-2 py-0.5 text-xs font-medium">
          {count}
        </span>
      </div>
      <div className="flex-1 overflow-y-auto p-2">{children}</div>
    </div>
  );
}
