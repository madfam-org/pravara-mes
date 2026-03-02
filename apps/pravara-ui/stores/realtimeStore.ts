/**
 * Zustand store for real-time connection state
 */
import { create } from "zustand";
import { subscribeWithSelector } from "zustand/middleware";
import type { ConnectionState } from "@/lib/realtime/types";

interface RealtimeState {
  // Connection state
  connectionState: ConnectionState;
  lastConnected: Date | null;
  reconnectAttempts: number;
  error: string | null;

  // Actions
  setConnectionState: (state: ConnectionState) => void;
  setError: (error: string | null) => void;
  incrementReconnectAttempts: () => void;
  resetReconnectAttempts: () => void;
}

export const useRealtimeStore = create<RealtimeState>()(
  subscribeWithSelector((set) => ({
    // Initial state
    connectionState: "disconnected",
    lastConnected: null,
    reconnectAttempts: 0,
    error: null,

    // Actions
    setConnectionState: (state) =>
      set((prev) => ({
        connectionState: state,
        lastConnected: state === "connected" ? new Date() : prev.lastConnected,
        error: state === "connected" ? null : prev.error,
      })),

    setError: (error) => set({ error }),

    incrementReconnectAttempts: () =>
      set((state) => ({ reconnectAttempts: state.reconnectAttempts + 1 })),

    resetReconnectAttempts: () => set({ reconnectAttempts: 0 }),
  }))
);

// Selectors for common use cases
export const selectIsConnected = (state: RealtimeState) =>
  state.connectionState === "connected";

export const selectIsConnecting = (state: RealtimeState) =>
  state.connectionState === "connecting";

export const selectHasError = (state: RealtimeState) =>
  state.connectionState === "error" || state.error !== null;
