/**
 * Hook for real-time telemetry updates with React Query integration
 */
"use client";

import { useEffect, useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { subscribeTelemetry } from "@/lib/realtime/channels";
import type { TelemetryBatchData, TelemetryMetric } from "@/lib/realtime/types";
import { useRealtimeStore, selectIsConnected } from "@/stores/realtimeStore";

interface UseTelemetryUpdatesOptions {
  /** Filter to specific machine ID */
  machineId?: string;
  /** Called when telemetry batch is received */
  onBatch?: (data: TelemetryBatchData) => void;
  /** Keep last N metrics in memory per machine (default: 100) */
  bufferSize?: number;
}

interface TelemetryBuffer {
  machineId: string;
  metrics: TelemetryMetric[];
  lastUpdated: Date;
}

export function useTelemetryUpdates(options: UseTelemetryUpdatesOptions = {}) {
  const { machineId, onBatch, bufferSize = 100 } = options;
  const queryClient = useQueryClient();
  const isConnected = useRealtimeStore(selectIsConnected);

  // Local buffer for recent telemetry data
  const [buffer, setBuffer] = useState<Map<string, TelemetryBuffer>>(new Map());

  // Handle telemetry batch event
  const handleBatch = useCallback(
    (data: TelemetryBatchData) => {
      // Filter by machine if specified
      if (machineId && data.machine_id !== machineId) {
        return;
      }

      // Update local buffer
      setBuffer((prev) => {
        const newBuffer = new Map(prev);
        const existing = newBuffer.get(data.machine_id);

        const newMetrics = existing
          ? [...existing.metrics, ...data.metrics].slice(-bufferSize)
          : data.metrics.slice(-bufferSize);

        newBuffer.set(data.machine_id, {
          machineId: data.machine_id,
          metrics: newMetrics,
          lastUpdated: new Date(data.received_at),
        });

        return newBuffer;
      });

      // Invalidate telemetry queries for this machine
      queryClient.invalidateQueries({
        queryKey: ["machines", data.machine_id, "telemetry"],
      });

      onBatch?.(data);
    },
    [machineId, bufferSize, queryClient, onBatch]
  );

  // Subscribe to telemetry events
  useEffect(() => {
    if (!isConnected) return;

    const unsubscribe = subscribeTelemetry(handleBatch);

    return () => {
      unsubscribe();
    };
  }, [isConnected, handleBatch]);

  // Get buffered metrics for a specific machine
  const getMetrics = useCallback(
    (targetMachineId: string): TelemetryMetric[] => {
      return buffer.get(targetMachineId)?.metrics || [];
    },
    [buffer]
  );

  // Get latest metric value for a machine and metric type
  const getLatestMetric = useCallback(
    (targetMachineId: string, metricType: string): TelemetryMetric | null => {
      const machineBuffer = buffer.get(targetMachineId);
      if (!machineBuffer) return null;

      for (let i = machineBuffer.metrics.length - 1; i >= 0; i--) {
        if (machineBuffer.metrics[i].type === metricType) {
          return machineBuffer.metrics[i];
        }
      }
      return null;
    },
    [buffer]
  );

  // Clear buffer for a machine or all machines
  const clearBuffer = useCallback((targetMachineId?: string) => {
    setBuffer((prev) => {
      if (targetMachineId) {
        const newBuffer = new Map(prev);
        newBuffer.delete(targetMachineId);
        return newBuffer;
      }
      return new Map();
    });
  }, []);

  return {
    isConnected,
    buffer,
    getMetrics,
    getLatestMetric,
    clearBuffer,
  };
}
