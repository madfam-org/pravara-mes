"use client";

import { Suspense, useMemo } from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { usePravaraSession } from "@/lib/auth";
import { machinesAPI } from "@/lib/api";
import { machinesNeedingAttention, summarizeMachines } from "@/lib/factory-summary";
import { QueryErrorState } from "@/components/query-error-state";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Eye,
  Camera,
  Settings,
  Play,
  Pause,
  RotateCw,
  Maximize,
  Grid3x3,
  Layers,
  Activity,
} from "lucide-react";

// Dynamically import 3D component to avoid SSR issues
const FactoryFloor3D = dynamic(
  () => import("@/components/factory-floor/FactoryFloor3D").then((mod) => mod.FactoryFloor3D),
  {
    ssr: false,
    loading: () => (
      <div className="w-full h-full flex items-center justify-center bg-muted/30">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary mx-auto mb-4" />
          <p className="text-muted-foreground">Loading 3D visualization...</p>
        </div>
      </div>
    ),
  }
);

/** One page of machines is enough for the header; the total comes from the API. */
const MACHINE_PAGE_SIZE = 200;

function StatCard({
  label,
  value,
  tone,
}: {
  label: string;
  value: number | null;
  tone?: string;
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">{label}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className={`text-2xl font-bold tabular-nums ${tone ?? ""}`}>
          {value === null ? (
            <span className="text-muted-foreground" aria-label="Not loaded">
              —
            </span>
          ) : (
            value
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export default function FactoryPage() {
  const { data: session } = usePravaraSession();
  const token = session?.accessToken ?? "";

  const machinesQuery = useQuery({
    queryKey: ["machines", "factory-floor"],
    queryFn: () =>
      machinesAPI.list(token, new URLSearchParams({ limit: String(MACHINE_PAGE_SIZE) })),
    enabled: !!token,
  });

  const machines = useMemo(() => machinesQuery.data?.data ?? [], [machinesQuery.data]);
  const loaded = machinesQuery.isSuccess;
  const summary = useMemo(() => summarizeMachines(machines), [machines]);
  const attention = useMemo(() => machinesNeedingAttention(machines), [machines]);
  const total = machinesQuery.data?.total ?? machines.length;
  const partial = loaded && total > summary.counted;

  return (
    <div className="container mx-auto p-6 space-y-6">
      {/* Header */}
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold">Factory Floor Digital Twin</h1>
          <p className="text-muted-foreground mt-1">
            Real-time 3D visualization and control of your production floor
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm">
            <Camera className="h-4 w-4 mr-2" />
            Cameras
          </Button>
          <Button variant="outline" size="sm">
            <Settings className="h-4 w-4 mr-2" />
            Settings
          </Button>
          <Button size="sm">
            <Maximize className="h-4 w-4 mr-2" />
            Fullscreen
          </Button>
        </div>
      </div>

      {/* Machine counts, from the machines list. */}
      {machinesQuery.isError ? (
        <QueryErrorState
          what="the machines"
          error={machinesQuery.error}
          onRetry={() => machinesQuery.refetch()}
          isRetrying={machinesQuery.isFetching}
        />
      ) : (
        <div className="space-y-2">
          <div className="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-6">
            <StatCard label="Machines" value={loaded ? total : null} />
            <StatCard
              label="Running"
              value={loaded ? summary.running : null}
              tone="text-green-600 dark:text-green-400"
            />
            <StatCard label="Ready" value={loaded ? summary.ready : null} />
            <StatCard
              label="Maintenance"
              value={loaded ? summary.maintenance : null}
              tone="text-orange-600 dark:text-orange-400"
            />
            <StatCard
              label="Error"
              value={loaded ? summary.error : null}
              tone={summary.error > 0 ? "text-red-600 dark:text-red-400" : undefined}
            />
            <StatCard label="Offline" value={loaded ? summary.offline : null} />
          </div>
          {partial && (
            <p className="text-sm text-muted-foreground">
              Status counts cover the first {summary.counted} of {total} machines.
            </p>
          )}
        </div>
      )}

      {/* Main Content */}
      <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
        {/* 3D Visualization (3/4 width) */}
        <div className="lg:col-span-3">
          <Card className="h-[600px]">
            <CardHeader className="pb-3">
              <div className="flex justify-between items-center">
                <div className="flex items-center gap-2">
                  <Eye className="h-5 w-5" />
                  <CardTitle>3D Factory View</CardTitle>
                </div>
                <div className="flex gap-1">
                  <Button size="icon" variant="ghost" className="h-8 w-8">
                    <Grid3x3 className="h-4 w-4" />
                  </Button>
                  <Button size="icon" variant="ghost" className="h-8 w-8">
                    <Layers className="h-4 w-4" />
                  </Button>
                  <Button size="icon" variant="ghost" className="h-8 w-8">
                    <RotateCw className="h-4 w-4" />
                  </Button>
                  <Button size="icon" variant="ghost" className="h-8 w-8">
                    <Play className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </CardHeader>
            <CardContent className="p-0 h-[calc(100%-4rem)]">
              <Suspense fallback={<div>Loading...</div>}>
                <FactoryFloor3D />
              </Suspense>
            </CardContent>
          </Card>
        </div>

        {/* Side Panel (1/4 width) */}
        <div className="space-y-6">
          {/* Machines in error or maintenance, from the machines list. */}
          <Card>
            <CardHeader>
              <div className="flex justify-between items-center">
                <CardTitle className="text-base">Needs attention</CardTitle>
                <Activity className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
              </div>
            </CardHeader>
            <CardContent className="space-y-3">
              {machinesQuery.isError ? (
                <p className="text-sm text-muted-foreground">
                  Machines could not be loaded, so nothing can be listed here.
                </p>
              ) : !loaded ? (
                <p className="text-sm text-muted-foreground">Loading machines…</p>
              ) : total === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No machines registered yet.{" "}
                  <Link href="/machines" className="font-medium text-foreground underline underline-offset-4">
                    Register a machine
                  </Link>
                </p>
              ) : attention.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No machine is in error or maintenance.
                </p>
              ) : (
                <ul role="list" className="space-y-3">
                  {attention.map((machine) => (
                    <li key={machine.id} className="flex items-start gap-2">
                      <Badge
                        variant={machine.status === "error" ? "destructive" : "secondary"}
                        className="mt-0.5"
                      >
                        {machine.status === "error" ? "Error" : "Maintenance"}
                      </Badge>
                      <Link
                        href={`/machines/${machine.id}`}
                        className="min-w-0 flex-1 text-sm underline-offset-4 hover:underline"
                      >
                        <span className="block truncate font-medium">{machine.name}</span>
                        <span className="block truncate text-xs text-muted-foreground">
                          {machine.code}
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>

          {/* View Controls */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">View Controls</CardTitle>
            </CardHeader>
            <CardContent>
              <Tabs defaultValue="cameras">
                <TabsList className="grid w-full grid-cols-3">
                  <TabsTrigger value="cameras">Cameras</TabsTrigger>
                  <TabsTrigger value="layers">Layers</TabsTrigger>
                  <TabsTrigger value="filters">Filters</TabsTrigger>
                </TabsList>
                <TabsContent value="cameras" className="space-y-2">
                  <Button variant="outline" size="sm" className="w-full justify-start">
                    Overview
                  </Button>
                  <Button variant="outline" size="sm" className="w-full justify-start">
                    Front View
                  </Button>
                  <Button variant="outline" size="sm" className="w-full justify-start">
                    Top View
                  </Button>
                  <Button variant="outline" size="sm" className="w-full justify-start">
                    Machine Focus
                  </Button>
                </TabsContent>
                <TabsContent value="layers" className="space-y-2">
                  <label className="flex items-center space-x-2">
                    <input type="checkbox" defaultChecked className="rounded" />
                    <span className="text-sm">Machines</span>
                  </label>
                  <label className="flex items-center space-x-2">
                    <input type="checkbox" defaultChecked className="rounded" />
                    <span className="text-sm">Tool Paths</span>
                  </label>
                  <label className="flex items-center space-x-2">
                    <input type="checkbox" defaultChecked className="rounded" />
                    <span className="text-sm">Heat Map</span>
                  </label>
                  <label className="flex items-center space-x-2">
                    <input type="checkbox" className="rounded" />
                    <span className="text-sm">Safety Zones</span>
                  </label>
                </TabsContent>
                <TabsContent value="filters" className="space-y-2">
                  <label className="flex items-center space-x-2">
                    <input type="checkbox" className="rounded" />
                    <span className="text-sm">Show Active Only</span>
                  </label>
                  <label className="flex items-center space-x-2">
                    <input type="checkbox" className="rounded" />
                    <span className="text-sm">Hide Idle</span>
                  </label>
                  <label className="flex items-center space-x-2">
                    <input type="checkbox" className="rounded" />
                    <span className="text-sm">Highlight Errors</span>
                  </label>
                </TabsContent>
              </Tabs>
            </CardContent>
          </Card>

          {/* Simulation Controls */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Simulation</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="flex gap-2">
                <Button size="sm" className="flex-1">
                  <Play className="h-4 w-4 mr-1" />
                  Run
                </Button>
                <Button size="sm" variant="outline" className="flex-1">
                  <Pause className="h-4 w-4 mr-1" />
                  Pause
                </Button>
              </div>
              <div className="space-y-2">
                <div className="flex justify-between text-sm">
                  <span>Speed</span>
                  <span>1.0x</span>
                </div>
                <input type="range" className="w-full" min="0.1" max="5" step="0.1" defaultValue="1" />
              </div>
              <div className="space-y-2">
                <div className="flex justify-between text-sm">
                  <span>Time</span>
                  <span>Real-time</span>
                </div>
                <select className="w-full px-3 py-1 border rounded-md text-sm">
                  <option>Real-time</option>
                  <option>1 hour ago</option>
                  <option>Today</option>
                  <option>Yesterday</option>
                </select>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}