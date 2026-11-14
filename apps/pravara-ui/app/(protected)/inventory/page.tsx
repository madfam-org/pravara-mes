"use client";

import { useState, useMemo } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery } from "@tanstack/react-query";
import { Search, Package, AlertTriangle, ArrowDownCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { QueryErrorState } from "@/components/query-error-state";
import { StockAdjustForm } from "@/components/inventory/stock-adjust-form";
import { InventorySectionNav } from "@/components/inventory/records/section-nav";
import { inventoryAPI, type InventoryItem } from "@/lib/api";
import { formatRecordedCost } from "@/lib/inventory";

function RecordedCost({ item }: { item: InventoryItem }) {
  if (item.unit_cost == null) return <span aria-label="No unit cost">—</span>;
  const { text, currencyKnown } = formatRecordedCost(
    item.unit_cost,
    item.currency
  );
  return (
    <span>
      {text}
      {!currencyKnown && (
        <span className="block whitespace-nowrap text-xs text-muted-foreground">
          currency not set
        </span>
      )}
    </span>
  );
}

function getStockColor(item: InventoryItem): string {
  if (item.quantity_available <= item.reorder_point) {
    return "text-red-600 dark:text-red-400";
  }
  if (item.quantity_available <= item.reorder_point * 2) {
    return "text-yellow-600 dark:text-yellow-400";
  }
  return "text-green-600 dark:text-green-400";
}

function getStockBadge(item: InventoryItem) {
  if (item.quantity_available <= item.reorder_point) {
    return (
      <Badge variant="error" className="whitespace-nowrap">
        Low Stock
      </Badge>
    );
  }
  if (item.quantity_available <= item.reorder_point * 2) {
    return (
      <Badge variant="warning" className="whitespace-nowrap">
        Warning
      </Badge>
    );
  }
  return null;
}

export default function InventoryPage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;

  const [searchQuery, setSearchQuery] = useState("");
  const [adjustingId, setAdjustingId] = useState<string | null>(null);
  const [receivingId, setReceivingId] = useState<string | null>(null);

  const inventoryQuery = useQuery({
    queryKey: ["inventory"],
    queryFn: () => inventoryAPI.list(token),
    enabled: !!token,
  });

  const lowStockQuery = useQuery({
    queryKey: ["inventory-low-stock"],
    queryFn: () => inventoryAPI.getLowStock(token),
    enabled: !!token,
  });

  const { data, isLoading } = inventoryQuery;
  const { data: lowStockData, isLoading: isLoadingLowStock } = lowStockQuery;

  const items = useMemo(() => data?.data ?? [], [data]);
  const lowStockItems = lowStockData ?? [];

  const filteredItems = useMemo(() => {
    if (!searchQuery) return items;
    const query = searchQuery.toLowerCase();
    return items.filter(
      (item) =>
        item.sku.toLowerCase().includes(query) ||
        item.name.toLowerCase().includes(query) ||
        item.category?.toLowerCase().includes(query)
    );
  }, [items, searchQuery]);

  const renderItemRow = (item: InventoryItem) => {
    const isAdjusting = adjustingId === item.id;
    const stockColor = getStockColor(item);
    const stockBadge = getStockBadge(item);

    return (
      <tr
        key={item.id}
        className="border-b transition-colors hover:bg-muted/50"
      >
        <td className="p-3 text-sm font-mono whitespace-nowrap">{item.sku}</td>
        <td className="p-3 text-sm font-medium">{item.name}</td>
        <td className="p-3 text-sm text-muted-foreground">
          {item.category || "—"}
        </td>
        <td className="p-3 text-sm text-right">{item.quantity_on_hand}</td>
        <td className="p-3 text-sm text-right text-muted-foreground">
          {item.quantity_reserved}
        </td>
        <td className="p-3 text-sm text-right">
          <div className="flex items-center justify-end gap-2">
            <span className={`font-semibold ${stockColor}`}>
              {item.quantity_available}
            </span>
            {stockBadge}
          </div>
        </td>
        <td className="p-3 text-sm text-muted-foreground">{item.unit}</td>
        <td className="p-3 text-sm text-right text-muted-foreground">
          <RecordedCost item={item} />
        </td>
        <td className="p-3 text-sm text-right text-muted-foreground">
          {item.reorder_point}
        </td>
        <td className="p-3 align-top">
          {isAdjusting ? (
            <StockAdjustForm
              item={item}
              token={token}
              onDone={() => setAdjustingId(null)}
            />
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() => setAdjustingId(item.id)}
              aria-label={`Adjust stock for ${item.name}`}
            >
              Adjust
            </Button>
          )}
        </td>
      </tr>
    );
  };

  const renderTable = (itemList: InventoryItem[]) => (
    <div className="relative rounded-md border overflow-x-auto">
      <table className="w-full text-left">
        <thead>
          <tr className="border-b bg-muted/50">
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground">
              SKU
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground">
              Name
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground">
              Category
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground text-right">
              On Hand
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground text-right">
              Reserved
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground text-right">
              Available
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground">
              Unit
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground text-right">
              Unit Cost
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground text-right">
              Reorder Point
            </th>
            <th scope="col" className="p-3 text-xs font-medium text-muted-foreground">
              Actions
            </th>
          </tr>
        </thead>
        <tbody>{itemList.map(renderItemRow)}</tbody>
      </table>
    </div>
  );

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold">Inventory</h1>
        <p className="text-muted-foreground">
          Track materials, parts, and stock levels
        </p>
      </div>
      <InventorySectionNav />

      {/* Search */}
      <div className="relative w-full sm:w-80">
        <Search
          className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
          aria-hidden="true"
        />
        <Input
          type="search"
          aria-label="Search inventory"
          placeholder="Search by SKU, name or category..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="pl-9"
        />
      </div>

      {/* Tabs */}
      <Tabs defaultValue="all" className="w-full">
        <TabsList>
          <TabsTrigger value="all">
            All Items
            {items.length > 0 && (
              <Badge variant="secondary" className="ml-2">
                {items.length}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger value="low-stock">
            <AlertTriangle className="mr-1.5 h-3.5 w-3.5" aria-hidden="true" />
            Low Stock
            {!lowStockQuery.isError && lowStockItems.length > 0 && (
              <Badge variant="error" className="ml-2">
                {lowStockItems.length}
              </Badge>
            )}
          </TabsTrigger>
        </TabsList>

        {/* All Items Tab */}
        <TabsContent value="all">
          {isLoading ? (
            <Card className="animate-pulse">
              <CardContent className="py-8">
                <div className="space-y-3">
                  {[...Array(5)].map((_, i) => (
                    <div key={i} className="h-10 w-full rounded bg-muted" />
                  ))}
                </div>
              </CardContent>
            </Card>
          ) : inventoryQuery.isError ? (
            <QueryErrorState
              what="inventory"
              error={inventoryQuery.error}
              onRetry={() => inventoryQuery.refetch()}
              isRetrying={inventoryQuery.isFetching}
            />
          ) : filteredItems.length === 0 ? (
            <Card>
              <CardContent className="flex flex-col items-center justify-center py-12 text-center">
                <Package className="h-12 w-12 text-muted-foreground" aria-hidden="true" />
                <h3 className="mt-4 text-lg font-semibold">
                  {items.length === 0
                    ? "No inventory items yet"
                    : "No matching items"}
                </h3>
                <p className="text-muted-foreground">
                  {items.length === 0
                    ? "Items appear here once they are created through the Pravara API."
                    : "Try adjusting your search query"}
                </p>
                {searchQuery && (
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
                <p className="text-sm text-muted-foreground mb-4">
                  Showing {filteredItems.length} of {items.length} items
                </p>
              )}
              {renderTable(filteredItems)}
            </>
          )}
        </TabsContent>

        {/* Low Stock Tab */}
        <TabsContent value="low-stock">
          {isLoadingLowStock ? (
            <Card className="animate-pulse">
              <CardContent className="py-8">
                <div className="space-y-3">
                  {[...Array(3)].map((_, i) => (
                    <div key={i} className="h-10 w-full rounded bg-muted" />
                  ))}
                </div>
              </CardContent>
            </Card>
          ) : lowStockQuery.isError ? (
            <QueryErrorState
              what="low stock alerts"
              error={lowStockQuery.error}
              onRetry={() => lowStockQuery.refetch()}
              isRetrying={lowStockQuery.isFetching}
            />
          ) : lowStockItems.length === 0 ? (
            <Card>
              <CardContent className="flex flex-col items-center justify-center py-12 text-center">
                <Package className="h-12 w-12 text-muted-foreground" aria-hidden="true" />
                <h3 className="mt-4 text-lg font-semibold">
                  No low stock alerts
                </h3>
                <p className="text-muted-foreground">
                  All inventory items are above their reorder points
                </p>
              </CardContent>
            </Card>
          ) : (
            <div className="space-y-4">
              <div className="flex items-center gap-2 rounded-lg border border-orange-200 bg-orange-50 p-3 dark:border-orange-900/50 dark:bg-orange-900/20">
                <AlertTriangle
                  className="h-5 w-5 shrink-0 text-orange-600 dark:text-orange-400"
                  aria-hidden="true"
                />
                <p className="text-sm text-orange-700 dark:text-orange-300">
                  {lowStockItems.length}{" "}
                  {lowStockItems.length === 1 ? "item is" : "items are"}{" "}
                  at or below reorder point and may need restocking.
                </p>
              </div>

              <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
                {lowStockItems.map((item) => (
                  <Card
                    key={item.id}
                    className="border-orange-200 dark:border-orange-900/50"
                  >
                    <CardHeader className="pb-2">
                      <div className="flex items-start justify-between">
                        <div>
                          <CardTitle className="text-base">
                            {item.name}
                          </CardTitle>
                          <p className="text-xs font-mono text-muted-foreground">
                            {item.sku}
                          </p>
                        </div>
                        {item.quantity_available <= 0 ? (
                          <Badge variant="error">Out of Stock</Badge>
                        ) : (
                          <Badge variant="warning">Low</Badge>
                        )}
                      </div>
                    </CardHeader>
                    <CardContent>
                      <div className="space-y-1.5 text-sm">
                        <div className="flex justify-between">
                          <span className="text-muted-foreground">
                            Available
                          </span>
                          <span
                            className={`font-semibold ${getStockColor(item)}`}
                          >
                            {item.quantity_available} {item.unit}
                          </span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-muted-foreground">
                            Reorder Point
                          </span>
                          <span className="font-medium">
                            {item.reorder_point} {item.unit}
                          </span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-muted-foreground">
                            Reorder Qty
                          </span>
                          <span className="font-medium">
                            {item.reorder_quantity} {item.unit}
                          </span>
                        </div>
                      </div>
                      {receivingId === item.id ? (
                        <div className="mt-3 border-t pt-3">
                          <StockAdjustForm
                            item={item}
                            token={token}
                            layout="card"
                            defaultType="receipt"
                            onDone={() => setReceivingId(null)}
                          />
                        </div>
                      ) : (
                        <Button
                          variant="outline"
                          size="sm"
                          className="mt-3 w-full text-xs"
                          onClick={() => setReceivingId(item.id)}
                          aria-label={`Record receipt for ${item.name}`}
                        >
                          <ArrowDownCircle
                            className="mr-1.5 h-3.5 w-3.5"
                            aria-hidden="true"
                          />
                          Record Receipt
                        </Button>
                      )}
                    </CardContent>
                  </Card>
                ))}
              </div>
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
