import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

/**
 * PravaraMES Admin Middleware
 *
 * Protects all routes by requiring a janua-session cookie.
 * Unauthenticated requests are redirected to /login.
 * API routes return 401 JSON instead of redirecting.
 */

const PUBLIC_PATHS = [
  "/login",
  "/api/auth",
  "/api/health",
  "/_next",
  "/favicon.ico",
  "/icon",
];

function isPublicPath(pathname: string): boolean {
  return PUBLIC_PATHS.some(
    (p) => pathname === p || pathname.startsWith(`${p}/`)
  );
}

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  // Allow public paths through without authentication
  if (isPublicPath(pathname)) {
    return NextResponse.next();
  }

  // Require janua-session cookie for all other routes
  const session = request.cookies.get("janua-session");
  if (!session?.value) {
    // API routes get a JSON 401
    if (pathname.startsWith("/api/")) {
      return NextResponse.json(
        { error: "No autenticado" },
        { status: 401 }
      );
    }

    // Page routes redirect to login with return URL
    const loginUrl = new URL("/login", request.url);
    loginUrl.searchParams.set("redirect", pathname);
    return NextResponse.redirect(loginUrl);
  }

  return NextResponse.next();
}

export const config = {
  matcher: [
    "/((?!_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico)$).*)",
  ],
};
