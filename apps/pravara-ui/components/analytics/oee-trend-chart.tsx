"use client";

import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from "recharts";
import type { OEESnapshot } from "@/lib/api";

interface OEETrendChartProps {
  data: OEESnapshot[];
  height?: number;
}

export function OEETrendChart({ data, height = 300 }: OEETrendChartProps) {
  if (data.length === 0) {
    return (
      <div
        className="flex items-center justify-center text-muted-foreground text-sm"
        style={{ height }}
      >
        No OEE trend data available
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={data}>
        <CartesianGrid strokeDasharray="3 3" />
        <XAxis
          dataKey="snapshot_date"
          tick={{ fontSize: 12 }}
          tickFormatter={(v: string) => {
            const d = new Date(v);
            return `${d.getMonth() + 1}/${d.getDate()}`;
          }}
        />
        <YAxis
          domain={[0, 1]}
          tickFormatter={(v: number) => `${(v * 100).toFixed(0)}%`}
          tick={{ fontSize: 12 }}
        />
        <Tooltip
          formatter={(v: number) => `${(v * 100).toFixed(1)}%`}
          labelFormatter={(label: string) => {
            const d = new Date(label);
            return d.toLocaleDateString("en-US", {
              year: "numeric",
              month: "short",
              day: "numeric",
            });
          }}
        />
        <Line
          type="monotone"
          dataKey="oee"
          stroke="#8884d8"
          name="OEE"
          strokeWidth={2}
          dot={false}
        />
        <Line
          type="monotone"
          dataKey="availability"
          stroke="#82ca9d"
          name="Availability"
          strokeWidth={1.5}
          dot={false}
        />
        <Line
          type="monotone"
          dataKey="performance"
          stroke="#ffc658"
          name="Performance"
          strokeWidth={1.5}
          dot={false}
        />
        <Line
          type="monotone"
          dataKey="quality"
          stroke="#ff7300"
          name="Quality"
          strokeWidth={1.5}
          dot={false}
        />
        <Legend />
      </LineChart>
    </ResponsiveContainer>
  );
}
