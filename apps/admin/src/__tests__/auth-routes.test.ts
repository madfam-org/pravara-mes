/**
 * Auth Routes Unit Tests — PravaraMES Admin
 *
 * Tests cover the four auth API routes and the middleware:
 *   - /api/auth/sso       — PKCE initiation, cookie storage, Janua redirect
 *   - /api/auth/callback  — code exchange, JWT session creation, error handling
 *   - /api/auth/me        — session validation, expiry checks
 *   - /api/auth/logout    — session cookie clearing
 *   - middleware           — public path passthrough, protected path enforcement
 *
 * Strategy:
 *   We mock `next/headers` (cookies) and `next/server` (NextResponse, NextRequest)
 *   at the module level so the route handlers operate against in-memory cookie stores.
 *   External fetch calls (token exchange, userinfo) are mocked via vi.stubGlobal.
 *   jose is mocked for the callback route's SignJWT, and its jwtVerify is mocked
 *   for the /me route.
 */

import { describe, it, expect, vi, beforeEach, type Mock } from "vitest";

// ---------------------------------------------------------------------------
// Mock cookie store — shared between all route handlers
// ---------------------------------------------------------------------------
const cookieJar = new Map<string, string>();

const mockCookieStore = {
  get: vi.fn((name: string) => {
    const value = cookieJar.get(name);
    return value !== undefined ? { name, value } : undefined;
  }),
  set: vi.fn((name: string, value: string, _opts?: unknown) => {
    cookieJar.set(name, value);
  }),
  delete: vi.fn((name: string) => {
    cookieJar.delete(name);
  }),
};

// Mock next/headers — all routes import `cookies` from here
vi.mock("next/headers", () => ({
  cookies: vi.fn(async () => mockCookieStore),
}));

// Mock next/server — NextResponse.json, NextResponse.redirect, NextResponse.next
const mockJson = vi.fn(
  (body: unknown, init?: { status?: number }) =>
    new Response(JSON.stringify(body), {
      status: init?.status ?? 200,
      headers: { "content-type": "application/json" },
    })
);

const mockRedirect = vi.fn((url: string | URL) => {
  const target = typeof url === "string" ? url : url.toString();
  return new Response(null, {
    status: 307,
    headers: { location: target },
  });
});

const mockNext = vi.fn(
  () => new Response(null, { status: 200 })
);

vi.mock("next/server", () => {
  // Minimal NextRequest shim for middleware tests
  class FakeNextRequest {
    url: string;
    nextUrl: URL;
    cookies: {
      get: (name: string) => { name: string; value: string } | undefined;
    };
    headers: Headers;

    constructor(url: string, opts?: { cookies?: Record<string, string> }) {
      this.url = url;
      this.nextUrl = new URL(url);
      const cookieMap = opts?.cookies ?? {};
      this.cookies = {
        get: (name: string) =>
          cookieMap[name] ? { name, value: cookieMap[name] } : undefined,
      };
      this.headers = new Headers();
    }
  }

  return {
    NextRequest: FakeNextRequest,
    NextResponse: {
      json: mockJson,
      redirect: mockRedirect,
      next: mockNext,
    },
  };
});

// ---------------------------------------------------------------------------
// Mock jose — SignJWT (callback) and jwtVerify (me)
// ---------------------------------------------------------------------------
const mockSign = vi.fn(async () => "mock-session-jwt-token");

vi.mock("jose", () => {
  class FakeSignJWT {
    constructor(_payload: unknown) {}
    setProtectedHeader() {
      return this;
    }
    setIssuedAt() {
      return this;
    }
    setExpirationTime() {
      return this;
    }
    sign = mockSign;
  }

  return {
    SignJWT: FakeSignJWT,
    jwtVerify: vi.fn(),
  };
});

// ---------------------------------------------------------------------------
// Helper: build a Request with optional search params and headers
// ---------------------------------------------------------------------------
function buildRequest(
  url: string,
  opts?: { headers?: Record<string, string> }
): Request {
  return new Request(url, {
    headers: new Headers(opts?.headers),
  });
}

