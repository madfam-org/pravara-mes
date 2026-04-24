"use client";

import { use } from "react";
import Link from "next/link";
import { usePravaraSession } from "@/lib/auth";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Edit, Settings } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { MachineControlPanel, MachineStatusCard } from "@/components/machines";
import { machinesAPI } from "@/lib/api";
import { useMachineUpdates } from "@/hooks/useMachineUpdates";

interface MachineDetailPageProps {
  params: Promise<{ id: string }>;
}

export default function MachineDetailPage({ params }: MachineDetailPageProps) {
  const { id } = use(params);
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;

  // Set up real-time updates for this machine
  useMachineUpdates({
    onStatusChange: (data) => {
      if (data.machine_id === id) {
        // Status already updated via React Query cache
      }
    },
    onHeartbeat: (data) => {
      if (data.machine_id === id) {
        // Heartbeat already updated via React Query cache
      }
    },
  });

  const {
    data: machine,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["machines", id],
    queryFn: () => machinesAPI.get(token, id),
    enabled: !!token && !!id,
    refetchInterval: 30000, // Refresh every 30 seconds as a fallback
  });

  if (isLoading) {
    return <MachineDetailSkeleton />;
  }

  if (error || !machine) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="sm" asChild>
            <Link href="/machines">
              <ArrowLeft className="mr-2 h-4 w-4" />
              Back to Machines
            </Link>
          </Button>
        </div>
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <h3 className="text-lg font-semibold text-destructive">
              Machine Not Found
            </h3>
            <p className="text-muted-foreground mt-2">
              The machine you&apos;re looking for doesn&apos;t exist or you don&apos;t have access.
            </p>
            <Button className="mt-4" asChild>
              <Link href="/machines">Return to Machines</Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="sm" asChild>
            <Link href="/machines">
              <ArrowLeft className="mr-2 h-4 w-4" />
              Back
            </Link>
          </Button>
          <div>
            <h1 className="text-2xl font-bold">{machine.name}</h1>
            <p className="text-sm font-mono text-muted-foreground">
              {machine.code}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm">
            <Settings className="mr-2 h-4 w-4" />
            Settings
          </Button>
          <Button variant="outline" size="sm">
            <Edit className="mr-2 h-4 w-4" />
            Edit
          </Button>
        </div>
      </div>

      {/* Main Content */}
      <div className="grid gap-6 md:grid-cols-2">
        {/* Status Card */}
        <MachineStatusCard machine={machine} />

        {/* Control Panel */}
        <MachineControlPanel machine={machine} />
      </div>

      {/* Description */}
      {machine.description && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Description</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">{machine.description}</p>
          </CardContent>
        </Card>
      )}

      {/* Specifications */}
      {machine.specifications && Object.keys(machine.specifications).length > 0 && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Specifications</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid gap-2 text-sm md:grid-cols-2">
              {Object.entries(machine.specifications).map(([key, value]) => (
                <div key={key} className="flex justify-between border-b pb-2">
                  <span className="text-muted-foreground capitalize">
                    {key.replace(/_/g, " ")}
                  </span>
                  <span className="font-medium">{String(value)}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Metadata */}
      {machine.metadata && Object.keys(machine.metadata).length > 0 && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Metadata</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="text-xs bg-muted p-4 rounded overflow-x-auto">
              {JSON.stringify(machine.metadata, null, 2)}
            </pre>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function MachineDetailSkeleton() {
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Skeleton className="h-9 w-20" />
        <div>
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-4 w-24 mt-1" />
        </div>
      </div>
      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader className="pb-3">
            <Skeleton className="h-5 w-16" />
          </CardHeader>
          <CardContent className="space-y-4">
            <Skeleton className="h-8 w-32" />
            <div className="space-y-2">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-3/4" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-3">
            <Skeleton className="h-5 w-32" />
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-4 gap-2">
              {[...Array(4)].map((_, i) => (
                <Skeleton key={i} className="h-16" />
              ))}
            </div>
            <Skeleton className="h-10 w-full" />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
