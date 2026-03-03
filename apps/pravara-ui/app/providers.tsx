"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { JanuaProvider } from "@janua/nextjs";
import { ThemeProvider } from "next-themes";
import { useState, type ReactNode } from "react";
import { Toaster } from "@/components/ui/toaster";

const januaConfig = {
  baseURL: process.env.NEXT_PUBLIC_JANUA_URL || "https://auth.madfam.io",
};

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 60 * 1000,
            refetchOnWindowFocus: false,
          },
        },
      })
  );

  return (
    <JanuaProvider config={januaConfig}>
      <ThemeProvider
        attribute="class"
        defaultTheme="system"
        enableSystem
        disableTransitionOnChange
      >
        <QueryClientProvider client={queryClient}>
          {children}
          <Toaster />
        </QueryClientProvider>
      </ThemeProvider>
    </JanuaProvider>
  );
}
