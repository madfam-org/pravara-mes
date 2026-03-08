"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { usePravaraSession } from "@/lib/auth";
import { Sidebar } from "@/components/sidebar";

export default function ProtectedLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { data: session, status } = usePravaraSession();
  const router = useRouter();

  useEffect(() => {
    if (status === "unauthenticated") {
      router.replace("/login");
    }
  }, [status, router]);

  if (status === "loading" || !session) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>
    );
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
