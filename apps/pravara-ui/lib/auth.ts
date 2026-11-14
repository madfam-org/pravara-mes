"use client";

/**
 * Auth bridge — maps Janua SDK to the session shape used by Pravara-MES.
 *
 * All 20+ consumer files access `session?.accessToken`, `session?.user?.tenantId`,
 * `session?.user?.role`, etc.  This bridge preserves that interface so every
 * import { usePravaraSession } from "@/lib/auth" just works.
 */

import { useEffect, useSyncExternalStore } from "react";
import { useJanua, useUser, useAuth as useJanuaAuth } from "@janua/nextjs";

export interface PravaraUser {
  id: string;
  name?: string | null;
  email?: string | null;
  image?: string | null;
  role: string;
  /**
   * The roles this application granted ("pravara-mes:<role>" entries of the
   * `roles` claim, prefix removed): what pravara-api checks on role-gated
   * routes. rolesKnown is false when the claims carry no `roles` array.
   */
  roles: string[];
  rolesKnown: boolean;
  tenantId: string;
  accessToken?: string;
}

/**
 * This application's slug in Janua application roles ("pravara-mes:admin").
 * pravara-api reads only these (docs/operations/sign-in-tokens.md), so the UI
 * does too: another product's role or a bare organization-account "admin"
 * grants nothing here and must not make the UI offer a gated action.
 */
export const APPLICATION_ROLE_PREFIX = "pravara-mes:";

/**
 * The roles this application granted, with the prefix removed, from the
 * Janua `roles` claim. `known` is false when the claims carry no `roles`
 * array at all (the UI then offers gated actions and lets the API decide).
 */
export function rolesFromClaims(claims: Record<string, unknown>): {
  roles: string[];
  known: boolean;
} {
  const roles = new Set<string>();
  if (Array.isArray(claims.roles)) {
    for (const r of claims.roles) {
      if (typeof r !== "string" || !r.startsWith(APPLICATION_ROLE_PREFIX)) continue;
      const name = r.slice(APPLICATION_ROLE_PREFIX.length);
      if (name && !name.includes(":")) roles.add(name);
    }
  }
  return {
    roles: [...roles],
    known: Array.isArray(claims.roles),
  };
}

export interface PravaraSession {
  user: PravaraUser;
  accessToken: string;
  error?: "RefreshAccessTokenError";
}

/**
 * Whether a stored sign-in is still being resolved.
 *
 * The SDK's provider (@janua/nextjs 0.1.x) reports `isLoading: false` as
 * soon as it STARTS fetching the current user, so for a moment after every
 * page load a signed-in visitor looks signed out. The protected layout acted
 * on that moment and sent everyone back to /login. This probe asks the same
 * two questions itself (is there a stored token, does it resolve to a user)
 * and holds the session at "loading" until the answer is in.
 *
 *   pending   not answered yet
 *   none      no stored token, or it did not resolve to a user
 *   user      it resolved to a user; the provider's own state follows
 *   stale     it resolved to a user but the provider never followed
 *             (PROVIDER_GRACE_MS), so stop waiting
 *
 * One probe per page load, shared by every usePravaraSession() caller.
 */
type ProbeState = "pending" | "none" | "user" | "stale";

const PROVIDER_GRACE_MS = 5000;

let probeState: ProbeState = "pending";
let probeStarted = false;
const probeListeners = new Set<() => void>();

function setProbeState(next: ProbeState) {
  probeState = next;
  for (const listener of probeListeners) listener();
}

function subscribeProbe(listener: () => void) {
  probeListeners.add(listener);
  return () => {
    probeListeners.delete(listener);
  };
}

interface ProbeClient {
  getAccessToken?: () => unknown;
  auth?: { getCurrentUser?: () => unknown };
}

