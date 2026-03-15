import type { Metadata } from "next";
import { Suspense } from "react";
import { GeistSans } from "geist/font/sans";
import { GeistMono } from "geist/font/mono";
import "./globals.css";
import { Providers } from "./providers";
import { PostHogProvider } from "@/components/PostHogProvider";

export const metadata: Metadata = {
  title: "PravaraMES - Manufacturing Execution System",
  description:
    "Cloud-native Manufacturing Execution System for scheduling, tracking, and optimizing production operations",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body
        className={`${GeistSans.variable} ${GeistMono.variable} antialiased`}
      >
        <Suspense>
          <PostHogProvider>
            <Providers>{children}</Providers>
          </PostHogProvider>
        </Suspense>
      </body>
    </html>
  );
}
