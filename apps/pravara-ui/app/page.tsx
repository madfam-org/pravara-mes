import { redirect } from "next/navigation";

export default function Home() {
  // Public visitors hit the marketing landing first. Authenticated
  // operators get a "Iniciar sesión" / "Abrir dashboard" link in the
  // landing nav that bounces them to /dashboard, which is gated by
  // the (protected) layout's session check.
  //
  // Previously this redirected straight to /dashboard, which sent
  // every cold visitor through the auth flow → bad first impression
  // for marketing. The /landing → /dashboard hop reflects the
  // standard B2B SaaS pattern (mes.madfam.io is the marketing
  // surface; auth-gated app lives one click in).
  redirect("/landing");
}
