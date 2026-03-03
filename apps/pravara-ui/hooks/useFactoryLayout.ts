/**
 * Hook for fetching and managing factory floor layouts
 */
"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { usePravaraSession } from "@/lib/auth";
import { layoutsAPI, type FactoryLayout } from "@/lib/api";

export function useFactoryLayout() {
  const { data: session } = usePravaraSession();
  const token = session?.accessToken as string | undefined;
  const queryClient = useQueryClient();

  const activeLayoutQuery = useQuery({
    queryKey: ["layouts", "active"],
    queryFn: () => layoutsAPI.getActive(token!),
    enabled: !!token,
    staleTime: 30_000,
    retry: 1,
  });

  const updateLayoutMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string;
      data: Partial<FactoryLayout>;
    }) => layoutsAPI.update(token!, id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["layouts"] });
    },
  });

  return {
    layout: activeLayoutQuery.data ?? null,
    isLoading: activeLayoutQuery.isLoading,
    error: activeLayoutQuery.error,
    updateLayout: updateLayoutMutation.mutate,
    isUpdating: updateLayoutMutation.isPending,
  };
}
