"use client";

import { useState, useMemo } from "react";
import { useSession } from "next-auth/react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Search, MoreVertical, Edit, Trash } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SearchInput } from "@/components/search-input";
import {
  FilterPopover,
  ActiveFilters,
  type FilterGroup,
  type FilterState,
} from "@/components/filter-popover";
import { ViewToggle, type ViewMode } from "@/components/view-toggle";
import { DataTable } from "@/components/data-table";
import { ordersAPI, type Order, type OrderStatus } from "@/lib/api";
import { formatDate } from "@/lib/utils";
import { OrderDialog } from "@/components/dialogs/order-dialog";
import { ConfirmDialog } from "@/components/dialogs/confirm-dialog";
import { useDeleteOrder } from "@/lib/mutations/use-order-mutations";
import { getOrderColumns } from "./columns";

const statusColors: Record<OrderStatus, string> = {
  received: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
  confirmed: "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400",
  in_production: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400",
  quality_check: "bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400",
  ready: "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400",
  shipped: "bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400",
  delivered: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",
  cancelled: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",
};

const statusLabels: Record<OrderStatus, string> = {
  received: "Received",
  confirmed: "Confirmed",
  in_production: "In Production",
  quality_check: "Quality Check",
  ready: "Ready",
  shipped: "Shipped",
  delivered: "Delivered",
  cancelled: "Cancelled",
};

const priorityOptions = [
  { value: "1", label: "Low", numValue: 1 },
  { value: "2", label: "Normal", numValue: 2 },
  { value: "3", label: "High", numValue: 3 },
  { value: "4", label: "Urgent", numValue: 4 },
];

const priorityLabels: Record<number, string> = {
  1: "Low",
  2: "Normal",
  3: "High",
  4: "Urgent",
};

