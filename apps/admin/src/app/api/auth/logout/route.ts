import { NextResponse } from "next/server";
import { cookies } from "next/headers";

/**
 * POST /api/auth/logout
 *
 * Clears the janua-session cookie, effectively ending the session.
 */
export async function POST() {
  const cookieStore = await cookies();

  cookieStore.set("janua-session", "", {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: 0,
  });

  return NextResponse.json({ success: true });
}
