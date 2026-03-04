"use client";

import { useState, useMemo } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import {
  GitBranch,
  Search,
  ExternalLink,
  Lock,
  FileEdit,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { SearchInput } from "@/components/search-input";
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

const statusLabels: Record<string, string> = {
  draft: "Draft",
  in_progress: "In Progress",
  completed: "Completed",
  sealed: "Sealed",
};

export default function GenealogyPage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;

  const [searchQuery, setSearchQuery] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: ["genealogy"],
    queryFn: () => genealogyAPI.list(token),
    enabled: !!token,
  });

  const records = data?.data || [];

  const filteredRecords = useMemo(() => {
    if (!searchQuery) return records;
    const query = searchQuery.toLowerCase();
    return records.filter(
      (r) =>
        r.serial_number?.toLowerCase().includes(query) ||
        r.lot_number?.toLowerCase().includes(query) ||
        r.product_sku?.toLowerCase().includes(query) ||
        r.product_name?.toLowerCase().includes(query)
    );
  }, [records, searchQuery]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Product Genealogy</h1>
          <p className="text-muted-foreground">
            Track product lineage, materials, and digital birth certificates
          </p>
        </div>
      </div>

      {/* Search */}
      <SearchInput
        placeholder="Search by serial, lot, SKU, or product name..."
        value={searchQuery}
        onChange={setSearchQuery}
        className="w-full sm:w-96"
      />

      {/* Loading state */}
      {isLoading ? (
        <div className="space-y-4">
          {[...Array(5)].map((_, i) => (
            <Card key={i} className="animate-pulse">
              <CardContent className="py-4">
                <div className="flex items-center gap-4">
                  <div className="h-10 w-10 rounded-full bg-muted" />
                  <div className="flex-1 space-y-2">
                    <div className="h-4 w-48 rounded bg-muted" />
                    <div className="h-3 w-32 rounded bg-muted" />
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : filteredRecords.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <GitBranch className="h-12 w-12 text-muted-foreground" />
            <h3 className="mt-4 text-lg font-semibold">
              {records.length === 0
                ? "No genealogy records yet"
                : "No matching records"}
            </h3>
            <p className="text-muted-foreground">
              {records.length === 0
                ? "Genealogy records are created automatically when tasks complete"
                : "Try adjusting your search"}
            </p>
            {records.length > 0 && searchQuery && (
              <Button
                variant="outline"
                className="mt-4"
                onClick={() => setSearchQuery("")}
              >
                Clear search
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <>
          {searchQuery && (
            <p className="text-sm text-muted-foreground">
              Showing {filteredRecords.length} of {records.length} records
            </p>
          )}

          {/* Records list */}
          <div className="space-y-3">
            {filteredRecords.map((record) => (
              <Link
                key={record.id}
                href={`/genealogy/${record.id}`}
                className="block"
              >
                <Card className="hover:shadow-md transition-shadow cursor-pointer">
                  <CardContent className="py-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-4">
                        <div className="flex h-10 w-10 items-center justify-center rounded-full bg-muted">
                          {record.status === "sealed" ? (
                            <Lock className="h-5 w-5 text-purple-500" />
                          ) : (
                            <FileEdit className="h-5 w-5 text-muted-foreground" />
                          )}
                        </div>
                        <div>
                          <div className="flex items-center gap-2">
                            <p className="font-medium">
                              {record.product_name || record.product_sku || "Unknown Product"}
                            </p>
                            <span
                              className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${
                                statusColors[record.status] || statusColors.draft
                              }`}
                            >
                              {statusLabels[record.status] || record.status}
                            </span>
                          </div>
                          <div className="flex items-center gap-3 text-sm text-muted-foreground">
                            {record.serial_number && (
                              <span className="font-mono">
                                SN: {record.serial_number}
                              </span>
                            )}
                            {record.lot_number && (
                              <span className="font-mono">
                                Lot: {record.lot_number}
                              </span>
                            )}
                            {record.product_sku && (
                              <span className="font-mono">
                                {record.product_sku}
                              </span>
                            )}
                          </div>
                        </div>
                      </div>
                      <div className="flex items-center gap-3">
                        <div className="text-right text-sm text-muted-foreground">
                          <p>{formatDate(record.created_at)}</p>
                          {record.birth_cert_url && (
                            <span className="flex items-center gap-1 text-purple-500">
                              <Lock className="h-3 w-3" />
                              Certified
                            </span>
                          )}
                        </div>
                        <ExternalLink className="h-4 w-4 text-muted-foreground" />
                      </div>
                    </div>
                  </CardContent>
                </Card>
              </Link>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
