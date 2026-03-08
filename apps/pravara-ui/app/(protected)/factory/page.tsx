"use client";

import { Suspense } from "react";
import dynamic from "next/dynamic";
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

// Mock data for demonstration
const mockStats = {
  totalMachines: 12,
  activeMachines: 8,
  idleMachines: 3,
  maintenanceMachines: 1,
  totalProduction: 1847,
  efficiency: 87.3,
  uptime: 94.2,
};

const mockAlerts = [
  { id: "1", type: "warning", message: "Machine CNC-003 vibration levels elevated", time: "5m ago" },
  { id: "2", type: "info", message: "3D Printer #2 filament change required soon", time: "12m ago" },
  { id: "3", type: "success", message: "Laser cutter maintenance completed", time: "1h ago" },
];

export default function FactoryPage() {
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

      {/* Stats Bar */}
      <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-7 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Total Machines</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.totalMachines}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Active</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600">{mockStats.activeMachines}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Idle</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-yellow-600">{mockStats.idleMachines}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Maintenance</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-orange-600">{mockStats.maintenanceMachines}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Production</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.totalProduction}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Efficiency</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.efficiency}%</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Uptime</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.uptime}%</div>
          </CardContent>
        </Card>
      </div>

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
          {/* Alerts */}
          <Card>
            <CardHeader>
              <div className="flex justify-between items-center">
                <CardTitle className="text-base">Alerts</CardTitle>
                <Activity className="h-4 w-4 text-muted-foreground" />
              </div>
            </CardHeader>
            <CardContent className="space-y-3">
              {mockAlerts.map((alert) => (
                <div key={alert.id} className="space-y-1">
                  <div className="flex items-start gap-2">
                    <Badge
                      variant={
                        alert.type === "warning"
                          ? "destructive"
                          : alert.type === "success"
                          ? "default"
                          : "secondary"
                      }
                      className="mt-0.5"
                    >
                      {alert.type}
                    </Badge>
                    <div className="flex-1">
                      <p className="text-sm">{alert.message}</p>
                      <p className="text-xs text-muted-foreground">{alert.time}</p>
                    </div>
                  </div>
                </div>
              ))}
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