/**
 * Centrifuge client for real-time WebSocket connections
 */
import { Centrifuge, Subscription, PublicationContext } from "centrifuge";
import type {
  ConnectionState,
  RealtimeTokenResponse,
  ChannelNamespace,
  RealtimeEvent,
} from "./types";

const GATEWAY_URL =
  process.env.NEXT_PUBLIC_REALTIME_URL || "ws://localhost:8000/connection/websocket";
const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4500";

export type ConnectionStateListener = (state: ConnectionState) => void;
export type EventListener<T = unknown> = (event: RealtimeEvent<T>) => void;

interface SubscriptionInfo {
  subscription: Subscription;
  listeners: Set<EventListener>;
}

class RealtimeClient {
  private client: Centrifuge | null = null;
  private subscriptions: Map<string, SubscriptionInfo> = new Map();
  private connectionListeners: Set<ConnectionStateListener> = new Set();
  private token: string | null = null;
  private tenantId: string | null = null;
  private connectionState: ConnectionState = "disconnected";
  private tokenRefreshTimer: NodeJS.Timeout | null = null;

  /**
   * Initialize the realtime client with authentication
   */
  async connect(authToken: string, tenantId: string): Promise<void> {
    if (this.client?.state === "connected") {
      return;
    }

    this.token = authToken;
    this.tenantId = tenantId;

    // Fetch Centrifugo token from API
    const tokenResponse = await this.fetchToken();

    this.client = new Centrifuge(tokenResponse.url || GATEWAY_URL, {
      token: tokenResponse.token,
      getToken: async () => {
        const response = await this.fetchToken();
        return response.token;
      },
    });

    this.setupEventHandlers();
    this.client.connect();
    this.scheduleTokenRefresh(tokenResponse.expires_at);
  }

  /**
   * Disconnect from the realtime server
   */
  disconnect(): void {
    if (this.tokenRefreshTimer) {
      clearTimeout(this.tokenRefreshTimer);
      this.tokenRefreshTimer = null;
    }

    // Unsubscribe from all channels
    this.subscriptions.forEach((info, channel) => {
      info.subscription.unsubscribe();
      this.subscriptions.delete(channel);
    });

    if (this.client) {
      this.client.disconnect();
      this.client = null;
    }

    this.updateConnectionState("disconnected");
  }

  /**
   * Subscribe to a channel namespace
   */
  subscribe<T = unknown>(
    namespace: ChannelNamespace,
    listener: EventListener<T>
  ): () => void {
    if (!this.client || !this.tenantId) {
      throw new Error("Client not connected. Call connect() first.");
    }

    const channel = `${namespace}:${this.tenantId}`;
    let subInfo = this.subscriptions.get(channel);

    if (!subInfo) {
      const subscription = this.client.newSubscription(channel);

      subscription.on("publication", (ctx: PublicationContext) => {
        const event = ctx.data as RealtimeEvent<T>;
        const info = this.subscriptions.get(channel);
        if (info) {
          info.listeners.forEach((l) => l(event as RealtimeEvent));
        }
      });

      subscription.on("subscribed", () => {
        console.log(`[Realtime] Subscribed to ${channel}`);
      });

      subscription.on("error", (ctx) => {
        console.error(`[Realtime] Subscription error on ${channel}:`, ctx);
      });

      subscription.subscribe();

      subInfo = {
        subscription,
        listeners: new Set(),
      };
      this.subscriptions.set(channel, subInfo);
    }

    subInfo.listeners.add(listener as EventListener);

    // Return unsubscribe function
    return () => {
      const info = this.subscriptions.get(channel);
      if (info) {
        info.listeners.delete(listener as EventListener);
        // If no more listeners, unsubscribe from channel
        if (info.listeners.size === 0) {
          info.subscription.unsubscribe();
          this.subscriptions.delete(channel);
        }
      }
    };
  }

  /**
   * Get current connection state
   */
  getConnectionState(): ConnectionState {
    return this.connectionState;
  }

  /**
   * Add connection state listener
   */
  onConnectionStateChange(listener: ConnectionStateListener): () => void {
    this.connectionListeners.add(listener);
    // Immediately call with current state
    listener(this.connectionState);
    return () => {
      this.connectionListeners.delete(listener);
    };
  }

  private async fetchToken(): Promise<RealtimeTokenResponse> {
    if (!this.token) {
      throw new Error("No auth token available");
    }

    const response = await fetch(`${API_BASE_URL}/v1/realtime/token`, {
      headers: {
        Authorization: `Bearer ${this.token}`,
        "Content-Type": "application/json",
      },
    });

    if (!response.ok) {
      throw new Error(`Failed to fetch realtime token: ${response.status}`);
    }

    return response.json();
  }

  private setupEventHandlers(): void {
    if (!this.client) return;

    this.client.on("connecting", () => {
      this.updateConnectionState("connecting");
    });

    this.client.on("connected", () => {
      this.updateConnectionState("connected");
      console.log("[Realtime] Connected to server");
    });

    this.client.on("disconnected", () => {
      this.updateConnectionState("disconnected");
      console.log("[Realtime] Disconnected from server");
    });

    this.client.on("error", (ctx) => {
      console.error("[Realtime] Connection error:", ctx);
      this.updateConnectionState("error");
    });
  }

  private updateConnectionState(state: ConnectionState): void {
    this.connectionState = state;
    this.connectionListeners.forEach((listener) => listener(state));
  }

  private scheduleTokenRefresh(expiresAt: number): void {
    if (this.tokenRefreshTimer) {
      clearTimeout(this.tokenRefreshTimer);
    }

    // Refresh 5 minutes before expiry
    const refreshTime = (expiresAt - Date.now() / 1000 - 300) * 1000;
    if (refreshTime > 0) {
      this.tokenRefreshTimer = setTimeout(async () => {
        try {
          await this.fetchToken();
        } catch (error) {
          console.error("[Realtime] Failed to refresh token:", error);
        }
      }, refreshTime);
    }
  }
}

// Singleton instance
export const realtimeClient = new RealtimeClient();
