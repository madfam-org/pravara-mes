"use client";

import { SignIn } from "@janua/nextjs";

/**
 * Login — Janua SSO via the SDK's <SignIn> card.
 *
 * Previously this rendered a button that navigated to
 * `${JANUA_URL}/authorize?redirect_uri=…`, which 404s: it lacks the `/api/v1`
 * prefix, a `client_id`, `response_type=code`, and PKCE, so the primary login
 * path was dead. The `@janua/nextjs` <SignIn> component runs the real
 * OIDC/PKCE/SSO flow, reading `NEXT_PUBLIC_JANUA_CLIENT_ID` /
 * `NEXT_PUBLIC_JANUA_REDIRECT_URI` from the environment (already provisioned in
 * pravara-secrets). It renders inside the JanuaProvider set up in providers.tsx.
 */
export default function LoginPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/50 p-4">
      <SignIn
        enableJanuaSSO
        redirectTo="/dashboard"
        headerText="PravaraMES"
        headerDescription="Sign in to access your manufacturing dashboard"
      />
    </div>
  );
}
