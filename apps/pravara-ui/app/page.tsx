import { getSession } from "@janua/nextjs";
import { redirect } from "next/navigation";

export default async function Home() {
  const session = await getSession(
    process.env.JANUA_APP_ID!,
    process.env.JANUA_API_KEY!,
    process.env.JANUA_JWT_SECRET!,
  );

  if (!session) {
    redirect("/login");
  }

  redirect("/dashboard");
}
