import { redirect } from "next/navigation";
import { auth } from "@/lib/auth";
import { Sidebar } from "@/components/sidebar";

export default async function ProtectedLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const session = await auth();

  if (!session) {
    redirect("/login");
  }

  return (
    <div className="flex h-screen">
      <Sidebar user={session.user} />
      <main
        id="main-content"
        className="flex-1 overflow-auto bg-muted/30"
        role="main"
        aria-label="Main content"
      >
        <a
          href="#main-content"
          className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-primary focus:text-primary-foreground focus:p-3 focus:m-2 focus:rounded-md"
        >
          Skip to main content
        </a>
        <div className="container mx-auto p-6">{children}</div>
      </main>
    </div>
  );
}
