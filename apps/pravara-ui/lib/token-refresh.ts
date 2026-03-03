"use client";

import { signIn, useSession } from "next-auth/react";
import { useEffect } from "react";

/**
 * Hook that detects token refresh failures and redirects to login.
 * Use in a client component that wraps protected routes.
 */
export function useTokenRefreshGuard() {
  const { data: session } = useSession();

  useEffect(() => {
    if ((session as any)?.error === "RefreshAccessTokenError") {
      // Force re-login when refresh token is invalid/expired
      signIn("janua", { callbackUrl: window.location.href });
    }
  }, [session]);
}
