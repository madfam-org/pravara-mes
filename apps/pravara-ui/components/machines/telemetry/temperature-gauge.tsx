"use client";

import * as React from "react";
import { cn } from "@/lib/utils";

interface TemperatureGaugeProps {
  value: number;
  target?: number;
  min?: number;
  max?: number;
  unit?: string;
  label: string;
  className?: string;
}

function getTemperatureColor(value: number, max: number): string {
  const ratio = value / max;
  if (ratio >= 0.8) return "#ef4444"; // red-500
  if (ratio >= 0.6) return "#f97316"; // orange-500
  if (ratio >= 0.4) return "#eab308"; // yellow-500
  return "#22c55e"; // green-500
}

const TemperatureGauge = React.memo(function TemperatureGauge({
  value,
  target,
  min = 0,
  max = 300,
  unit = "°C",
  label,
  className,
}: TemperatureGaugeProps) {
  const size = 120;
  const strokeWidth = 12;
  const center = size / 2;
  const radius = (size - strokeWidth) / 2 - 5;

  // Semi-circle arc (180 degrees = π radians)
  const startAngle = Math.PI;
  const endAngle = 2 * Math.PI;
  const angleRange = endAngle - startAngle;

  // Clamp value to range
  const clampedValue = Math.min(max, Math.max(min, value));
  const valueRatio = (clampedValue - min) / (max - min);
  const valueAngle = startAngle + valueRatio * angleRange;

  // Target indicator
  const targetRatio = target !== undefined ? (Math.min(max, Math.max(min, target)) - min) / (max - min) : null;
  const targetAngle = targetRatio !== null ? startAngle + targetRatio * angleRange : null;

  // Arc path calculations
  const describeArc = (startAng: number, endAng: number) => {
    const startX = center + radius * Math.cos(startAng);
    const startY = center + radius * Math.sin(startAng);
    const endX = center + radius * Math.cos(endAng);
    const endY = center + radius * Math.sin(endAng);
    const largeArc = endAng - startAng > Math.PI ? 1 : 0;

    return `M ${startX} ${startY} A ${radius} ${radius} 0 ${largeArc} 1 ${endX} ${endY}`;
  };

  const color = getTemperatureColor(clampedValue, max);

  return (
    <div className={cn("flex flex-col items-center", className)}>
      <div className="relative" style={{ width: size, height: size / 2 + 20 }}>
        <svg
          width={size}
          height={size / 2 + 20}
          viewBox={`0 0 ${size} ${size / 2 + 20}`}
          aria-label={`${label}: ${clampedValue}${unit}`}
          role="meter"
          aria-valuenow={clampedValue}
          aria-valuemin={min}
          aria-valuemax={max}
        >
          {/* Background arc */}
          <path
            d={describeArc(startAngle, endAngle)}
            fill="none"
            strokeWidth={strokeWidth}
            strokeLinecap="round"
            className="stroke-muted"
          />
          {/* Value arc */}
          {valueRatio > 0 && (
            <path
              d={describeArc(startAngle, valueAngle)}
              fill="none"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap="round"
              className="transition-all duration-300 ease-out"
            />
          )}
          {/* Target indicator */}
          {targetAngle !== null && (
            <line
              x1={center + (radius - strokeWidth / 2 - 2) * Math.cos(targetAngle)}
              y1={center + (radius - strokeWidth / 2 - 2) * Math.sin(targetAngle)}
              x2={center + (radius + strokeWidth / 2 + 2) * Math.cos(targetAngle)}
              y2={center + (radius + strokeWidth / 2 + 2) * Math.sin(targetAngle)}
              strokeWidth={3}
              strokeLinecap="round"
              className="stroke-foreground"
            />
          )}
          {/* Min/Max labels */}
          <text
            x={center - radius}
            y={center + 15}
            textAnchor="middle"
            className="fill-muted-foreground text-[10px]"
          >
            {min}
          </text>
          <text
            x={center + radius}
            y={center + 15}
            textAnchor="middle"
            className="fill-muted-foreground text-[10px]"
          >
            {max}
          </text>
        </svg>
        {/* Center value display */}
        <div
          className="absolute left-1/2 -translate-x-1/2"
          style={{ bottom: 0 }}
        >
          <div className="text-center">
            <span className="text-2xl font-bold tabular-nums" style={{ color }}>
              {clampedValue.toFixed(0)}
            </span>
            <span className="text-sm text-muted-foreground">{unit}</span>
          </div>
        </div>
      </div>
      <span className="text-xs text-muted-foreground mt-1">{label}</span>
      {target !== undefined && (
        <span className="text-[10px] text-muted-foreground">
          Target: {target}{unit}
        </span>
      )}
    </div>
  );
});

TemperatureGauge.displayName = "TemperatureGauge";

export { TemperatureGauge, type TemperatureGaugeProps };