// ============================================================================
// /api/auth/sso
// ============================================================================
describe("/api/auth/sso — PKCE initiation", () => {
  beforeEach(() => {
    cookieJar.clear();
    vi.clearAllMocks();
  });

  it("returns 503 when CLIENT_ID is empty", async () => {
    // Temporarily clear CLIENT_ID by re-importing with env override
    const origKey = process.env.NEXT_PUBLIC_JANUA_PUBLISHABLE_KEY;
    const origIssuer = process.env.NEXT_PUBLIC_OIDC_CLIENT_ID;
    process.env.NEXT_PUBLIC_JANUA_PUBLISHABLE_KEY = "";
    process.env.NEXT_PUBLIC_OIDC_CLIENT_ID = "";

    // Force module re-evaluation
    vi.resetModules();

    // Re-mock next/headers and next/server after resetModules
    vi.doMock("next/headers", () => ({
      cookies: vi.fn(async () => mockCookieStore),
    }));
    vi.doMock("next/server", () => ({
      NextResponse: {
        json: mockJson,
        redirect: mockRedirect,
        next: mockNext,
      },
    }));
    vi.doMock("jose", () => ({
      SignJWT: class {
        constructor() {}
        setProtectedHeader() {
          return this;
        }
        setIssuedAt() {
          return this;
        }
        setExpirationTime() {
          return this;
        }
        sign = mockSign;
      },
      jwtVerify: vi.fn(),
    }));

    const { GET } = await import("@/app/api/auth/sso/route");
    const req = buildRequest("https://mes-admin.madfam.io/api/auth/sso");
    await GET(req);

    expect(mockJson).toHaveBeenCalledWith(
      { error: "Janua no configurado" },
      { status: 503 }
    );

    // Restore
    process.env.NEXT_PUBLIC_JANUA_PUBLISHABLE_KEY = origKey;
    process.env.NEXT_PUBLIC_OIDC_CLIENT_ID = origIssuer;
  });

  it("sets state and PKCE verifier cookies, then redirects to Janua /authorize", async () => {
    vi.resetModules();
    vi.doMock("next/headers", () => ({
      cookies: vi.fn(async () => mockCookieStore),
    }));
    vi.doMock("next/server", () => ({
      NextResponse: {
        json: mockJson,
        redirect: mockRedirect,
        next: mockNext,
      },
    }));
    vi.doMock("jose", () => ({
      SignJWT: class {
        constructor() {}
        setProtectedHeader() {
          return this;
        }
        setIssuedAt() {
          return this;
        }
        setExpirationTime() {
          return this;
        }
        sign = mockSign;
      },
      jwtVerify: vi.fn(),
    }));

    const { GET } = await import("@/app/api/auth/sso/route");
    const req = buildRequest("https://mes-admin.madfam.io/api/auth/sso");
    await GET(req);

    // Should set two cookies: state and PKCE verifier
    const stateCall = mockCookieStore.set.mock.calls.find(
      (c: unknown[]) => c[0] === "janua-oauth-state"
    );
    const verifierCall = mockCookieStore.set.mock.calls.find(
      (c: unknown[]) => c[0] === "janua-pkce-verifier"
    );

    expect(stateCall).toBeDefined();
    expect(verifierCall).toBeDefined();

    // State is a UUID (36 chars with hyphens)
    expect(stateCall![1]).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
    );
    // PKCE verifier is a base64url-encoded 32-byte value (43 chars)
    expect(verifierCall![1]).toMatch(/^[A-Za-z0-9_-]{43}$/);

    // Cookie options should include httpOnly, 300s maxAge
    const cookieOpts = stateCall![2] as {
      httpOnly: boolean;
      maxAge: number;
    };
    expect(cookieOpts.httpOnly).toBe(true);
    expect(cookieOpts.maxAge).toBe(300);

    // Should redirect to Janua authorize endpoint
    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const redirectUrl = mockRedirect.mock.calls[0][0] as string;
    expect(redirectUrl).toContain(
      "https://auth.test.io/api/v1/oauth/authorize?"
    );

    // Verify query params
    const params = new URL(redirectUrl).searchParams;
    expect(params.get("client_id")).toBe("test-client-id");
    expect(params.get("response_type")).toBe("code");
    expect(params.get("scope")).toBe("openid profile email");
    expect(params.get("code_challenge_method")).toBe("S256");
    expect(params.get("state")).toBe(stateCall![1]);
    expect(params.get("redirect_uri")).toBe(
      "https://mes-admin.madfam.io/api/auth/callback"
    );
    // code_challenge should be a base64url string (43 chars for SHA-256)
    expect(params.get("code_challenge")).toMatch(/^[A-Za-z0-9_-]{43}$/);
  });

  it("uses x-forwarded-host/proto to derive origin behind a reverse proxy", async () => {
    vi.resetModules();
    vi.doMock("next/headers", () => ({
      cookies: vi.fn(async () => mockCookieStore),
    }));
    vi.doMock("next/server", () => ({
      NextResponse: {
        json: mockJson,
        redirect: mockRedirect,
        next: mockNext,
      },
    }));
    vi.doMock("jose", () => ({
      SignJWT: class {
        constructor() {}
        setProtectedHeader() {
          return this;
        }
        setIssuedAt() {
          return this;
        }
        setExpirationTime() {
          return this;
        }
        sign = mockSign;
      },
      jwtVerify: vi.fn(),
    }));

    const { GET } = await import("@/app/api/auth/sso/route");

    const req = buildRequest("http://localhost:4503/api/auth/sso", {
      headers: {
        "x-forwarded-host": "mes-admin.madfam.io",
        "x-forwarded-proto": "https",
      },
    });
    await GET(req);

    const redirectUrl = mockRedirect.mock.calls[0][0] as string;
    const params = new URL(redirectUrl).searchParams;
    expect(params.get("redirect_uri")).toBe(
      "https://mes-admin.madfam.io/api/auth/callback"
    );
  });
});

