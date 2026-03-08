"use client";

import { use } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import {
  ArrowLeft,
  Lock,
  FileEdit,
  Package,
  Factory,
  Kanban,
  CheckCircle,
  Award,
  GitBranch,
  Layers,
  type LucideIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { genealogyAPI, type ProductGenealogy } from "@/lib/api";
import { formatDate } from "@/lib/utils";

const statusColors: Record<string, string> = {
  draft:
    "bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400",
  in_progress:
    "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
  completed:
    "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",
  sealed:
    "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400",
};

interface TimelineStep {
  label: string;
  icon: LucideIcon;
  value: string | null;
  status: "completed" | "current" | "pending";
  detail?: string;
}

export default function GenealogyDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();

  const { data: record, isLoading } = useQuery({
    queryKey: ["genealogy", id],
    queryFn: () => genealogyAPI.get(token, id),
    enabled: !!token,
  });

  const { data: tree } = useQuery({
    queryKey: ["genealogy", id, "tree"],
    queryFn: () => genealogyAPI.getTree(token, id),
    enabled: !!token,
  });

  const sealMutation = useMutation({
    mutationFn: () => genealogyAPI.seal(token, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["genealogy", id] });
    },
  });

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-lg bg-muted" />
      </div>
    );
  }

  if (!record) {
    return (
      <div className="space-y-6">
        <Link href="/genealogy">
          <Button variant="ghost" size="sm">
            <ArrowLeft className="mr-2 h-4 w-4" />
            Back to Genealogy
          </Button>
        </Link>
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <GitBranch className="h-12 w-12 text-muted-foreground" />
            <h3 className="mt-4 text-lg font-semibold">Record not found</h3>
          </CardContent>
        </Card>
      </div>
    );
  }

  const isSealed = record.status === "sealed";

  // Build timeline steps
  const timelineSteps: TimelineStep[] = [
    {
      label: "Product Definition",
      icon: Package,
      value: record.product_name || record.product_sku || null,
      status: record.product_definition_id ? "completed" : "pending",
      detail: record.product_sku
        ? `SKU: ${record.product_sku}`
        : undefined,
    },
    {
      label: "Order",
      icon: Layers,
      value: record.order_id || null,
      status: record.order_id ? "completed" : "pending",
      detail: record.order_item_id
        ? `Item: ${record.order_item_id}`
        : undefined,
    },
    {
      label: "Production Task",
      icon: Kanban,
      value: record.task_id || null,
      status: record.task_id ? "completed" : "pending",
    },
    {
      label: "Machine",
      icon: Factory,
      value: record.machine_id || null,
      status: record.machine_id ? "completed" : "pending",
    },
    {
      label: "Quality Check",
      icon: CheckCircle,
      value: record.quality_result || null,
      status: record.quality_result ? "completed" : "pending",
      detail: record.inspection_id
        ? `Inspection: ${record.inspection_id}`
        : undefined,
    },
    {
      label: "Certificate",
      icon: Award,
      value: record.certificate_id || null,
      status: record.certificate_id ? "completed" : "pending",
    },
    {
      label: "Birth Certificate",
      icon: Lock,
      value: record.birth_cert_url || null,
      status: isSealed ? "completed" : "pending",
      detail: record.birth_cert_hash
        ? `SHA-256: ${record.birth_cert_hash.substring(0, 16)}...`
        : undefined,
    },
  ];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Link href="/genealogy">
            <Button variant="ghost" size="sm">
              <ArrowLeft className="mr-2 h-4 w-4" />
              Back
            </Button>
          </Link>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-bold">
                {record.product_name || "Genealogy Record"}
              </h1>
              <span
                className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium ${
                  statusColors[record.status] || statusColors.draft
                }`}
              >
                {record.status === "sealed" && (
                  <Lock className="mr-1 h-3 w-3" />
                )}
                {record.status}
              </span>
            </div>
            <div className="flex items-center gap-4 text-sm text-muted-foreground">
              {record.serial_number && (
                <span className="font-mono">SN: {record.serial_number}</span>
              )}
              {record.lot_number && (
                <span className="font-mono">Lot: {record.lot_number}</span>
              )}
              <span>Created {formatDate(record.created_at)}</span>
            </div>
          </div>
        </div>

        {!isSealed && record.status === "completed" && (
          <Button
            onClick={() => sealMutation.mutate()}
            disabled={sealMutation.isPending}
          >
            <Lock className="mr-2 h-4 w-4" />
            {sealMutation.isPending ? "Sealing..." : "Seal Record"}
          </Button>
        )}
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Timeline */}
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <GitBranch className="h-5 w-5" />
              Production Timeline
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="relative space-y-0">
              {timelineSteps.map((step, index) => {
                const Icon = step.icon;
                const isLast = index === timelineSteps.length - 1;
                const isCompleted = step.status === "completed";

                return (
                  <div key={step.label} className="relative flex gap-4 pb-8">
                    {/* Vertical line */}
                    {!isLast && (
                      <div
                        className={`absolute left-5 top-10 h-full w-px ${
                          isCompleted
                            ? "bg-primary"
                            : "bg-border"
                        }`}
                      />
                    )}

                    {/* Icon circle */}
                    <div
                      className={`relative z-10 flex h-10 w-10 shrink-0 items-center justify-center rounded-full border-2 ${
                        isCompleted
                          ? "border-primary bg-primary text-primary-foreground"
                          : "border-border bg-background text-muted-foreground"
                      }`}
                    >
                      <Icon className="h-4 w-4" />
                    </div>

                    {/* Content */}
                    <div className="flex-1 pt-1">
                      <p
                        className={`font-medium ${
                          isCompleted
                            ? "text-foreground"
                            : "text-muted-foreground"
                        }`}
                      >
                        {step.label}
                      </p>
                      {step.value && (
                        <p className="text-sm text-muted-foreground font-mono mt-0.5">
                          {step.value}
                        </p>
                      )}
                      {step.detail && (
                        <p className="text-xs text-muted-foreground mt-0.5">
                          {step.detail}
                        </p>
                      )}
                      {!isCompleted && (
                        <p className="text-xs text-muted-foreground italic mt-0.5">
                          Pending
                        </p>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>

        {/* Details sidebar */}
        <div className="space-y-6">
          {/* Record Info */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Record Details</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">ID</span>
                <span className="font-mono text-xs">{record.id}</span>
              </div>
              {record.serial_number && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Serial Number</span>
                  <span className="font-mono">{record.serial_number}</span>
                </div>
              )}
              {record.lot_number && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Lot Number</span>
                  <span className="font-mono">{record.lot_number}</span>
                </div>
              )}
              {record.quantity != null && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Quantity</span>
                  <span>{record.quantity}</span>
                </div>
              )}
              {record.quality_result && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Quality Result</span>
                  <Badge
                    variant={
                      record.quality_result === "pass"
                        ? "success"
                        : record.quality_result === "fail"
                        ? "destructive"
                        : "secondary"
                    }
                  >
                    {record.quality_result}
                  </Badge>
                </div>
              )}
              <div className="flex justify-between">
                <span className="text-muted-foreground">Created</span>
                <span>{formatDate(record.created_at)}</span>
              </div>
              {record.sealed_at && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Sealed</span>
                  <span>{formatDate(record.sealed_at)}</span>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Birth Certificate */}
          {isSealed && !!record.birth_cert_url && (
            <Card className="border-purple-200 dark:border-purple-800">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base text-purple-600 dark:text-purple-400">
                  <Lock className="h-4 w-4" />
                  Digital Birth Certificate
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm">
                <p className="text-muted-foreground">
                  This record has been sealed with a cryptographic hash.
                  The birth certificate is immutable.
                </p>
                {!!record.birth_cert_hash && (
                  <div>
                    <p className="text-xs text-muted-foreground mb-1">
                      SHA-256 Hash
                    </p>
                    <p className="font-mono text-xs break-all bg-muted p-2 rounded">
                      {record.birth_cert_hash}
                    </p>
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {/* Material Consumption */}
          {tree?.materials && tree.materials.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Materials Consumed</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-2 text-sm">
                  {tree.materials.map((mat: any, i: number) => (
                    <div
                      key={i}
                      className="flex items-center justify-between border-b last:border-0 pb-2 last:pb-0"
                    >
                      <div>
                        <p className="font-medium">{mat.material_name}</p>
                        {mat.batch_lot_number && (
                          <p className="text-xs text-muted-foreground font-mono">
                            Lot: {mat.batch_lot_number}
                          </p>
                        )}
                      </div>
                      <span className="text-muted-foreground">
                        {mat.quantity_used} {mat.unit}
                      </span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </div>
  );
}
