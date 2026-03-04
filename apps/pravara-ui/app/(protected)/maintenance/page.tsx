"use client";

import { useState } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Plus,
  Wrench,
  ClipboardList,
  Calendar,
  User,
  RefreshCw,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  maintenanceAPI,
  machinesAPI,
  type MaintenanceSchedule,
  type MaintenanceWorkOrder,
  type Machine,
} from "@/lib/api";
import { formatDate } from "@/lib/utils";
import { WorkOrderCard } from "@/components/maintenance/work-order-card";

type Tab = "schedules" | "work-orders";

const scheduleStatusVariant = (isActive: boolean) =>
  isActive ? "success" : "secondary";

const workOrderStatusConfig: Record<
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

export default function MaintenancePage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<Tab>("schedules");

  // Fetch machines for name resolution
  const { data: machinesData } = useQuery({
    queryKey: ["machines"],
    queryFn: () => machinesAPI.list(token),
    enabled: !!token,
  });
  const machines: Machine[] = machinesData?.data || [];
  const machineNameMap = new Map(machines.map((m) => [m.id, m.name]));

  // Fetch schedules
  const { data: schedulesData, isLoading: schedulesLoading } = useQuery({
    queryKey: ["maintenance-schedules"],
    queryFn: () => maintenanceAPI.listSchedules(token),
    enabled: !!token,
  });
  const schedules: MaintenanceSchedule[] = schedulesData?.data || [];

  // Fetch work orders
  const { data: workOrdersData, isLoading: workOrdersLoading } = useQuery({
    queryKey: ["maintenance-work-orders"],
    queryFn: () => maintenanceAPI.listWorkOrders(token),
    enabled: !!token,
  });
  const workOrders: MaintenanceWorkOrder[] = workOrdersData?.data || [];

  // Complete work order mutation
  const completeMutation = useMutation({
    mutationFn: (id: string) => maintenanceAPI.completeWorkOrder(token, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["maintenance-work-orders"] });
    },
  });

  // Start work order mutation (update status to in_progress)
  const startMutation = useMutation({
    mutationFn: (id: string) =>
      maintenanceAPI.updateWorkOrder(token, id, { status: "in_progress" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["maintenance-work-orders"] });
    },
  });

  const handleCompleteWorkOrder = (id: string) => {
    completeMutation.mutate(id);
  };

  const handleStartWorkOrder = (id: string) => {
    startMutation.mutate(id);
  };

  const isLoading = activeTab === "schedules" ? schedulesLoading : workOrdersLoading;

  const triggerTypeLabels: Record<string, string> = {
    calendar: "Calendar",
    runtime_hours: "Runtime Hours",
    cycle_count: "Cycle Count",
    condition: "Condition",
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Maintenance</h1>
          <p className="text-muted-foreground">
            Schedules and work orders for preventive maintenance
          </p>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="outline">
            <Plus className="mr-2 h-4 w-4" />
            New Schedule
          </Button>
          <Button size="sm">
            <Plus className="mr-2 h-4 w-4" />
            New Work Order
          </Button>
        </div>
      </div>

      {/* Tab Toggle */}
      <div className="flex gap-1 rounded-lg bg-muted p-1 w-fit">
        <Button
          variant={activeTab === "schedules" ? "default" : "ghost"}
          size="sm"
          onClick={() => setActiveTab("schedules")}
          className="gap-2"
        >
          <Calendar className="h-4 w-4" />
          Schedules
          {schedules.length > 0 && (
            <span className="ml-1 rounded-full bg-background/50 px-1.5 py-0.5 text-xs tabular-nums">
              {schedules.length}
            </span>
          )}
        </Button>
        <Button
          variant={activeTab === "work-orders" ? "default" : "ghost"}
          size="sm"
          onClick={() => setActiveTab("work-orders")}
          className="gap-2"
        >
          <ClipboardList className="h-4 w-4" />
          Work Orders
          {workOrders.length > 0 && (
            <span className="ml-1 rounded-full bg-background/50 px-1.5 py-0.5 text-xs tabular-nums">
              {workOrders.length}
            </span>
          )}
        </Button>
      </div>

      {/* Loading State */}
      {isLoading && (
        <div className="flex items-center justify-center py-12">
          <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      )}

      {/* Schedules Tab */}
      {activeTab === "schedules" && !schedulesLoading && (
        <>
          {schedules.length === 0 ? (
            <Card>
              <CardContent className="flex flex-col items-center justify-center py-12">
                <Calendar className="h-12 w-12 text-muted-foreground" />
                <h3 className="mt-4 text-lg font-semibold">
                  No maintenance schedules
                </h3>
                <p className="text-muted-foreground">
                  Create your first schedule to set up preventive maintenance
                </p>
                <Button className="mt-4" size="sm">
                  <Plus className="mr-2 h-4 w-4" />
                  New Schedule
                </Button>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="pt-6">
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b">
                        <th className="text-left py-3 px-2 font-medium">Name</th>
                        <th className="text-left py-3 px-2 font-medium">Machine</th>
                        <th className="text-left py-3 px-2 font-medium">Trigger</th>
                        <th className="text-left py-3 px-2 font-medium">Interval</th>
                        <th className="text-left py-3 px-2 font-medium">Next Due</th>
                        <th className="text-left py-3 px-2 font-medium">Assigned To</th>
                        <th className="text-left py-3 px-2 font-medium">Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      {schedules.map((schedule) => {
                        let interval = "-";
                        if (schedule.interval_days)
                          interval = `${schedule.interval_days} days`;
                        else if (schedule.interval_hours)
                          interval = `${schedule.interval_hours} hrs`;
                        else if (schedule.interval_cycles)
                          interval = `${schedule.interval_cycles} cycles`;

                        return (
                          <tr
                            key={schedule.id}
                            className="border-b last:border-0 hover:bg-muted/50"
                          >
                            <td className="py-3 px-2 font-medium">
                              {schedule.name}
                            </td>
                            <td className="py-3 px-2">
                              {machineNameMap.get(schedule.machine_id) ||
                                schedule.machine_id.slice(0, 8)}
                            </td>
                            <td className="py-3 px-2">
                              {triggerTypeLabels[schedule.trigger_type] ||
                                schedule.trigger_type}
                            </td>
                            <td className="py-3 px-2 tabular-nums">{interval}</td>
                            <td className="py-3 px-2">
                              {schedule.next_due_at
                                ? formatDate(schedule.next_due_at)
                                : "-"}
                            </td>
                            <td className="py-3 px-2">
                              {schedule.assigned_to ? (
                                <span className="flex items-center gap-1">
                                  <User className="h-3 w-3" />
                                  {schedule.assigned_to}
                                </span>
                              ) : (
                                <span className="text-muted-foreground">-</span>
                              )}
                            </td>
                            <td className="py-3 px-2">
                              <Badge variant={scheduleStatusVariant(schedule.is_active)}>
                                {schedule.is_active ? "Active" : "Inactive"}
                              </Badge>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          )}
        </>
      )}

      {/* Work Orders Tab */}
      {activeTab === "work-orders" && !workOrdersLoading && (
        <>
          {workOrders.length === 0 ? (
            <Card>
              <CardContent className="flex flex-col items-center justify-center py-12">
                <ClipboardList className="h-12 w-12 text-muted-foreground" />
                <h3 className="mt-4 text-lg font-semibold">
                  No work orders
                </h3>
                <p className="text-muted-foreground">
                  Work orders will appear here when maintenance is due
                </p>
                <Button className="mt-4" size="sm">
                  <Plus className="mr-2 h-4 w-4" />
                  New Work Order
                </Button>
              </CardContent>
            </Card>
          ) : (
            <div className="space-y-6">
              {/* Table view */}
              <Card>
                <CardContent className="pt-6">
                  <div className="overflow-x-auto">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="border-b">
                          <th className="text-left py-3 px-2 font-medium">WO #</th>
                          <th className="text-left py-3 px-2 font-medium">Title</th>
                          <th className="text-left py-3 px-2 font-medium">Machine</th>
                          <th className="text-left py-3 px-2 font-medium">Status</th>
                          <th className="text-left py-3 px-2 font-medium">Priority</th>
                          <th className="text-left py-3 px-2 font-medium">Assigned To</th>
                          <th className="text-left py-3 px-2 font-medium">Due</th>
                        </tr>
                      </thead>
                      <tbody>
                        {workOrders.map((wo) => {
                          const config =
                            workOrderStatusConfig[wo.status] ||
                            workOrderStatusConfig.scheduled;
                          return (
                            <tr
                              key={wo.id}
                              className="border-b last:border-0 hover:bg-muted/50"
                            >
                              <td className="py-3 px-2 font-mono text-xs">
                                {wo.work_order_number}
                              </td>
                              <td className="py-3 px-2 font-medium">
                                {wo.title}
                              </td>
                              <td className="py-3 px-2">
                                {machineNameMap.get(wo.machine_id) ||
                                  wo.machine_id.slice(0, 8)}
                              </td>
                              <td className="py-3 px-2">
                                <Badge variant={config.variant}>
                                  {config.label}
                                </Badge>
                              </td>
                              <td className="py-3 px-2">
                                {priorityLabels[wo.priority] || `P${wo.priority}`}
                              </td>
                              <td className="py-3 px-2">
                                {wo.assigned_to ? (
                                  <span className="flex items-center gap-1">
                                    <User className="h-3 w-3" />
                                    {wo.assigned_to}
                                  </span>
                                ) : (
                                  <span className="text-muted-foreground">-</span>
                                )}
                              </td>
                              <td className="py-3 px-2">
                                {wo.due_at ? formatDate(wo.due_at) : "-"}
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                </CardContent>
              </Card>

              {/* Card grid view for active work orders */}
              {workOrders.some(
                (wo) =>
                  wo.status === "scheduled" ||
                  wo.status === "overdue" ||
                  wo.status === "in_progress"
              ) && (
                <div>
                  <h2 className="text-lg font-semibold mb-3">Active Work Orders</h2>
                  <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                    {workOrders
                      .filter(
                        (wo) =>
                          wo.status === "scheduled" ||
                          wo.status === "overdue" ||
                          wo.status === "in_progress"
                      )
                      .map((wo) => (
                        <WorkOrderCard
                          key={wo.id}
                          workOrder={wo}
                          onComplete={handleCompleteWorkOrder}
                          onStart={handleStartWorkOrder}
                        />
                      ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
