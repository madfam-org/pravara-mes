"use client";

import { usePravaraSession } from "@/lib/auth";
import { useQuery } from "@tanstack/react-query";
import {
  Package,
  Factory,
  CheckCircle,
  Clock,
  AlertTriangle,
  TrendingUp,
  BarChart3,
  Wrench,
} from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ordersAPI, machinesAPI, tasksAPI, analyticsAPI, maintenanceAPI, Task, TaskStatus } from "@/lib/api";

export default function DashboardPage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;

  const { data: ordersData } = useQuery({
    queryKey: ["orders"],
    queryFn: () => ordersAPI.list(token),
    enabled: !!token,
  });

  const { data: machinesData } = useQuery({
    queryKey: ["machines"],
    queryFn: () => machinesAPI.list(token),
    enabled: !!token,
  });

  const { data: boardData } = useQuery({
    queryKey: ["kanban-board"],
    queryFn: () => tasksAPI.getBoard(token),
    enabled: !!token,
  });

  const { data: oeeSummary } = useQuery({
    queryKey: ["oee-summary"],
    queryFn: () => analyticsAPI.getOEESummary(token),
    enabled: !!token,
  });

  const { data: workOrdersData } = useQuery({
    queryKey: ["maintenance-work-orders-dashboard"],
    queryFn: () => maintenanceAPI.listWorkOrders(token, new URLSearchParams({ status: "overdue" })),
    enabled: !!token,
  });

  const orders = ordersData?.data || [];
  const machines = machinesData?.data || [];
  const board: Partial<Record<TaskStatus, Task[]>> = boardData?.columns || {};

  const inProgressTasks = board["in_progress"]?.length || 0;
  const completedTasks = board["completed"]?.length || 0;
  const blockedTasks = board["blocked"]?.length || 0;
  const totalTasks = Object.values(board).flat().length;

  const onlineMachines = machines.filter(
    (m) => m.status === "online" || m.status === "running"
  ).length;

  const fleetOEE = oeeSummary && oeeSummary.length > 0
    ? oeeSummary.reduce((sum, s) => sum + s.oee, 0) / oeeSummary.length
    : null;

  const overdueCount = workOrdersData?.total || 0;

  const stats = [
    {
      name: "Active Orders",
      value: orders.filter((o) => o.status !== "delivered" && o.status !== "cancelled")
        .length,
      icon: Package,
      color: "text-blue-500",
    },
    {
      name: "Online Machines",
      value: `${onlineMachines}/${machines.length}`,
      icon: Factory,
      color: "text-green-500",
    },
    {
      name: "Tasks In Progress",
      value: inProgressTasks,
      icon: Clock,
      color: "text-orange-500",
    },
    {
      name: "Completed Today",
      value: completedTasks,
      icon: CheckCircle,
      color: "text-emerald-500",
    },
    {
      name: "Fleet OEE",
      value: fleetOEE !== null ? `${(fleetOEE * 100).toFixed(0)}%` : "—",
      icon: BarChart3,
      color: "text-purple-500",
    },
    {
      name: "Overdue Maintenance",
      value: overdueCount,
      icon: Wrench,
      color: overdueCount > 0 ? "text-red-500" : "text-gray-400",
    },
  ];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Dashboard</h1>
        <p className="text-muted-foreground">
          Welcome back, {session?.user?.name}
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
        {stats.map((stat) => (
          <Card key={stat.name}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{stat.name}</CardTitle>
              <stat.icon className={`h-5 w-5 ${stat.color}`} />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stat.value}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <TrendingUp className="h-5 w-5" />
              Task Overview
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">Backlog</span>
                <span className="font-medium">{board["backlog"]?.length || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">Queued</span>
                <span className="font-medium">{board["queued"]?.length || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">In Progress</span>
                <span className="font-medium">{inProgressTasks}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">Quality Check</span>
                <span className="font-medium">{board["quality_check"]?.length || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">Completed</span>
                <span className="font-medium">{completedTasks}</span>
              </div>
              {blockedTasks > 0 && (
                <div className="flex items-center justify-between text-destructive">
                  <span className="flex items-center gap-1 text-sm">
                    <AlertTriangle className="h-4 w-4" />
                    Blocked
                  </span>
                  <span className="font-medium">{blockedTasks}</span>
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Factory className="h-5 w-5" />
              Machine Status
            </CardTitle>
          </CardHeader>
          <CardContent>
            {machines.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No machines registered yet.
              </p>
            ) : (
              <div className="space-y-3">
                {machines.slice(0, 5).map((machine) => (
                  <div
                    key={machine.id}
                    className="flex items-center justify-between"
                  >
                    <div>
                      <p className="font-medium">{machine.name}</p>
                      <p className="text-xs text-muted-foreground">
                        {machine.code}
                      </p>
                    </div>
                    <span
                      className={`inline-flex items-center rounded-full px-2 py-1 text-xs font-medium ${
                        machine.status === "online" || machine.status === "running"
                          ? "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400"
                          : machine.status === "error"
                          ? "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400"
                          : "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-400"
                      }`}
                    >
                      {machine.status}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
