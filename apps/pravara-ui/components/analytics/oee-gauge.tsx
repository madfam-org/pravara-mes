"use client";

import { cn } from "@/lib/utils";

interface OEEGaugeProps {
  value: number;
  label: string;
  size?: "sm" | "md" | "lg";
}

const sizeConfig = {
  sm: { width: 100, radius: 35, strokeWidth: 6, fontSize: "text-lg", labelSize: "text-[10px]" },
  md: { width: 140, radius: 45, strokeWidth: 8, fontSize: "text-2xl", labelSize: "text-xs" },
  lg: { width: 180, radius: 55, strokeWidth: 10, fontSize: "text-3xl", labelSize: "text-sm" },
};

export function OEEGauge({ value, label, size = "md" }: OEEGaugeProps) {
  const percentage = Math.round(value * 100);
  const config = sizeConfig[size];
  const circumference = 2 * Math.PI * config.radius;
  const strokeDashoffset = circumference - value * circumference;
  const color =
    percentage >= 85
      ? "text-green-500"
      : percentage >= 60
        ? "text-yellow-500"
        : "text-red-500";
  const trackColor = "text-muted/30";

  return (
    <div className="flex flex-col items-center gap-1">
      <svg
        width={config.width}
        height={config.width}
        viewBox={`0 0 ${config.width} ${config.width}`}
        className="-rotate-90"
        role="img"
        aria-label={`${label}: ${percentage}%`}
      >
        {/* Background track */}
        <circle
          cx={config.width / 2}
          cy={config.width / 2}
          r={config.radius}
          fill="none"
          strokeWidth={config.strokeWidth}
          className={cn("stroke-current", trackColor)}
        />
        {/* Value arc */}
        <circle
          cx={config.width / 2}
          cy={config.width / 2}
          r={config.radius}
          fill="none"
          strokeWidth={config.strokeWidth}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={strokeDashoffset}
          className={cn("stroke-current transition-all duration-700 ease-out", color)}
        />
      </svg>
      {/* Centered percentage text (positioned over the SVG) */}
      <div
        className="flex flex-col items-center justify-center"
        style={{ marginTop: -config.width / 2 - config.radius / 3 }}
      >
        <span className={cn("font-bold tabular-nums", config.fontSize, color)}>
          {percentage}%
        </span>
      </div>
      {/* Bottom spacer to account for the negative margin */}
      <div style={{ height: config.width / 2 - config.radius / 3 - 8 }} />
      <span className={cn("font-medium text-muted-foreground", config.labelSize)}>
        {label}
      </span>
    </div>
  );
}
