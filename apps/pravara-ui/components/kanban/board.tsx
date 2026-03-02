"use client";

import { useState } from "react";
import {
  DndContext,
  DragEndEvent,
  DragOverEvent,
  DragOverlay,
  DragStartEvent,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { KanbanColumn } from "./column";
import { KanbanCard, type CommandStatus } from "./card";
import type { Task, TaskStatus, Machine } from "@/lib/api";

const COLUMNS: { id: TaskStatus; title: string; color: string }[] = [
  { id: "backlog", title: "Backlog", color: "bg-blue-500" },
  { id: "queued", title: "Queued", color: "bg-purple-500" },
  { id: "in_progress", title: "In Progress", color: "bg-yellow-500" },
  { id: "quality_check", title: "Quality Check", color: "bg-cyan-500" },
  { id: "completed", title: "Completed", color: "bg-green-500" },
  { id: "blocked", title: "Blocked", color: "bg-red-500" },
];

interface KanbanBoardProps {
  tasks: Record<TaskStatus, Task[]>;
  machines?: Map<string, Machine>;
  commandStatuses?: Map<string, CommandStatus>;
  onTaskMove: (taskId: string, status: TaskStatus, position: number) => void;
  onTaskClick?: (task: Task) => void;
}

export function KanbanBoard({
  tasks,
  machines,
  commandStatuses,
  onTaskMove,
  onTaskClick,
}: KanbanBoardProps) {
  const [activeTask, setActiveTask] = useState<Task | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8,
      },
    })
  );

  function handleDragStart(event: DragStartEvent) {
    const { active } = event;
    const task = findTask(active.id as string);
    if (task) {
      setActiveTask(task);
    }
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;

    if (!over) {
      setActiveTask(null);
      return;
    }

    const activeId = active.id as string;
    const overId = over.id as string;

    // Determine the target column
    let targetStatus: TaskStatus;
    let targetPosition: number;

    // Check if dropping on a column
    const targetColumn = COLUMNS.find((col) => col.id === overId);
    if (targetColumn) {
      targetStatus = targetColumn.id;
      targetPosition = (tasks[targetStatus]?.length || 0) + 1;
    } else {
      // Dropping on another task
      const overTask = findTask(overId);
      if (overTask) {
        targetStatus = overTask.status;
        const columnTasks = tasks[targetStatus] || [];
        const overIndex = columnTasks.findIndex((t) => t.id === overId);
        targetPosition = overIndex + 1;
      } else {
        setActiveTask(null);
        return;
      }
    }

    const task = findTask(activeId);
    if (task && (task.status !== targetStatus || task.kanban_position !== targetPosition)) {
      onTaskMove(activeId, targetStatus, targetPosition);
    }

    setActiveTask(null);
  }

  function findTask(id: string): Task | undefined {
    for (const column of COLUMNS) {
      const task = tasks[column.id]?.find((t) => t.id === id);
      if (task) return task;
    }
    return undefined;
  }

  function getMachineForTask(task: Task): Machine | undefined {
    if (!machines || !task.machine_id) return undefined;
    return machines.get(task.machine_id);
  }

  function getCommandStatusForTask(task: Task): CommandStatus | undefined {
    if (!commandStatuses) return undefined;
    return commandStatuses.get(task.id);
  }

  return (
    <DndContext
      sensors={sensors}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <div className="flex h-full gap-4 overflow-x-auto pb-4">
        {COLUMNS.map((column) => (
          <KanbanColumn
            key={column.id}
            id={column.id}
            title={column.title}
            color={column.color}
            count={tasks[column.id]?.length || 0}
          >
            <SortableContext
              items={tasks[column.id]?.map((t) => t.id) || []}
              strategy={verticalListSortingStrategy}
            >
              <div className="flex flex-col gap-2">
                {tasks[column.id]?.map((task) => (
                  <KanbanCard
                    key={task.id}
                    task={task}
                    machine={getMachineForTask(task)}
                    commandStatus={getCommandStatusForTask(task)}
                    onClick={() => onTaskClick?.(task)}
                  />
                ))}
              </div>
            </SortableContext>
          </KanbanColumn>
        ))}
      </div>
      <DragOverlay>
        {activeTask ? (
          <KanbanCard
            task={activeTask}
            machine={getMachineForTask(activeTask)}
            commandStatus={getCommandStatusForTask(activeTask)}
            isDragging
          />
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}
