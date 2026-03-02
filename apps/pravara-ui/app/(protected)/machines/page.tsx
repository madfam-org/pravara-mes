"use client";

import { useState } from "react";
import { useSession } from "next-auth/react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Factory, Activity, Wifi, WifiOff, MoreVertical, Edit, Trash } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { machinesAPI, type Machine, type MachineStatus } from "@/lib/api";
import { formatRelativeTime } from "@/lib/utils";
import { MachineDialog } from "@/components/dialogs/machine-dialog";
import { ConfirmDialog } from "@/components/dialogs/confirm-dialog";
import { useDeleteMachine } from "@/lib/mutations/use-machine-mutations";

const statusConfig: Record<MachineStatus, { color: string; icon: typeof Wifi }> = {
  offline: { color: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-400", icon: WifiOff },
  online: { color: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400", icon: Wifi },
  idle: { color: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400", icon: Activity },
  running: { color: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400", icon: Activity },
  maintenance: { color: "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400", icon: Activity },
  error: { color: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400", icon: Activity },
};

export default function MachinesPage() {
  const { data: session } = useSession();
  const token = (session?.user as any)?.accessToken;
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedMachine, setSelectedMachine] = useState<Machine | undefined>();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [machineToDelete, setMachineToDelete] = useState<Machine | undefined>();

  const deleteMutation = useDeleteMachine();

  const { data, isLoading } = useQuery({
    queryKey: ["machines"],
    queryFn: () => machinesAPI.list(token),
    enabled: !!token,
  });

  const machines = data?.data || [];

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

      {isLoading ? (
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
      ) : machines.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <Factory className="h-12 w-12 text-muted-foreground" />
            <h3 className="mt-4 text-lg font-semibold">No machines registered</h3>
            <p className="text-muted-foreground">
              Register your first machine to start monitoring
            </p>
            <Button className="mt-4" onClick={handleNewMachine}>
              <Plus className="mr-2 h-4 w-4" />
              Register Machine
            </Button>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {machines.map((machine) => {
            const config = statusConfig[machine.status] || statusConfig.offline;
            const StatusIcon = config.icon;

            return (
              <Card key={machine.id} className="hover:shadow-md transition-shadow">
                <CardHeader className="pb-2">
                  <div className="flex items-start justify-between">
                    <div>
                      <CardTitle className="text-lg">{machine.name}</CardTitle>
                      <p className="text-sm font-mono text-muted-foreground">
                        {machine.code}
                      </p>
                    </div>
                    <div className="flex items-center gap-2">
                      <span
                        className={`inline-flex items-center gap-1 rounded-full px-2 py-1 text-xs font-medium ${config.color}`}
                      >
                        <StatusIcon className="h-3 w-3" />
                        {machine.status}
                      </span>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon" className="h-8 w-8">
                            <MoreVertical className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => handleEditMachine(machine)}>
                            <Edit className="mr-2 h-4 w-4" />
                            Edit
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            onClick={() => handleDeleteClick(machine)}
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
                      <span className="font-medium">{machine.type}</span>
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
              </Card>
            );
          })}
        </div>
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
