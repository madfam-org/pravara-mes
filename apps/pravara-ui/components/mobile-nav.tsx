"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { Menu, X } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Sidebar } from "@/components/sidebar";

interface MobileNavProps {
  user?: {
    name?: string | null;
    email?: string | null;
    image?: string | null;
  };
}

/**
 * Top bar and navigation drawer for screens narrower than the `md`
 * breakpoint, where the fixed sidebar would take most of the width.
 */
export function MobileNav({ user }: MobileNavProps) {
  const [open, setOpen] = useState(false);

  return (
    <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-card px-3 md:hidden">
      <Dialog.Root open={open} onOpenChange={setOpen}>
        <Dialog.Trigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="h-11 w-11"
            aria-label="Open navigation"
          >
            <Menu className="h-5 w-5" aria-hidden="true" />
          </Button>
        </Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-40 bg-black/60 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
          <Dialog.Content
            aria-describedby={undefined}
            onOpenAutoFocus={(event) => {
              // Start on the current page's link, never on "Sign out".
              const target = event.currentTarget as HTMLElement;
              const link = target.querySelector<HTMLElement>(
                'a[aria-current="page"], nav a'
              );
              if (link) {
                event.preventDefault();
                link.focus();
              }
            }}
            className="fixed inset-y-0 left-0 z-50 flex w-64 max-w-[85vw] shadow-xl outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-left data-[state=open]:slide-in-from-left"
          >
            <Dialog.Title className="sr-only">Navigation</Dialog.Title>
            <Sidebar
              user={user}
              collapsible={false}
              onNavigate={() => setOpen(false)}
              className="w-full border-r-0"
            />
            <Dialog.Close asChild>
              <Button
                variant="ghost"
                size="icon"
                className="absolute right-2 top-2.5 h-11 w-11"
                aria-label="Close navigation"
              >
                <X className="h-5 w-5" aria-hidden="true" />
              </Button>
            </Dialog.Close>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
      <span className="text-base font-semibold">PravaraMES</span>
    </header>
  );
}
