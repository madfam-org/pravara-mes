"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { usePravaraSession } from "@/lib/auth";
import { Sidebar } from "@/components/sidebar";
import { MobileNav } from "@/components/mobile-nav";

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
    <div className="flex h-dvh flex-col md:flex-row">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:m-2 focus:rounded-md focus:bg-primary focus:p-3 focus:text-primary-foreground"
      >
        Skip to main content
      </a>
      <MobileNav user={session.user} />
      <Sidebar user={session.user} className="hidden md:flex" />
      <main
        id="main-content"
        tabIndex={-1}
        className="min-h-0 flex-1 overflow-auto bg-muted/30 outline-none"
        aria-label="Main content"
      >
        <div className="container mx-auto p-4 md:p-6">{children}</div>
      </main>
    </div>
  );
}