export default function OrdersPage() {
  const { data: session } = useSession();
  const token = (session?.user as any)?.accessToken;
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedOrder, setSelectedOrder] = useState<Order | undefined>();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [orderToDelete, setOrderToDelete] = useState<Order | undefined>();

  // View state
  const [view, setView] = useState<ViewMode>("grid");

  // Search and filter state
  const [searchQuery, setSearchQuery] = useState("");
  const [filters, setFilters] = useState<FilterState>({});

  const deleteMutation = useDeleteOrder();

  const { data, isLoading } = useQuery({
    queryKey: ["orders"],
    queryFn: () => ordersAPI.list(token),
    enabled: !!token,
  });

  const orders = data?.data || [];

  // Generate filter groups with counts
  const filterGroups: FilterGroup[] = useMemo(() => {
    const statusCounts = orders.reduce((acc, order) => {
      acc[order.status] = (acc[order.status] || 0) + 1;
      return acc;
    }, {} as Record<string, number>);

    const priorityCounts = orders.reduce((acc, order) => {
      acc[String(order.priority)] = (acc[String(order.priority)] || 0) + 1;
      return acc;
    }, {} as Record<string, number>);

    return [
      {
        id: "status",
        label: "Status",
        multiple: true,
        options: Object.entries(statusLabels).map(([value, label]) => ({
          value,
          label,
          count: statusCounts[value] || 0,
        })),
      },
      {
        id: "priority",
        label: "Priority",
        multiple: true,
        options: priorityOptions.map((opt) => ({
          ...opt,
          count: priorityCounts[opt.value] || 0,
        })),
      },
    ];
  }, [orders]);

  // Filter and search orders
  const filteredOrders = useMemo(() => {
    return orders.filter((order) => {
      // Search filter
      if (searchQuery) {
        const query = searchQuery.toLowerCase();
        const matchesSearch =
          order.customer_name?.toLowerCase().includes(query) ||
          order.customer_email?.toLowerCase().includes(query) ||
          order.external_id?.toLowerCase().includes(query);
        if (!matchesSearch) return false;
      }

      // Status filter
      const statusFilters = filters.status || [];
      if (statusFilters.length > 0 && !statusFilters.includes(order.status)) {
        return false;
      }

      // Priority filter
      const priorityFilters = filters.priority || [];
      if (priorityFilters.length > 0 && !priorityFilters.includes(String(order.priority))) {
        return false;
      }

      return true;
    });
  }, [orders, searchQuery, filters]);

  const handleNewOrder = () => {
    setSelectedOrder(undefined);
    setDialogOpen(true);
  };

  const handleEditOrder = (order: Order) => {
    setSelectedOrder(order);
    setDialogOpen(true);
  };

  const handleDeleteClick = (order: Order) => {
    setOrderToDelete(order);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!token || !orderToDelete) return;
    await deleteMutation.mutateAsync({ token, id: orderToDelete.id });
    setOrderToDelete(undefined);
  };

  // Get columns for table view
  const columns = useMemo(
    () =>
      getOrderColumns({
        onEdit: handleEditOrder,
        onDelete: handleDeleteClick,
      }),
    []
  );

  const renderGridView = () => (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {filteredOrders.map((order) => (
        <Card key={order.id} className="hover:shadow-md transition-shadow">
          <CardHeader className="pb-2">
            <div className="flex items-start justify-between">
              <CardTitle className="text-lg">{order.customer_name}</CardTitle>
              <div className="flex items-center gap-2">
                <span
                  className={`inline-flex items-center rounded-full px-2 py-1 text-xs font-medium ${
                    statusColors[order.status]
                  }`}
                >
                  {statusLabels[order.status]}
                </span>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8"
                      aria-label={`Actions for ${order.customer_name}`}
                    >
                      <MoreVertical className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => handleEditOrder(order)}>
                      <Edit className="mr-2 h-4 w-4" />
                      Edit
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() => handleDeleteClick(order)}
                      className="text-destructive"
                    >
                      <Trash className="mr-2 h-4 w-4" />
                      Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </div>
            {order.external_id && (
              <p className="text-sm text-muted-foreground">
                #{order.external_id}
              </p>
            )}
          </CardHeader>
          <CardContent>
            <div className="space-y-2 text-sm">
              {order.customer_email && (
                <p className="text-muted-foreground">{order.customer_email}</p>
              )}
              <div className="flex justify-between">
                <span className="text-muted-foreground">Priority</span>
                <span className="font-medium">{priorityLabels[order.priority] || order.priority}</span>
              </div>
              {order.due_date && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Due Date</span>
                  <span className="font-medium">{formatDate(order.due_date)}</span>
                </div>
              )}
              {order.total_amount && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Amount</span>
                  <span className="font-medium">
                    {new Intl.NumberFormat("en-US", {
                      style: "currency",
                      currency: order.currency || "MXN",
                    }).format(order.total_amount)}
                  </span>
                </div>
              )}
              <p className="text-xs text-muted-foreground pt-2">
                Created {formatDate(order.created_at)}
              </p>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );

  const renderTableView = () => (
    <DataTable
      columns={columns}
      data={filteredOrders}
      isLoading={isLoading}
      emptyMessage={orders.length === 0 ? "No orders yet" : "No matching orders"}
    />
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Orders</h1>
          <p className="text-muted-foreground">
            Manage and track production orders
          </p>
        </div>
        <Button size="sm" onClick={handleNewOrder}>
          <Plus className="mr-2 h-4 w-4" />
          New Order
        </Button>
      </div>

      {/* Search, Filter, and View Toggle Bar */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
          <SearchInput
            placeholder="Search orders..."
            value={searchQuery}
            onChange={setSearchQuery}
            className="w-full sm:w-80"
          />
          <FilterPopover
            groups={filterGroups}
            value={filters}
            onChange={setFilters}
          />
        </div>
        <ViewToggle view={view} onViewChange={setView} />
      </div>

      {/* Active Filters */}
      <ActiveFilters
        groups={filterGroups}
        value={filters}
        onChange={setFilters}
      />

      {isLoading && view === "grid" ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(6)].map((_, i) => (
            <Card key={i} className="animate-pulse">
              <CardHeader>
                <div className="h-5 w-32 rounded bg-muted" />
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="h-4 w-full rounded bg-muted" />
                  <div className="h-4 w-2/3 rounded bg-muted" />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : filteredOrders.length === 0 && view === "grid" ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <Search className="h-12 w-12 text-muted-foreground" />
            <h3 className="mt-4 text-lg font-semibold">
              {orders.length === 0 ? "No orders yet" : "No matching orders"}
            </h3>
            <p className="text-muted-foreground">
              {orders.length === 0
                ? "Create your first order to get started"
                : "Try adjusting your search or filters"}
            </p>
            {orders.length === 0 && (
              <Button className="mt-4" onClick={handleNewOrder}>
                <Plus className="mr-2 h-4 w-4" />
                Create Order
              </Button>
            )}
            {orders.length > 0 && (searchQuery || Object.keys(filters).length > 0) && (
              <Button
                variant="outline"
                className="mt-4"
                onClick={() => {
                  setSearchQuery("");
                  setFilters({});
                }}
              >
                Clear filters
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <>
          {/* Results count */}
          {(searchQuery || Object.keys(filters).length > 0) && (
            <p className="text-sm text-muted-foreground">
              Showing {filteredOrders.length} of {orders.length} orders
            </p>
          )}

          {view === "grid" ? renderGridView() : renderTableView()}
        </>
      )}

      <OrderDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        order={selectedOrder}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Order"
        description={`Are you sure you want to delete the order for ${orderToDelete?.customer_name}? This action cannot be undone.`}
        onConfirm={handleDeleteConfirm}
        variant="destructive"
      />
    </div>
  );
}
