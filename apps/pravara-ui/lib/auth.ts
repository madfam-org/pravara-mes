"use client";

/**
 * Auth bridge — maps Janua SDK to the session shape used by Pravara-MES.
 *
 * All 20+ consumer files access `session?.accessToken`, `session?.user?.tenantId`,
 * `session?.user?.role`, etc.  This bridge preserves that interface so every
 * import { usePravaraSession } from "@/lib/auth" just works.
 */

import { useJanua, useUser, useAuth as useJanuaAuth } from "@janua/nextjs";

export interface PravaraUser {
  id: string;
  name?: string | null;
  email?: string | null;
  image?: string | null;
  role: string;
  tenantId: string;
  accessToken?: string;
}

export interface PravaraSession {
  user: PravaraUser;
  accessToken: string;
  error?: "RefreshAccessTokenError";
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

  if (isLoading) {
    return { data: null, status: "loading" };
  }

  if (!isAuthenticated || !januaUser) {
    return { data: null, status: "unauthenticated" };
  }

  const accessToken = janua.client?.getAccessToken?.() || "";
  const claims = januaUser as Record<string, unknown>;

  const session: PravaraSession = {
    user: {
      id: januaUser.id || "",
      name: januaUser.name || januaUser.display_name,
      email: januaUser.email || "",
      image: (claims.picture as string) || (claims.avatar as string) || null,
      role: (claims.role as string) || "operator",
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
