"use client";

import { useState, useMemo } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Plus,
  Search,
  Package,
  AlertTriangle,
  ArrowDownCircle,
  ArrowUpCircle,
  RefreshCw,
  X,
} from "lucide-react";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { inventoryAPI, type InventoryItem } from "@/lib/api";

const transactionTypes = [
  { value: "receipt", label: "Receipt", icon: ArrowDownCircle },
  { value: "consumption", label: "Consumption", icon: ArrowUpCircle },
  { value: "adjustment", label: "Adjustment", icon: RefreshCw },
];

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
    return <Badge variant="error">Low Stock</Badge>;
  }
  if (item.quantity_available <= item.reorder_point * 2) {
    return <Badge variant="warning">Warning</Badge>;
  }
  return null;
}

export default function InventoryPage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();

  const [searchQuery, setSearchQuery] = useState("");
  const [adjustingId, setAdjustingId] = useState<string | null>(null);
  const [adjustQuantity, setAdjustQuantity] = useState("");
  const [adjustType, setAdjustType] = useState("receipt");
  const [adjustNotes, setAdjustNotes] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => inventoryAPI.list(token),
    enabled: !!token,
  });

  const { data: lowStockData, isLoading: isLoadingLowStock } = useQuery({
    queryKey: ["inventory-low-stock"],
    queryFn: () => inventoryAPI.getLowStock(token),
    enabled: !!token,
  });

  const adjustMutation = useMutation({
    mutationFn: ({
      id,
      quantity,
      transaction_type,
      notes,
    }: {
      id: string;
      quantity: number;
      transaction_type: string;
      notes?: string;
    }) =>
      inventoryAPI.adjust(token, id, {
        quantity,
        transaction_type,
        notes,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["inventory"] });
      queryClient.invalidateQueries({ queryKey: ["inventory-low-stock"] });
      resetAdjustForm();
    },
  });

  const items = data?.data || [];
  const lowStockItems = lowStockData || [];

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

  const resetAdjustForm = () => {
    setAdjustingId(null);
    setAdjustQuantity("");
    setAdjustType("receipt");
    setAdjustNotes("");
  };

  const handleAdjustSubmit = (id: string) => {
    const qty = parseFloat(adjustQuantity);
    if (isNaN(qty) || qty === 0) return;
    adjustMutation.mutate({
      id,
      quantity: qty,
      transaction_type: adjustType,
      notes: adjustNotes || undefined,
    });
  };

  const renderItemRow = (item: InventoryItem) => {
    const isAdjusting = adjustingId === item.id;
    const stockColor = getStockColor(item);
    const stockBadge = getStockBadge(item);

    return (
      <tr
        key={item.id}
        className="border-b transition-colors hover:bg-muted/50"
      >
        <td className="p-3 text-sm font-mono">{item.sku}</td>
        <td className="p-3 text-sm font-medium">{item.name}</td>
        <td className="p-3 text-sm text-muted-foreground">
          {item.category || "-"}
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
          {item.unit_cost != null
            ? new Intl.NumberFormat("en-US", {
                style: "currency",
                currency: item.currency || "USD",
              }).format(item.unit_cost)
            : "-"}
        </td>
        <td className="p-3 text-sm text-right text-muted-foreground">
          {item.reorder_point}
        </td>
        <td className="p-3">
          {isAdjusting ? (
            <div className="flex items-center gap-2">
              <Select value={adjustType} onValueChange={setAdjustType}>
                <SelectTrigger className="w-[120px] h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {transactionTypes.map((t) => (
                    <SelectItem key={t.value} value={t.value}>
                      {t.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Input
                type="number"
                placeholder="Qty"
                value={adjustQuantity}
                onChange={(e) => setAdjustQuantity(e.target.value)}
                className="w-20 h-8 text-xs"
              />
              <Input
                placeholder="Notes"
                value={adjustNotes}
                onChange={(e) => setAdjustNotes(e.target.value)}
                className="w-28 h-8 text-xs"
              />
              <Button
                size="sm"
                className="h-8 text-xs"
                onClick={() => handleAdjustSubmit(item.id)}
                disabled={
                  adjustMutation.isPending ||
                  !adjustQuantity ||
                  parseFloat(adjustQuantity) === 0
                }
              >
                Save
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                onClick={resetAdjustForm}
                aria-label="Cancel adjustment"
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() => setAdjustingId(item.id)}
            >
              Adjust
            </Button>
          )}
        </td>
      </tr>
    );
  };

  const renderTable = (itemList: InventoryItem[]) => (
    <div className="rounded-md border overflow-x-auto">
      <table className="w-full text-left">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="p-3 text-xs font-medium text-muted-foreground">
              SKU
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground">
              Name
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground">
              Category
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground text-right">
              On Hand
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground text-right">
              Reserved
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground text-right">
              Available
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground">
              Unit
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground text-right">
              Unit Cost
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground text-right">
              Reorder Pt
            </th>
            <th className="p-3 text-xs font-medium text-muted-foreground">
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
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Inventory</h1>
          <p className="text-muted-foreground">
            Track materials, parts, and stock levels
          </p>
        </div>
        <Button size="sm">
          <Plus className="mr-2 h-4 w-4" />
          Add Item
        </Button>
      </div>

      {/* Search */}
      <div className="relative w-full sm:w-80">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          placeholder="Search by SKU or name..."
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
            <AlertTriangle className="mr-1.5 h-3.5 w-3.5" />
            Low Stock
            {lowStockItems.length > 0 && (
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
          ) : filteredItems.length === 0 ? (
            <Card>
              <CardContent className="flex flex-col items-center justify-center py-12">
                <Package className="h-12 w-12 text-muted-foreground" />
                <h3 className="mt-4 text-lg font-semibold">
                  {items.length === 0
                    ? "No inventory items yet"
                    : "No matching items"}
                </h3>
                <p className="text-muted-foreground">
                  {items.length === 0
                    ? "Add your first inventory item to get started"
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
          ) : lowStockItems.length === 0 ? (
            <Card>
              <CardContent className="flex flex-col items-center justify-center py-12">
                <Package className="h-12 w-12 text-muted-foreground" />
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
                <AlertTriangle className="h-5 w-5 text-orange-600 dark:text-orange-400" />
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
                      <Button
                        variant="outline"
                        size="sm"
                        className="mt-3 w-full text-xs"
                        onClick={() => setAdjustingId(item.id)}
                      >
                        <ArrowDownCircle className="mr-1.5 h-3.5 w-3.5" />
                        Record Receipt
                      </Button>
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
