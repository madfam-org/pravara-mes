"use client";

import { Calendar, Clock, User, Wrench } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import type { MaintenanceWorkOrder } from "@/lib/api";
import { formatDate } from "@/lib/utils";

interface WorkOrderCardProps {
  workOrder: MaintenanceWorkOrder;
  onComplete: (id: string) => void;
  onStart?: (id: string) => void;
}

const statusConfig: Record<
  MaintenanceWorkOrder["status"],
  { variant: "default" | "secondary" | "destructive" | "outline" | "success" | "warning" | "error"; label: string }
> = {
  scheduled: { variant: "default", label: "Scheduled" },
  overdue: { variant: "error", label: "Overdue" },
  in_progress: { variant: "warning", label: "In Progress" },
  completed: { variant: "success", label: "Completed" },
  cancelled: { variant: "secondary", label: "Cancelled" },
};

const priorityLabels: Record<number, string> = {
  1: "Low",
  2: "Normal",
  3: "High",
  4: "Urgent",
};

const priorityColors: Record<number, string> = {
  1: "text-muted-foreground",
  2: "text-foreground",
  3: "text-yellow-600 dark:text-yellow-400",
  4: "text-red-600 dark:text-red-400",
};

export function WorkOrderCard({ workOrder, onComplete, onStart }: WorkOrderCardProps) {
  const config = statusConfig[workOrder.status] || statusConfig.scheduled;

  const checklist = (workOrder.checklist || []) as Array<{ title?: string; done?: boolean }>;
  const checklistTotal = checklist.length;
  const checklistDone = checklist.filter((item) => item.done).length;
  const checklistProgress = checklistTotal > 0 ? (checklistDone / checklistTotal) * 100 : 0;

  const canStart = workOrder.status === "scheduled" || workOrder.status === "overdue";
  const canComplete = workOrder.status === "in_progress";

  return (
    <Card className="hover:shadow-md transition-shadow">
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <CardTitle className="text-base truncate">{workOrder.title}</CardTitle>
            <p className="text-xs font-mono text-muted-foreground">
              {workOrder.work_order_number}
            </p>
          </div>
          <Badge variant={config.variant}>{config.label}</Badge>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-3 text-sm">
          {/* Priority */}
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">Priority</span>
            <span className={priorityColors[workOrder.priority] || "text-foreground"}>
              {priorityLabels[workOrder.priority] || `P${workOrder.priority}`}
            </span>
          </div>

          {/* Machine */}
          <div className="flex items-center gap-2 text-muted-foreground">
            <Wrench className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">Machine: {workOrder.machine_id}</span>
          </div>

          {/* Assigned To */}
          {workOrder.assigned_to && (
            <div className="flex items-center gap-2 text-muted-foreground">
              <User className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate">{workOrder.assigned_to}</span>
            </div>
          )}

          {/* Dates */}
          {workOrder.scheduled_at && (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Calendar className="h-3.5 w-3.5 shrink-0" />
              <span>Scheduled: {formatDate(workOrder.scheduled_at)}</span>
            </div>
          )}
          {workOrder.due_at && (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Clock className="h-3.5 w-3.5 shrink-0" />
              <span>Due: {formatDate(workOrder.due_at)}</span>
            </div>
          )}

          {/* Checklist Progress */}
          {checklistTotal > 0 && (
            <div className="space-y-1">
              <div className="flex items-center justify-between text-xs">
                <span className="text-muted-foreground">Checklist</span>
                <span className="tabular-nums">
                  {checklistDone}/{checklistTotal}
                </span>
              </div>
              <Progress value={checklistProgress} className="h-1.5" />
            </div>
          )}

          {/* Actions */}
          {(canStart || canComplete) && (
            <div className="flex gap-2 pt-1">
              {canStart && onStart && (
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1"
                  onClick={() => onStart(workOrder.id)}
                >
                  Start
                </Button>
              )}
              {canComplete && (
                <Button
                  size="sm"
                  className="flex-1"
                  onClick={() => onComplete(workOrder.id)}
                >
                  Complete
                </Button>
              )}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
