import { Suspense } from "react";
import type { Metadata } from "next";
import { PostHogProvider } from "@/components/PostHogProvider";
import { AuthProvider } from "@/lib/auth";
import "./globals.css";

export const metadata: Metadata = {
  title: "PravaraMES Admin",
  description: "Consola de administracion de PravaraMES",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="es">
      <body>
        <Suspense fallback={null}>
          <PostHogProvider>
            <AuthProvider>{children}</AuthProvider>
          </PostHogProvider>
        </Suspense>
      </body>
    </html>
  );
}
