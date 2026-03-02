"use client";

import { useState, useMemo } from "react";
import Link from "next/link";
import { useSession } from "next-auth/react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Factory, Activity, Wifi, WifiOff, MoreVertical, Edit, Trash, ExternalLink, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
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
import { machinesAPI, type Machine, type MachineStatus } from "@/lib/api";
import { formatRelativeTime } from "@/lib/utils";
import { MachineDialog } from "@/components/dialogs/machine-dialog";
import { ConfirmDialog } from "@/components/dialogs/confirm-dialog";
import { useDeleteMachine } from "@/lib/mutations/use-machine-mutations";
import { getMachineColumns } from "./columns";

const statusConfig: Record<MachineStatus, { variant: "default" | "secondary" | "destructive" | "outline" | "success" | "warning" | "error"; icon: typeof Wifi; label: string }> = {
  offline: { variant: "secondary", icon: WifiOff, label: "Offline" },
  online: { variant: "success", icon: Wifi, label: "Online" },
  idle: { variant: "warning", icon: Activity, label: "Idle" },
  running: { variant: "default", icon: Activity, label: "Running" },
  maintenance: { variant: "warning", icon: Activity, label: "Maintenance" },
  error: { variant: "error", icon: Activity, label: "Error" },
};

const typeOptions = [
  { value: "cnc", label: "CNC" },
  { value: "laser", label: "Laser" },
  { value: "3d_printer", label: "3D Printer" },
  { value: "injection", label: "Injection" },
  { value: "assembly", label: "Assembly" },
  { value: "other", label: "Other" },
];

