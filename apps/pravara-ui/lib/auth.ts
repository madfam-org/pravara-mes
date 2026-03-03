import NextAuth from "next-auth";
import type { NextAuthConfig } from "next-auth";

async function refreshAccessToken(token: any) {
  try {
    const response = await fetch(
      `${process.env.OIDC_ISSUER}/protocol/openid-connect/token`,
      {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({
          client_id: process.env.OIDC_CLIENT_ID!,
          client_secret: process.env.OIDC_CLIENT_SECRET!,
          grant_type: "refresh_token",
          refresh_token: token.refreshToken,
        }),
      }
    );

    const refreshed = await response.json();

    if (!response.ok) {
      throw new Error(refreshed.error || "Failed to refresh token");
    }

    return {
      ...token,
      accessToken: refreshed.access_token,
      accessTokenExpires: Date.now() + refreshed.expires_in * 1000,
      refreshToken: refreshed.refresh_token ?? token.refreshToken,
    };
  } catch (error) {
    console.error("Error refreshing access token:", error);
    return {
      ...token,
      error: "RefreshAccessTokenError" as const,
    };
  }
}

const config: NextAuthConfig = {
  providers: [
    {
      id: "janua",
      name: "Janua SSO",
      type: "oidc",
      issuer: process.env.OIDC_ISSUER,
      clientId: process.env.OIDC_CLIENT_ID,
      clientSecret: process.env.OIDC_CLIENT_SECRET,
      authorization: {
        params: {
          scope: "openid profile email offline_access",
        },
      },
      profile(profile) {
        return {
          id: profile.sub,
          name: profile.name,
          email: profile.email,
          image: profile.picture,
          role: profile.role || "operator",
          tenantId: profile.tenant_id,
        };
      },
    },
  ],
  callbacks: {
    async jwt({ token, account, profile }) {
      // Initial sign-in: capture tokens and expiry
      if (account && profile) {
        return {
          ...token,
          accessToken: account.access_token,
          accessTokenExpires: account.expires_at
            ? account.expires_at * 1000
            : Date.now() + 3600 * 1000,
          refreshToken: account.refresh_token,
          role: (profile as any).role || "operator",
          tenantId: (profile as any).tenant_id,
        };
      }

      // Return token if still valid (with 60s buffer)
      if (
        typeof token.accessTokenExpires === "number" &&
        Date.now() < token.accessTokenExpires - 60 * 1000
      ) {
        return token;
      }

      // Token expired, attempt refresh
      if (token.refreshToken) {
        return refreshAccessToken(token);
      }

      return token;
    },
    async session({ session, token }) {
      if (session.user) {
        (session.user as any).id = token.sub;
        (session.user as any).accessToken = token.accessToken;
        (session.user as any).role = token.role;
        (session.user as any).tenantId = token.tenantId;
      }
      if (token.error) {
        (session as any).error = token.error;
      }
      return session;
    },
  },
  pages: {
    signIn: "/login",
    error: "/login",
  },
};

export const { handlers, auth, signIn, signOut } = NextAuth(config);
