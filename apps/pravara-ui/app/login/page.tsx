"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { SignIn } from "@janua/nextjs";
import { usePravaraSession } from "@/lib/auth";

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
 *
 * Email and password only. The card's Google, GitHub and "Sign in with Janua"
 * buttons all start at Janua's social-login route, which refuses this app's
 * address ("Redirect URL domain not allowed"), so every one of them ended in a
 * connection error. They stay off until that route accepts the app and the
 * app has a callback page to return to.
 *
 * The wrapper puts the card's inputs on the page background: on the SDK's
 * default input fill the placeholder text measured 4.4:1, under the 4.5:1
 * minimum.
 */
export default function LoginPage() {
  const { status } = usePravaraSession();
  const router = useRouter();

  // Someone already signed in has no use for this page.
  useEffect(() => {
    if (status === "authenticated") router.replace("/dashboard");
  }, [status, router]);

  return (
    <div className="flex min-h-dvh items-center justify-center bg-muted/50 p-4 [&_input]:bg-background">
      <SignIn
        socialProviders={{ google: false, github: false }}
        redirectTo="/dashboard"
        headerText="PravaraMES"
        headerDescription="Sign in to access your manufacturing dashboard"
      />
    </div>
  );
}