// ============================================================================
// /api/auth/callback
// ============================================================================
describe("/api/auth/callback — code exchange and session creation", () => {
  let fetchMock: Mock;

  beforeEach(() => {
    cookieJar.clear();
    vi.clearAllMocks();

    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
  });

  /** Helper to import the callback route with fresh module scope */
  async function importCallback() {
    vi.resetModules();
    vi.doMock("next/headers", () => ({
      cookies: vi.fn(async () => mockCookieStore),
    }));
    vi.doMock("next/server", () => ({
      NextResponse: {
        json: mockJson,
        redirect: mockRedirect,
        next: mockNext,
      },
    }));
    vi.doMock("jose", () => ({
      SignJWT: class {
        constructor() {}
        setProtectedHeader() {
          return this;
        }
        setIssuedAt() {
          return this;
        }
        setExpirationTime() {
          return this;
        }
        sign = mockSign;
      },
      jwtVerify: vi.fn(),
    }));
    return import("@/app/api/auth/callback/route");
  }

  it("redirects to /login with error when OAuth error param is present", async () => {
    const { GET } = await importCallback();
    const req = buildRequest(
      "https://mes-admin.madfam.io/api/auth/callback?error=access_denied&error_description=User%20denied"
    );
    await GET(req);

    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const url = mockRedirect.mock.calls[0][0] as string;
    expect(url).toContain("/login?sso_error=");
    expect(url).toContain("User%20denied");
  });

  it("redirects to /login when code or state is missing", async () => {
    const { GET } = await importCallback();
    const req = buildRequest(
      "https://mes-admin.madfam.io/api/auth/callback?code=abc"
    );
    await GET(req);

    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const url = mockRedirect.mock.calls[0][0] as string;
    expect(url).toContain("/login?sso_error=");
    expect(url).toContain("incompleta");
  });

  it("redirects to /login when state does not match stored cookie", async () => {
    cookieJar.set("janua-oauth-state", "stored-state-abc");
    cookieJar.set("janua-pkce-verifier", "verifier-xyz");

    const { GET } = await importCallback();
    const req = buildRequest(
      "https://mes-admin.madfam.io/api/auth/callback?code=authcode&state=wrong-state"
    );
    await GET(req);

    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const url = mockRedirect.mock.calls[0][0] as string;
    expect(url).toContain("/login?sso_error=");
    expect(url).toContain("invalido");
  });

  it("cleans up state and verifier cookies on callback", async () => {
    cookieJar.set("janua-oauth-state", "good-state");
    cookieJar.set("janua-pkce-verifier", "verifier-xyz");

    // Token exchange will fail, but cookies should still be cleaned
    fetchMock.mockResolvedValueOnce(
      new Response("bad", { status: 400 })
    );

    const { GET } = await importCallback();
    const req = buildRequest(
      "https://mes-admin.madfam.io/api/auth/callback?code=authcode&state=good-state"
    );
    await GET(req);

    expect(mockCookieStore.delete).toHaveBeenCalledWith("janua-oauth-state");
    expect(mockCookieStore.delete).toHaveBeenCalledWith("janua-pkce-verifier");
  });

  it("redirects to /login when token exchange fails (non-OK response)", async () => {
    cookieJar.set("janua-oauth-state", "good-state");
    cookieJar.set("janua-pkce-verifier", "verifier-xyz");

    fetchMock.mockResolvedValueOnce(
      new Response('{"error":"invalid_grant"}', { status: 400 })
    );

    const { GET } = await importCallback();
    const req = buildRequest(
      "https://mes-admin.madfam.io/api/auth/callback?code=authcode&state=good-state"
    );
    await GET(req);

    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const url = mockRedirect.mock.calls[0][0] as string;
    expect(url).toContain("intercambiar");
  });

  it("redirects to /login when token exchange throws a network error", async () => {
    cookieJar.set("janua-oauth-state", "good-state");
    cookieJar.set("janua-pkce-verifier", "verifier-xyz");

    fetchMock.mockRejectedValueOnce(new Error("ECONNREFUSED"));

    const { GET } = await importCallback();
    const req = buildRequest(
      "https://mes-admin.madfam.io/api/auth/callback?code=authcode&state=good-state"
    );
    await GET(req);

    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const url = mockRedirect.mock.calls[0][0] as string;
    expect(url).toContain("conexion");
  });

  it("exchanges code, fetches userinfo, signs JWT, sets session cookie, and redirects to /", async () => {
    cookieJar.set("janua-oauth-state", "good-state");
    cookieJar.set("janua-pkce-verifier", "verifier-xyz");

    // Mock token exchange response
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          access_token: "at-abc123",
          refresh_token: "rt-def456",
          expires_in: 3600,
        }),
        { status: 200, headers: { "content-type": "application/json" } }
      )
    );

    // Mock userinfo response
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          sub: "user-001",
          email: "admin@madfam.io",
          given_name: "Test",
          family_name: "Admin",
          email_verified: true,
          picture: "https://example.com/avatar.jpg",
        }),
        { status: 200, headers: { "content-type": "application/json" } }
      )
    );

    const { GET } = await importCallback();
    const req = buildRequest(
      "https://mes-admin.madfam.io/api/auth/callback?code=authcode&state=good-state"
    );
    await GET(req);

    // Token exchange should POST to Janua with correct params
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const tokenCall = fetchMock.mock.calls[0];
    expect(tokenCall[0]).toContain("/api/v1/oauth/token");
    expect(tokenCall[1].method).toBe("POST");

    const tokenBody = new URLSearchParams(tokenCall[1].body as string);
    expect(tokenBody.get("grant_type")).toBe("authorization_code");
    expect(tokenBody.get("code")).toBe("authcode");
    expect(tokenBody.get("client_id")).toBe("test-client-id");
    expect(tokenBody.get("code_verifier")).toBe("verifier-xyz");

    // Userinfo fetch should use the access token
    const userinfoCall = fetchMock.mock.calls[1];
    expect(userinfoCall[0]).toContain("/api/v1/oauth/userinfo");
    expect(userinfoCall[1].headers.Authorization).toBe("Bearer at-abc123");

    // Should sign a JWT
    expect(mockSign).toHaveBeenCalledTimes(1);

    // Should set janua-session cookie with the signed token
    const sessionSetCall = mockCookieStore.set.mock.calls.find(
      (c: unknown[]) => c[0] === "janua-session"
    );
    expect(sessionSetCall).toBeDefined();
    expect(sessionSetCall![1]).toBe("mock-session-jwt-token");
    const sessionOpts = sessionSetCall![2] as {
      httpOnly: boolean;
      maxAge: number;
    };
    expect(sessionOpts.httpOnly).toBe(true);
    expect(sessionOpts.maxAge).toBe(60 * 60 * 24 * 7); // 7 days

    // Should set janua-sso-tokens bridge cookie (not httpOnly)
    const bridgeSetCall = mockCookieStore.set.mock.calls.find(
      (c: unknown[]) => c[0] === "janua-sso-tokens"
    );
    expect(bridgeSetCall).toBeDefined();
    const bridgeOpts = bridgeSetCall![2] as {
      httpOnly: boolean;
      maxAge: number;
    };
    expect(bridgeOpts.httpOnly).toBe(false);
    expect(bridgeOpts.maxAge).toBe(60);

    const bridgeData = JSON.parse(bridgeSetCall![1] as string);
    expect(bridgeData.access_token).toBe("at-abc123");
    expect(bridgeData.refresh_token).toBe("rt-def456");

    // Should redirect to dashboard root
    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const redirectUrl = mockRedirect.mock.calls[0][0] as string;
    expect(redirectUrl).toBe("https://mes-admin.madfam.io/");
  });

  it("creates a fallback user when userinfo fetch fails", async () => {
    cookieJar.set("janua-oauth-state", "good-state");
    cookieJar.set("janua-pkce-verifier", "verifier-xyz");

    // Token exchange succeeds
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({ access_token: "at-abc123", expires_in: 3600 }),
        { status: 200, headers: { "content-type": "application/json" } }
      )
    );

    // Userinfo fails
    fetchMock.mockRejectedValueOnce(new Error("userinfo unavailable"));

    const { GET } = await importCallback();
    const req = buildRequest(
      "https://mes-admin.madfam.io/api/auth/callback?code=authcode&state=good-state"
    );
    await GET(req);

    // Should still create session and redirect (userinfo is optional)
    expect(mockSign).toHaveBeenCalledTimes(1);
    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const redirectUrl = mockRedirect.mock.calls[0][0] as string;
    expect(redirectUrl).toBe("https://mes-admin.madfam.io/");
  });
});

