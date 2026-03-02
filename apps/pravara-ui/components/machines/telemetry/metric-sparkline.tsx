"use client";

import * as React from "react";
import { useMemo } from "react";
import { AreaChart, Area, ResponsiveContainer } from "recharts";
import { cn } from "@/lib/utils";
import { useTelemetryUpdates } from "@/hooks/useTelemetryUpdates";

interface MetricSparklineProps {
  machineId: string;
  metricType: string;
  dataPoints?: number;
  height?: number;
  color?: string;
  className?: string;
}

const MetricSparkline = React.memo(function MetricSparkline({
  machineId,
  metricType,
  dataPoints = 30,
  height = 40,
  color = "#3b82f6", // blue-500
  className,
}: MetricSparklineProps) {
  const { getMetrics } = useTelemetryUpdates({ machineId });

  const chartData = useMemo(() => {
    const metrics = getMetrics(machineId);
    const filtered = metrics
      .filter((m) => m.type === metricType)
      .slice(-dataPoints);

    return filtered.map((m, index) => ({
      index,
      value: m.value,
      timestamp: m.timestamp,
    }));
  }, [getMetrics, machineId, metricType, dataPoints]);

  if (chartData.length === 0) {
    return (
      <div
        className={cn(
          "flex items-center justify-center text-xs text-muted-foreground",
          className
        )}
        style={{ height }}
      >
        No data
      </div>
    );
  }

  const gradientId = `sparkline-gradient-${machineId}-${metricType}`;

  return (
    <div className={cn("w-full", className)} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart
          data={chartData}
          margin={{ top: 0, right: 0, left: 0, bottom: 0 }}
        >
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.3} />
              <stop offset="100%" stopColor={color} stopOpacity={0.05} />
            </linearGradient>
          </defs>
          <Area
            type="monotone"
            dataKey="value"
            stroke={color}
            strokeWidth={1.5}
            fill={`url(#${gradientId})`}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
});

MetricSparkline.displayName = "MetricSparkline";

export { MetricSparkline, type MetricSparklineProps };
