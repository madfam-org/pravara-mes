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
  /**
   * Every role the identity token names: the `roles` claim (what pravara-api
   * checks for role-gated routes) plus the single `role` claim. Empty when
   * the token carries neither; then rolesKnown is false.
   */
  roles: string[];
  rolesKnown: boolean;
  tenantId: string;
  accessToken?: string;
}

/** The roles named by Janua user claims (`roles` array and `role`). */
export function rolesFromClaims(claims: Record<string, unknown>): {
  roles: string[];
  known: boolean;
} {
  const roles = new Set<string>();
  if (Array.isArray(claims.roles)) {
    for (const r of claims.roles) if (typeof r === "string" && r) roles.add(r);
  }
  if (typeof claims.role === "string" && claims.role) roles.add(claims.role);
  return {
    roles: [...roles],
    known: Array.isArray(claims.roles) || typeof claims.role === "string",
  };
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

  const accessToken = String(janua.client?.getAccessToken?.() ?? "");
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