function startProbe(client: ProbeClient | null | undefined) {
  if (probeStarted) return;
  probeStarted = true;
  (async () => {
    try {
      const token = await client?.getAccessToken?.();
      if (!token) return setProbeState("none");
      const user = await client?.auth?.getCurrentUser?.();
      if (!user) return setProbeState("none");
      setProbeState("user");
      setTimeout(() => {
        if (probeState === "user") setProbeState("stale");
      }, PROVIDER_GRACE_MS);
    } catch {
      setProbeState("none");
    }
  })();
}

/** Test hook: forget the probe so the next render starts a new one. */
export function resetSessionProbeForTests() {
  probeState = "pending";
  probeStarted = false;
}

/**
 * The access token to send to pravara-api.
 *
 * `client.getAccessToken()` is async in the SDK; calling it inline produced
 * the text "[object Promise]" as the bearer token. The SDK keeps the current
 * token in localStorage and rewrites it on refresh, so read it there, and
 * fall back to the token the provider resolved at load.
 */
const ACCESS_TOKEN_STORAGE_KEY = "janua_access_token";

export function currentAccessToken(fallback?: string | null): string {
  try {
    const stored = globalThis.localStorage?.getItem(ACCESS_TOKEN_STORAGE_KEY);
    if (stored) return stored;
  } catch {
    // Storage can be unavailable (private mode, blocked site data).
  }
  return typeof fallback === "string" ? fallback : "";
}

/**
 * Drop-in replacement for next-auth's usePravaraSession().
 * Returns { data: session, status } with the same shape.
 */
export function usePravaraSession(): {
  data: PravaraSession | null;
  status: "loading" | "authenticated" | "unauthenticated";
} {
  const janua = useJanua();
  const { user: januaUser } = useUser();
  const { isAuthenticated, isLoading } = useJanuaAuth();
  const probe = useSyncExternalStore(
    subscribeProbe,
    () => probeState,
    () => "pending" as ProbeState
  );

  useEffect(() => {
    startProbe(janua.client as ProbeClient | null | undefined);
  }, [janua.client]);

  if (isLoading) {
    return { data: null, status: "loading" };
  }

  if (!isAuthenticated || !januaUser) {
    // Signed out only once the probe says so (or the provider never caught up).
    if (probe === "pending" || probe === "user") {
      return { data: null, status: "loading" };
    }
    return { data: null, status: "unauthenticated" };
  }

  const providerSession = (janua as { session?: { accessToken?: unknown } }).session;
  const accessToken = currentAccessToken(
    typeof providerSession?.accessToken === "string" ? providerSession.accessToken : null
  );
  const claims = januaUser as unknown as Record<string, unknown>;
  const { roles, known: rolesKnown } = rolesFromClaims(claims);

  const session: PravaraSession = {
    user: {
      id: januaUser.id || "",
      name: januaUser.name || januaUser.display_name,
      email: januaUser.email || "",
      image: (claims.picture as string) || (claims.avatar as string) || null,
      role: (claims.role as string) || "operator",
      roles,
      rolesKnown,
      tenantId: (claims.tenant_id as string) || "",
      accessToken,
    },
    accessToken,
  };

  return { data: session, status: "authenticated" };
}

/**
 * Sign in via Janua SDK.
 */
export function pravaraSignIn(callbackUrl?: string) {
  if (typeof window !== "undefined") {
    if (callbackUrl) {
      localStorage.setItem("auth_return_url", callbackUrl);
    }
    const baseURL = process.env.NEXT_PUBLIC_JANUA_URL || "https://auth.madfam.io";
    window.location.href = `${baseURL}/authorize?redirect_uri=${encodeURIComponent(window.location.origin)}`;
  }
}

/**
 * Sign out via Janua SDK.
 */
export async function pravaraSignOut(options?: { callbackUrl?: string }) {
  if (typeof window !== "undefined") {
    const baseURL = process.env.NEXT_PUBLIC_JANUA_URL || "https://auth.madfam.io";
    window.location.href = `${baseURL}/sign-out?redirect_uri=${encodeURIComponent(options?.callbackUrl || window.location.origin + "/login")}`;
  }
}