export default function MachinesPage() {
  const { data: session } = useSession();
  const token = (session?.user as any)?.accessToken;
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedMachine, setSelectedMachine] = useState<Machine | undefined>();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [machineToDelete, setMachineToDelete] = useState<Machine | undefined>();

  // View state
  const [view, setView] = useState<ViewMode>("grid");

  // Search and filter state
  const [searchQuery, setSearchQuery] = useState("");
  const [filters, setFilters] = useState<FilterState>({});

  const deleteMutation = useDeleteMachine();

  const { data, isLoading } = useQuery({
    queryKey: ["machines"],
    queryFn: () => machinesAPI.list(token),
    enabled: !!token,
  });

  const machines = data?.data || [];

  // Generate filter groups with counts
  const filterGroups: FilterGroup[] = useMemo(() => {
    const statusCounts = machines.reduce((acc, machine) => {
      acc[machine.status] = (acc[machine.status] || 0) + 1;
      return acc;
    }, {} as Record<string, number>);

    const typeCounts = machines.reduce((acc, machine) => {
      acc[machine.type] = (acc[machine.type] || 0) + 1;
      return acc;
    }, {} as Record<string, number>);

    const locationCounts = machines.reduce((acc, machine) => {
      const loc = machine.location || "unspecified";
      acc[loc] = (acc[loc] || 0) + 1;
      return acc;
    }, {} as Record<string, number>);

    const locationOptions = Object.entries(locationCounts)
      .filter(([loc]) => loc !== "unspecified")
      .map(([value, count]) => ({ value, label: value, count }));

    return [
      {
        id: "status",
        label: "Status",
        multiple: true,
        options: Object.entries(statusConfig).map(([value, config]) => ({
          value,
          label: config.label,
          count: statusCounts[value] || 0,
        })),
      },
      {
        id: "type",
        label: "Type",
        multiple: true,
        options: typeOptions.map((opt) => ({
          ...opt,
          count: typeCounts[opt.value] || 0,
        })),
      },
      ...(locationOptions.length > 0
        ? [
            {
              id: "location",
              label: "Location",
              multiple: true,
              options: locationOptions,
            },
          ]
        : []),
    ];
  }, [machines]);

  // Filter and search machines
  const filteredMachines = useMemo(() => {
    return machines.filter((machine) => {
      // Search filter
      if (searchQuery) {
        const query = searchQuery.toLowerCase();
        const matchesSearch =
          machine.name?.toLowerCase().includes(query) ||
          machine.code?.toLowerCase().includes(query) ||
          machine.location?.toLowerCase().includes(query) ||
          machine.description?.toLowerCase().includes(query);
        if (!matchesSearch) return false;
      }

      // Status filter
      const statusFilters = filters.status || [];
      if (statusFilters.length > 0 && !statusFilters.includes(machine.status)) {
        return false;
      }

      // Type filter
      const typeFilters = filters.type || [];
      if (typeFilters.length > 0 && !typeFilters.includes(machine.type)) {
        return false;
      }

      // Location filter
      const locationFilters = filters.location || [];
      if (locationFilters.length > 0 && !locationFilters.includes(machine.location || "")) {
        return false;
      }

      return true;
    });
  }, [machines, searchQuery, filters]);

  const onlineMachines = machines.filter(
    (m) => m.status === "online" || m.status === "running"
  ).length;

  const handleNewMachine = () => {
    setSelectedMachine(undefined);
    setDialogOpen(true);
  };

  const handleEditMachine = (machine: Machine) => {
    setSelectedMachine(machine);
    setDialogOpen(true);
  };

  const handleDeleteClick = (machine: Machine) => {
    setMachineToDelete(machine);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!token || !machineToDelete) return;
    await deleteMutation.mutateAsync({ token, id: machineToDelete.id });
    setMachineToDelete(undefined);
  };

  // Get columns for table view
  const columns = useMemo(
    () =>
      getMachineColumns({
        onEdit: handleEditMachine,
        onDelete: handleDeleteClick,
      }),
    []
  );

  const renderGridView = () => (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {filteredMachines.map((machine) => {
        const config = statusConfig[machine.status] || statusConfig.offline;
        const StatusIcon = config.icon;

        return (
          <Card key={machine.id} className="hover:shadow-md transition-shadow cursor-pointer group">
            <Link href={`/machines/${machine.id}`} className="block">
              <CardHeader className="pb-2">
                <div className="flex items-start justify-between">
                  <div>
                    <CardTitle className="text-lg">{machine.name}</CardTitle>
                    <p className="text-sm font-mono text-muted-foreground">
                      {machine.code}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant={config.variant} className="gap-1">
                      <StatusIcon className="h-3 w-3" />
                      {config.label}
                    </Badge>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild onClick={(e) => e.preventDefault()}>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          aria-label={`Actions for ${machine.name}`}
                        >
                          <MoreVertical className="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem asChild>
                          <Link href={`/machines/${machine.id}`}>
                            <ExternalLink className="mr-2 h-4 w-4" />
                            View Details
                          </Link>
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={(e) => {
                          e.preventDefault();
                          handleEditMachine(machine);
                        }}>
                          <Edit className="mr-2 h-4 w-4" />
                          Edit
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={(e) => {
                            e.preventDefault();
                            handleDeleteClick(machine);
                          }}
                          className="text-destructive"
                        >
                          <Trash className="mr-2 h-4 w-4" />
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Type</span>
                    <span className="font-medium capitalize">{machine.type.replace("_", " ")}</span>
                  </div>
                  {machine.location && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Location</span>
                      <span className="font-medium">{machine.location}</span>
                    </div>
                  )}
                  {machine.mqtt_topic && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">MQTT Topic</span>
                      <span className="font-mono text-xs">{machine.mqtt_topic}</span>
                    </div>
                  )}
                  {machine.last_heartbeat && (
                    <p className="text-xs text-muted-foreground pt-2">
                      Last seen {formatRelativeTime(machine.last_heartbeat)}
                    </p>
                  )}
                  {machine.description && (
                    <p className="text-muted-foreground line-clamp-2 pt-1">
                      {machine.description}
                    </p>
                  )}
                </div>
              </CardContent>
            </Link>
          </Card>
        );
      })}
    </div>
  );

  const renderTableView = () => (
    <DataTable
      columns={columns}
      data={filteredMachines}
      isLoading={isLoading}
      emptyMessage={machines.length === 0 ? "No machines registered" : "No matching machines"}
    />
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Machines</h1>
          <p className="text-muted-foreground">
            {onlineMachines} of {machines.length} machines online
          </p>
        </div>
        <Button size="sm" onClick={handleNewMachine}>
          <Plus className="mr-2 h-4 w-4" />
          Register Machine
        </Button>
      </div>

      {/* Search, Filter, and View Toggle Bar */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
          <SearchInput
            placeholder="Search machines..."
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
      ) : filteredMachines.length === 0 && view === "grid" ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <Factory className="h-12 w-12 text-muted-foreground" />
            <h3 className="mt-4 text-lg font-semibold">
              {machines.length === 0 ? "No machines registered" : "No matching machines"}
            </h3>
            <p className="text-muted-foreground">
              {machines.length === 0
                ? "Register your first machine to start monitoring"
                : "Try adjusting your search or filters"}
            </p>
            {machines.length === 0 && (
              <Button className="mt-4" onClick={handleNewMachine}>
                <Plus className="mr-2 h-4 w-4" />
                Register Machine
              </Button>
            )}
            {machines.length > 0 && (searchQuery || Object.keys(filters).length > 0) && (
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
              Showing {filteredMachines.length} of {machines.length} machines
            </p>
          )}

          {view === "grid" ? renderGridView() : renderTableView()}
        </>
      )}

      <MachineDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        machine={selectedMachine}
      />

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Machine"
        description={`Are you sure you want to delete ${machineToDelete?.name}? This action cannot be undone.`}
        onConfirm={handleDeleteConfirm}
        variant="destructive"
      />
    </div>
  );
}
