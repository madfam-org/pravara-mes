"use client";

import * as React from "react";
import { useMemo } from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { cn } from "@/lib/utils";
import { useTelemetryUpdates } from "@/hooks/useTelemetryUpdates";

interface RealtimeLineChartProps {
  machineId: string;
  metricTypes: string[];
  timeWindow?: number; // ms, default 5 min
  height?: number;
  showGrid?: boolean;
  showLegend?: boolean;
  className?: string;
}

const COLORS = [
  "#3b82f6", // blue-500
  "#ef4444", // red-500
  "#22c55e", // green-500
  "#f59e0b", // amber-500
  "#8b5cf6", // violet-500
  "#06b6d4", // cyan-500
];

function formatTime(timestamp: string | number): string {
  const date = new Date(timestamp);
  return date.toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

function formatMetricName(type: string): string {
  return type
    .replace(/_/g, " ")
    .replace(/\b\w/g, (l) => l.toUpperCase());
}

interface TooltipProps {
  active?: boolean;
  payload?: Array<{
    name: string;
    value: number;
    color: string;
  }>;
  label?: string;
}

function CustomTooltip({ active, payload, label }: TooltipProps) {
  if (!active || !payload || payload.length === 0) {
    return null;
  }

  return (
    <div className="rounded-lg border bg-background p-2 shadow-md">
      <p className="text-xs font-medium text-muted-foreground mb-1">
        {label}
      </p>
      {payload.map((entry, index) => (
        <div key={index} className="flex items-center gap-2 text-sm">
          <div
            className="h-2 w-2 rounded-full"
            style={{ backgroundColor: entry.color }}
          />
          <span className="text-muted-foreground">{entry.name}:</span>
          <span className="font-medium tabular-nums">
            {entry.value.toFixed(1)}
          </span>
        </div>
      ))}
    </div>
  );
}

const RealtimeLineChart = React.memo(function RealtimeLineChart({
  machineId,
  metricTypes,
  timeWindow = 5 * 60 * 1000, // 5 minutes
  height = 200,
  showGrid = true,
  showLegend = true,
  className,
}: RealtimeLineChartProps) {
  const { getMetrics } = useTelemetryUpdates({ machineId });

  const chartData = useMemo(() => {
    const metrics = getMetrics(machineId);
    const now = Date.now();
    const windowStart = now - timeWindow;

    // Group metrics by timestamp (approximately, within 100ms)
    const timeGroups = new Map<number, Record<string, number>>();

    metrics.forEach((m) => {
      if (!metricTypes.includes(m.type)) return;

      const ts = new Date(m.timestamp).getTime();
      if (ts < windowStart) return;

      // Round to nearest 100ms for grouping
      const roundedTs = Math.floor(ts / 100) * 100;

      if (!timeGroups.has(roundedTs)) {
        timeGroups.set(roundedTs, {});
      }
      timeGroups.get(roundedTs)![m.type] = m.value;
    });

    // Convert to array sorted by time
    return Array.from(timeGroups.entries())
      .sort(([a], [b]) => a - b)
      .map(([timestamp, values]) => ({
        time: formatTime(timestamp),
        timestamp,
        ...values,
      }));
  }, [getMetrics, machineId, metricTypes, timeWindow]);

  if (chartData.length === 0) {
    return (
      <div
        className={cn(
          "flex items-center justify-center text-sm text-muted-foreground rounded-lg border bg-muted/50",
          className
        )}
        style={{ height }}
      >
        Waiting for telemetry data...
      </div>
    );
  }

  return (
    <div className={cn("w-full", className)} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart
          data={chartData}
          margin={{ top: 5, right: 20, left: 0, bottom: 5 }}
        >
          {showGrid && (
            <CartesianGrid
              strokeDasharray="3 3"
              className="stroke-muted"
              vertical={false}
            />
          )}
          <XAxis
            dataKey="time"
            tick={{ fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            className="text-muted-foreground"
          />
          <YAxis
            tick={{ fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            width={40}
            className="text-muted-foreground"
          />
          <Tooltip content={<CustomTooltip />} />
          {showLegend && (
            <Legend
              formatter={(value) => (
                <span className="text-xs">{formatMetricName(value)}</span>
              )}
              iconSize={8}
              wrapperStyle={{ fontSize: 11 }}
            />
          )}
          {metricTypes.map((type, index) => (
            <Line
              key={type}
              type="monotone"
              dataKey={type}
              name={formatMetricName(type)}
              stroke={COLORS[index % COLORS.length]}
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
              connectNulls
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
});

RealtimeLineChart.displayName = "RealtimeLineChart";

export { RealtimeLineChart, type RealtimeLineChartProps };