// ============================================================================
// /api/auth/me
// ============================================================================
describe("/api/auth/me — session validation", () => {
  beforeEach(() => {
    cookieJar.clear();
    vi.clearAllMocks();
  });

  async function importMe() {
    vi.resetModules();
    vi.doMock("next/headers", () => ({
      cookies: vi.fn(async () => mockCookieStore),
    }));
    vi.doMock("next/server", () => ({
      NextResponse: {
        json: mockJson,
        redirect: mockRedirect,
        next: mockNext,
      },
    }));

    const joseModule = {
      SignJWT: class {
        constructor() {}
        setProtectedHeader() {
          return this;
        }
        setIssuedAt() {
          return this;
        }
        setExpirationTime() {
          return this;
        }
        sign = mockSign;
      },
      jwtVerify: vi.fn(),
    };
    vi.doMock("jose", () => joseModule);
    const mod = await import("@/app/api/auth/me/route");
    return { mod, joseModule };
  }

  it("returns 401 when no janua-session cookie is present", async () => {
    const { mod } = await importMe();
    await mod.GET();

    expect(mockJson).toHaveBeenCalledWith(
      { authenticated: false },
      { status: 401 }
    );
  });

  it("returns 500 when JANUA_SECRET_KEY is not configured", async () => {
    const origSecret = process.env.JANUA_SECRET_KEY;
    process.env.JANUA_SECRET_KEY = "";

    vi.resetModules();
    vi.doMock("next/headers", () => ({
      cookies: vi.fn(async () => mockCookieStore),
    }));
    vi.doMock("next/server", () => ({
      NextResponse: {
        json: mockJson,
        redirect: mockRedirect,
        next: mockNext,
      },
    }));
    vi.doMock("jose", () => ({
      SignJWT: class {},
      jwtVerify: vi.fn(),
    }));

    cookieJar.set("janua-session", "some-jwt-token");

    const mod = await import("@/app/api/auth/me/route");
    await mod.GET();

    expect(mockJson).toHaveBeenCalledWith(
      { authenticated: false, error: "Servidor no configurado" },
      { status: 500 }
    );

    process.env.JANUA_SECRET_KEY = origSecret;
  });

  it("returns 401 when JWT verification fails (invalid signature)", async () => {
    cookieJar.set("janua-session", "invalid-jwt-token");

    const { mod, joseModule } = await importMe();
    (joseModule.jwtVerify as Mock).mockRejectedValueOnce(
      new Error("JWS signature verification failed")
    );

    await mod.GET();

    expect(mockJson).toHaveBeenCalledWith(
      { authenticated: false },
      { status: 401 }
    );
  });

  it("returns 401 when session has expired", async () => {
    cookieJar.set("janua-session", "valid-but-expired-jwt");

    const { mod, joseModule } = await importMe();

    // Session expired 1 hour ago
    const expiredAt = new Date(Date.now() - 3600_000).toISOString();
    (joseModule.jwtVerify as Mock).mockResolvedValueOnce({
      payload: {
        data: {
          user: { id: "user-001", email: "admin@madfam.io" },
          session: { id: "sess-001", expires_at: expiredAt },
          accessToken: "at-abc123",
        },
      },
    });

    await mod.GET();

    expect(mockJson).toHaveBeenCalledWith(
      { authenticated: false, reason: "expired" },
      { status: 401 }
    );
  });

  it("returns 401 when payload is missing user or accessToken", async () => {
    cookieJar.set("janua-session", "jwt-with-bad-payload");

    const { mod, joseModule } = await importMe();
    (joseModule.jwtVerify as Mock).mockResolvedValueOnce({
      payload: {
        data: {
          user: null,
          session: { id: "sess-001", expires_at: "2099-01-01T00:00:00Z" },
          accessToken: null,
        },
      },
    });

    await mod.GET();

    expect(mockJson).toHaveBeenCalledWith(
      { authenticated: false },
      { status: 401 }
    );
  });

  it("returns authenticated user data for a valid, non-expired session", async () => {
    cookieJar.set("janua-session", "valid-session-jwt");

    const { mod, joseModule } = await importMe();

    const futureExpiry = new Date(Date.now() + 3600_000).toISOString();
    (joseModule.jwtVerify as Mock).mockResolvedValueOnce({
      payload: {
        data: {
          user: {
            id: "user-001",
            email: "admin@madfam.io",
            first_name: "Test",
            last_name: "Admin",
            email_verified: true,
            profile_image_url: "https://example.com/avatar.jpg",
          },
          session: { id: "sess-001", expires_at: futureExpiry },
          accessToken: "at-abc123",
          refreshToken: "rt-def456",
        },
      },
    });

    await mod.GET();

    expect(mockJson).toHaveBeenCalledWith(
      expect.objectContaining({
        authenticated: true,
        user: expect.objectContaining({
          id: "user-001",
          email: "admin@madfam.io",
        }),
        access_token: "at-abc123",
        refresh_token: "rt-def456",
      })
    );

    // Verify no error status was passed
    const callArgs = mockJson.mock.calls[0];
    expect(callArgs[1]).toBeUndefined();
  });
});

