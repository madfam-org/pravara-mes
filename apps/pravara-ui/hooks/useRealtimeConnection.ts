/**
 * Hook for managing real-time connection lifecycle
 */
"use client";

import { useEffect, useCallback } from "react";
import { useSession } from "next-auth/react";
import { realtimeClient } from "@/lib/realtime/client";
import {
  useRealtimeStore,
  selectIsConnected,
  selectIsConnecting,
} from "@/stores/realtimeStore";

interface UseRealtimeConnectionOptions {
  /** Auto-connect when session is available (default: true) */
  autoConnect?: boolean;
  /** Max reconnection attempts before giving up (default: 5) */
  maxReconnectAttempts?: number;
}

export function useRealtimeConnection(
  options: UseRealtimeConnectionOptions = {}
) {
  const { autoConnect = true, maxReconnectAttempts = 5 } = options;

  const { data: session, status } = useSession();
  const {
    connectionState,
    error,
    reconnectAttempts,
    setConnectionState,
    setError,
    incrementReconnectAttempts,
    resetReconnectAttempts,
  } = useRealtimeStore();

  const isConnected = selectIsConnected(useRealtimeStore.getState());
  const isConnecting = selectIsConnecting(useRealtimeStore.getState());

  // Connect to realtime server
  const connect = useCallback(async () => {
    const accessToken = session?.accessToken;
    const tenantId = session?.user?.tenantId;

    if (!accessToken || !tenantId) {
      setError("No session or tenant ID available");
      return;
    }

    if (isConnected || isConnecting) {
      return;
    }

    try {
      setError(null);
      await realtimeClient.connect(accessToken, tenantId);
      resetReconnectAttempts();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Connection failed";
      setError(message);
      incrementReconnectAttempts();
    }
  }, [
    session,
    isConnected,
    isConnecting,
    setError,
    resetReconnectAttempts,
    incrementReconnectAttempts,
  ]);

  // Disconnect from realtime server
  const disconnect = useCallback(() => {
    realtimeClient.disconnect();
    resetReconnectAttempts();
    setError(null);
  }, [resetReconnectAttempts, setError]);

  // Listen to connection state changes
  useEffect(() => {
    const unsubscribe = realtimeClient.onConnectionStateChange((state) => {
      setConnectionState(state);
    });

    return () => {
      unsubscribe();
    };
  }, [setConnectionState]);

  // Auto-connect when session is available
  useEffect(() => {
    const hasToken = Boolean(session?.accessToken);
    if (
      autoConnect &&
      status === "authenticated" &&
      hasToken &&
      connectionState === "disconnected" &&
      reconnectAttempts < maxReconnectAttempts
    ) {
      connect();
    }
  }, [
    autoConnect,
    status,
    session,
    connectionState,
    reconnectAttempts,
    maxReconnectAttempts,
    connect,
  ]);

  // Disconnect on unmount or session end
  useEffect(() => {
    if (status === "unauthenticated") {
      disconnect();
    }

    return () => {
      // Don't disconnect on unmount if we want to keep connection alive
      // disconnect();
    };
  }, [status, disconnect]);

  return {
    connectionState,
    isConnected,
    isConnecting,
    error,
    reconnectAttempts,
    connect,
    disconnect,
  };
}
