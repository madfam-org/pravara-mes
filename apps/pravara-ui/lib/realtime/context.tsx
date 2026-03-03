"use client";

import {
  createContext,
  useContext,
  useEffect,
  useState,
  useCallback,
  type ReactNode,
} from "react";
import { usePravaraSession } from "@/lib/auth";
import { realtimeClient, type EventListener } from "./client";
import type { ConnectionState, ChannelNamespace, EventType, RealtimeEvent } from "./types";
import { ChannelNamespaces } from "./types";

interface RealtimeContextValue {
  connectionState: ConnectionState;
  isConnected: boolean;
  subscribe: <T = unknown>(eventType: EventType, handler: (event: RealtimeEvent<T>) => void) => void;
  unsubscribe: <T = unknown>(eventType: EventType, handler: (event: RealtimeEvent<T>) => void) => void;
}

const RealtimeContext = createContext<RealtimeContextValue | null>(null);

// Map event types to their channel namespaces
function getNamespaceForEventType(eventType: EventType): ChannelNamespace {
  if (eventType.startsWith("machine.")) return ChannelNamespaces.MACHINES;
  if (eventType.startsWith("task.")) return ChannelNamespaces.TASKS;
  if (eventType.startsWith("order.")) return ChannelNamespaces.ORDERS;
  if (eventType.startsWith("notification.")) return ChannelNamespaces.NOTIFICATIONS;
  return ChannelNamespaces.TELEMETRY;
}

export function RealtimeProvider({ children }: { children: ReactNode }) {
  const { data: session } = usePravaraSession();
  const [connectionState, setConnectionState] = useState<ConnectionState>("disconnected");
  const [eventHandlers] = useState(() => new Map<EventType, Set<EventListener>>());

  // Connect/disconnect based on session
  useEffect(() => {
    const user = session?.user as any;
    const token = user?.accessToken;
    const tenantId = user?.tenantId;

    if (!token || !tenantId) {
      realtimeClient.disconnect();
      return;
    }

    realtimeClient.connect(token, tenantId).catch((err) => {
      console.error("[Realtime] Connection failed:", err);
    });

    const unsubscribeState = realtimeClient.onConnectionStateChange((state) => {
      setConnectionState(state);
    });

    return () => {
      unsubscribeState();
      realtimeClient.disconnect();
    };
  }, [session]);

  // Subscribe to namespace and filter by event type
  const subscribe = useCallback(<T = unknown>(
    eventType: EventType,
    handler: (event: RealtimeEvent<T>) => void
  ) => {
    // Add to local handler registry
    if (!eventHandlers.has(eventType)) {
      eventHandlers.set(eventType, new Set());
    }
    eventHandlers.get(eventType)!.add(handler as EventListener);

    // Subscribe to the namespace if connected
    if (connectionState === "connected") {
      const namespace = getNamespaceForEventType(eventType);
      realtimeClient.subscribe(namespace, (event: RealtimeEvent) => {
        if (event.type === eventType) {
          const handlers = eventHandlers.get(eventType);
          if (handlers) {
            handlers.forEach((h) => h(event));
          }
        }
      });
    }
  }, [connectionState, eventHandlers]);

  const unsubscribe = useCallback(<T = unknown>(
    eventType: EventType,
    handler: (event: RealtimeEvent<T>) => void
  ) => {
    const handlers = eventHandlers.get(eventType);
    if (handlers) {
      handlers.delete(handler as EventListener);
      if (handlers.size === 0) {
        eventHandlers.delete(eventType);
      }
    }
  }, [eventHandlers]);

  const value: RealtimeContextValue = {
    connectionState,
    isConnected: connectionState === "connected",
    subscribe,
    unsubscribe,
  };

  return (
    <RealtimeContext.Provider value={value}>{children}</RealtimeContext.Provider>
  );
}

export function useRealtime(): RealtimeContextValue {
  const context = useContext(RealtimeContext);
  if (!context) {
    // Return a no-op context when not wrapped in provider
    return {
      connectionState: "disconnected",
      isConnected: false,
      subscribe: () => {},
      unsubscribe: () => {},
    };
  }
  return context;
}