// ============================================================================
// /api/auth/logout
// ============================================================================
describe("/api/auth/logout — session clearing", () => {
  beforeEach(() => {
    cookieJar.clear();
    vi.clearAllMocks();
  });

  async function importLogout() {
    vi.resetModules();
    vi.doMock("next/headers", () => ({
      cookies: vi.fn(async () => mockCookieStore),
    }));
    vi.doMock("next/server", () => ({
      NextResponse: {
        json: mockJson,
        redirect: mockRedirect,
        next: mockNext,
      },
    }));
    return import("@/app/api/auth/logout/route");
  }

  it("clears the janua-session cookie with maxAge: 0", async () => {
    cookieJar.set("janua-session", "existing-session-jwt");

    const { POST } = await importLogout();
    await POST();

    const sessionSetCall = mockCookieStore.set.mock.calls.find(
      (c: unknown[]) => c[0] === "janua-session"
    );
    expect(sessionSetCall).toBeDefined();
    expect(sessionSetCall![1]).toBe(""); // empty value
    const opts = sessionSetCall![2] as { maxAge: number; httpOnly: boolean };
    expect(opts.maxAge).toBe(0);
    expect(opts.httpOnly).toBe(true);
  });

  it("returns { success: true } JSON response", async () => {
    const { POST } = await importLogout();
    await POST();

    expect(mockJson).toHaveBeenCalledWith({ success: true });
  });
});

