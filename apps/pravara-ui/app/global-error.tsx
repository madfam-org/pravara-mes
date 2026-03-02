"use client";

import { useEffect } from "react";
import { AlertTriangle, RotateCcw } from "lucide-react";

interface GlobalErrorProps {
  error: Error & { digest?: string };
  reset: () => void;
}

export default function GlobalError({ error, reset }: GlobalErrorProps) {
  useEffect(() => {
    // Log the error to an error reporting service
    console.error("Global application error:", error);
  }, [error]);

  return (
    <html lang="en">
      <body className="antialiased">
        <div className="flex min-h-screen flex-col items-center justify-center bg-white px-4 dark:bg-zinc-950">
          <div className="mx-auto max-w-md text-center">
            <div className="mb-8 flex justify-center">
              <div className="rounded-full bg-red-100 p-6 dark:bg-red-950">
                <AlertTriangle className="h-16 w-16 text-red-600 dark:text-red-400" />
              </div>
            </div>

            <h1 className="mb-2 text-4xl font-bold tracking-tight text-zinc-900 dark:text-zinc-50">
              Critical Error
            </h1>
            <p className="mb-4 text-lg text-zinc-600 dark:text-zinc-400">
              A critical error occurred that prevented the application from
              loading.
            </p>

            {error.digest && (
              <p className="mb-6 font-mono text-xs text-zinc-500 dark:text-zinc-500">
                Error ID: {error.digest}
              </p>
            )}

            <button
              onClick={reset}
              className="inline-flex items-center justify-center rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-zinc-50 hover:bg-zinc-800 dark:bg-zinc-50 dark:text-zinc-900 dark:hover:bg-zinc-200"
            >
              <RotateCcw className="mr-2 h-4 w-4" />
              Try Again
            </button>

            <p className="mt-8 text-sm text-zinc-500 dark:text-zinc-500">
              If this problem persists, please{" "}
              <a
                href="mailto:support@pravara.io"
                className="text-blue-600 underline-offset-4 hover:underline dark:text-blue-400"
              >
                contact support
              </a>
              .
            </p>
          </div>
        </div>
      </body>
    </html>
  );
}
