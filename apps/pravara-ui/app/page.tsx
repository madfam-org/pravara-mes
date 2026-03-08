import { redirect } from "next/navigation";

export default function Home() {
  // The /dashboard route is protected by the (protected) layout,
  // which checks auth via usePravaraSession and redirects to /login.
  redirect("/dashboard");
}