// ============================================================================
// Middleware
// ============================================================================
describe("middleware — route protection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  async function importMiddleware() {
    vi.resetModules();
    vi.doMock("next/headers", () => ({
      cookies: vi.fn(async () => mockCookieStore),
    }));
    vi.doMock("next/server", () => {
      class FakeNextRequest {
        url: string;
        nextUrl: URL;
        cookies: {
          get: (
            name: string
          ) => { name: string; value: string } | undefined;
        };
        headers: Headers;

        constructor(
          url: string,
          opts?: { cookies?: Record<string, string> }
        ) {
          this.url = url;
          this.nextUrl = new URL(url);
          const cookieMap = opts?.cookies ?? {};
          this.cookies = {
            get: (name: string) =>
              cookieMap[name]
                ? { name, value: cookieMap[name] }
                : undefined,
          };
          this.headers = new Headers();
        }
      }

      return {
        NextRequest: FakeNextRequest,
        NextResponse: {
          json: mockJson,
          redirect: mockRedirect,
          next: mockNext,
        },
      };
    });
    return import("@/middleware");
  }

  // Public paths that should pass through without authentication
  const publicPaths = [
    "/login",
    "/login/reset",
    "/api/auth/sso",
    "/api/auth/callback",
    "/api/auth/me",
    "/api/auth/logout",
    "/api/health",
    "/_next/static/chunk.js",
    "/favicon.ico",
    "/icon/logo.png",
  ];

  it.each(publicPaths)(
    "allows %s without session cookie",
    async (path) => {
      const { middleware } = await importMiddleware();
      // Use the NextRequest from the mocked module
      const { NextRequest } = await import("next/server");
      const req = new (NextRequest as any)(
        `https://mes-admin.madfam.io${path}`
      );
      middleware(req);

      expect(mockNext).toHaveBeenCalledTimes(1);
      expect(mockRedirect).not.toHaveBeenCalled();
      expect(mockJson).not.toHaveBeenCalled();
    }
  );

  it("redirects unauthenticated page requests to /login with redirect param", async () => {
    const { middleware } = await importMiddleware();
    const { NextRequest } = await import("next/server");
    const req = new (NextRequest as any)(
      "https://mes-admin.madfam.io/dashboard/settings"
    );
    middleware(req);

    expect(mockRedirect).toHaveBeenCalledTimes(1);
    const redirectUrl = mockRedirect.mock.calls[0][0] as URL;
    const urlStr =
      typeof redirectUrl === "string"
        ? redirectUrl
        : redirectUrl.toString();
    expect(urlStr).toContain("/login");
    expect(urlStr).toContain("redirect=%2Fdashboard%2Fsettings");
  });

  it("returns 401 JSON for unauthenticated API requests on protected paths", async () => {
    const { middleware } = await importMiddleware();
    const { NextRequest } = await import("next/server");
    const req = new (NextRequest as any)(
      "https://mes-admin.madfam.io/api/settings/profile"
    );
    middleware(req);

    expect(mockJson).toHaveBeenCalledWith(
      { error: "No autenticado" },
      { status: 401 }
    );
    expect(mockRedirect).not.toHaveBeenCalled();
  });

  it("allows authenticated requests through to protected paths", async () => {
    const { middleware } = await importMiddleware();
    const { NextRequest } = await import("next/server");
    const req = new (NextRequest as any)(
      "https://mes-admin.madfam.io/dashboard",
      { cookies: { "janua-session": "valid-session-jwt" } }
    );
    middleware(req);

    expect(mockNext).toHaveBeenCalledTimes(1);
    expect(mockRedirect).not.toHaveBeenCalled();
    expect(mockJson).not.toHaveBeenCalled();
  });

  it("allows authenticated API requests through to protected endpoints", async () => {
    const { middleware } = await importMiddleware();
    const { NextRequest } = await import("next/server");
    const req = new (NextRequest as any)(
      "https://mes-admin.madfam.io/api/settings/profile",
      { cookies: { "janua-session": "valid-session-jwt" } }
    );
    middleware(req);

    expect(mockNext).toHaveBeenCalledTimes(1);
    expect(mockJson).not.toHaveBeenCalled();
  });

  it("treats root path (/) as protected", async () => {
    const { middleware } = await importMiddleware();
    const { NextRequest } = await import("next/server");
    const req = new (NextRequest as any)(
      "https://mes-admin.madfam.io/"
    );
    middleware(req);

    expect(mockRedirect).toHaveBeenCalledTimes(1);
  });

  it("exports a matcher config for static asset exclusion", async () => {
    const mod = await importMiddleware();
    expect(mod.config).toBeDefined();
    expect(mod.config.matcher).toBeDefined();
    expect(mod.config.matcher.length).toBeGreaterThan(0);
  });
});
