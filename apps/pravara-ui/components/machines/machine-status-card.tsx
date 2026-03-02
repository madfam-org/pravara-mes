"use client";

import { Activity, Wifi, WifiOff, Clock, MapPin, Cpu } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { type Machine, type MachineStatus } from "@/lib/api";
import { formatRelativeTime } from "@/lib/utils";
import { useTelemetryUpdates } from "@/hooks/useTelemetryUpdates";
import {
  ProgressRing,
  TemperatureGauge,
  MetricSparkline,
} from "@/components/machines/telemetry";

interface MachineStatusCardProps {
  machine: Machine;
}

const statusConfig: Record<MachineStatus, { variant: "default" | "secondary" | "destructive" | "outline" | "success" | "warning" | "error"; label: string }> = {
  offline: {
    variant: "secondary",
    label: "Offline",
  },
  online: {
    variant: "success",
    label: "Online",
  },
  idle: {
    variant: "warning",
    label: "Idle",
  },
  running: {
    variant: "default",
    label: "Running",
  },
  maintenance: {
    variant: "warning",
    label: "Maintenance",
  },
  error: {
    variant: "error",
    label: "Error",
  },
};

export function MachineStatusCard({ machine }: MachineStatusCardProps) {
  const config = statusConfig[machine.status] || statusConfig.offline;
  const StatusIcon = machine.status === "offline" ? WifiOff : Wifi;

  const { getLatestMetric } = useTelemetryUpdates({ machineId: machine.id });

  // Get latest metrics for display
  const hotendTemp = getLatestMetric(machine.id, "hotend_temp");
  const bedTemp = getLatestMetric(machine.id, "bed_temp");
  const progressMetric = getLatestMetric(machine.id, "job_progress");

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Status</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Status Badge */}
        <div className="flex items-center gap-3">
          <Badge variant={config.variant} className="gap-1.5">
            <StatusIcon className="h-3.5 w-3.5" />
            {config.label}
          </Badge>
          {machine.status === "running" && (
            <Badge variant="outline" className="gap-1">
              <Activity className="h-3 w-3 animate-pulse" />
              Active
            </Badge>
          )}
        </div>

        {/* Machine Info */}
        <div className="grid gap-3 text-sm">
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground flex items-center gap-2">
              <Cpu className="h-4 w-4" />
              Type
            </span>
            <span className="font-medium">{machine.type}</span>
          </div>

          {machine.location && (
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground flex items-center gap-2">
                <MapPin className="h-4 w-4" />
                Location
              </span>
              <span className="font-medium">{machine.location}</span>
            </div>
          )}

          <div className="flex items-center justify-between">
            <span className="text-muted-foreground flex items-center gap-2">
              <Clock className="h-4 w-4" />
              Last Seen
            </span>
            <span className="font-medium">
              {machine.last_heartbeat
                ? formatRelativeTime(machine.last_heartbeat)
                : "Never"}
            </span>
          </div>
        </div>

        {/* Live Telemetry (if running) */}
        {machine.status === "running" && (hotendTemp || bedTemp || progressMetric) && (
          <>
            <Separator />
            <div>
              <p className="text-xs text-muted-foreground mb-3">Live Telemetry</p>

              {/* Job Progress with Progress Ring */}
              {progressMetric && (
                <div className="flex flex-col items-center mb-4">
                  <ProgressRing
                    value={progressMetric.value}
                    size={100}
                    strokeWidth={10}
                    label="Job Progress"
                    showValue
                  />
                </div>
              )}

              {/* Temperature gauges with sparklines */}
              {(hotendTemp || bedTemp) && (
                <div className="grid grid-cols-2 gap-4">
                  {hotendTemp && (
                    <div className="flex flex-col items-center gap-2">
                      <TemperatureGauge
                        value={hotendTemp.value}
                        target={220}
                        max={300}
                        label="Hotend"
                      />
                      <MetricSparkline
                        machineId={machine.id}
                        metricType="hotend_temp"
                        dataPoints={20}
                        height={30}
                        color="#ef4444"
                      />
                    </div>
                  )}
                  {bedTemp && (
                    <div className="flex flex-col items-center gap-2">
                      <TemperatureGauge
                        value={bedTemp.value}
                        target={60}
                        max={120}
                        label="Bed"
                      />
                      <MetricSparkline
                        machineId={machine.id}
                        metricType="bed_temp"
                        dataPoints={20}
                        height={30}
                        color="#f59e0b"
                      />
                    </div>
                  )}
                </div>
              )}
            </div>
          </>
        )}

        {/* MQTT Topic */}
        {machine.mqtt_topic && (
          <>
            <Separator />
            <div>
              <p className="text-xs text-muted-foreground mb-1">MQTT Topic</p>
              <code className="text-xs bg-muted px-2 py-1 rounded block overflow-x-auto">
                {machine.mqtt_topic}
              </code>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
