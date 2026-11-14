"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { usePravaraSession } from "@/lib/auth";
import { materialsAPI } from "@/lib/inventory-records-api";
import { canManageInventory, retryRead } from "@/lib/inventory-records";

/** Query-key roots, so a write can refresh every screen that shows it. */
export const RECORD_KEYS = {
  materials: ["inventory-materials"] as const,
  unlinked: ["inventory-unlinked"] as const,
  gaps: ["inventory-conversion-gaps"] as const,
  receipts: ["inventory-receipts"] as const,
  counts: ["inventory-counts"] as const,
  consumption: ["job-consumption"] as const,
  coverage: ["job-consumption-coverage"] as const,
  stock: ["inventory"] as const,
};

export function useAccessToken(): string {
  const { data: session } = usePravaraSession();
  return ((session?.user as { accessToken?: string } | undefined)?.accessToken ?? "") as string;
}

/** Whether this person may record receipts, counts and registry changes. */
export function useCanManageInventory(): boolean {
  const { data: session } = usePravaraSession();
  return canManageInventory(session?.user);
}

/** Every material, for pickers (the API caps a page at 1000). */
export function useMaterialOptions(token: string, opts: { activeOnly?: boolean } = {}) {
  return useQuery({
    queryKey: [...RECORD_KEYS.materials, "options", opts.activeOnly ?? false],
    queryFn: () =>
      materialsAPI.list(token, { limit: 1000, active: opts.activeOnly ? true : undefined }),
    enabled: !!token,
    retry: retryRead,
  });
}

export function useDebouncedValue<T>(value: T, ms = 300): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

/** A fresh key per form instance; reused when the same form is resent. */
export function newIdempotencyKey(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `rcpt-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
