"use client";

import Link from "next/link";
import { ColumnDef } from "@tanstack/react-table";
import { MoreHorizontal, Edit, Trash, ExternalLink, Wifi, WifiOff, Activity } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header";
import { type Machine, type MachineStatus } from "@/lib/api";
import { formatRelativeTime } from "@/lib/utils";

const statusConfig: Record<MachineStatus, { variant: "default" | "secondary" | "destructive" | "outline" | "success" | "warning" | "error"; icon: typeof Wifi; label: string }> = {
  offline: { variant: "secondary", icon: WifiOff, label: "Offline" },
  online: { variant: "success", icon: Wifi, label: "Online" },
  idle: { variant: "warning", icon: Activity, label: "Idle" },
  running: { variant: "default", icon: Activity, label: "Running" },
  maintenance: { variant: "warning", icon: Activity, label: "Maintenance" },
  error: { variant: "error", icon: Activity, label: "Error" },
};

interface MachineColumnsProps {
  onEdit: (machine: Machine) => void;
  onDelete: (machine: Machine) => void;
}

export function getMachineColumns({ onEdit, onDelete }: MachineColumnsProps): ColumnDef<Machine>[] {
  return [
    {
      accessorKey: "name",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Name" />
      ),
      cell: ({ row }) => {
        const machine = row.original;
        return (
          <div>
            <Link
              href={`/machines/${machine.id}`}
              className="font-medium hover:underline"
            >
              {machine.name}
            </Link>
            <div className="text-sm font-mono text-muted-foreground">{machine.code}</div>
          </div>
        );
      },
    },
    {
      accessorKey: "status",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Status" />
      ),
      cell: ({ row }) => {
        const status = row.getValue("status") as MachineStatus;
        const config = statusConfig[status] || statusConfig.offline;
        const StatusIcon = config.icon;
        return (
          <Badge variant={config.variant} className="gap-1">
            <StatusIcon className="h-3 w-3" />
            {config.label}
          </Badge>
        );
      },
      filterFn: (row, id, value) => {
        return value.includes(row.getValue(id));
      },
    },
    {
      accessorKey: "type",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Type" />
      ),
      cell: ({ row }) => (
        <span className="capitalize">{(row.getValue("type") as string).replace("_", " ")}</span>
      ),
    },
    {
      accessorKey: "location",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Location" />
      ),
      cell: ({ row }) => (
        <span>{row.getValue("location") || "-"}</span>
      ),
    },
    {
      accessorKey: "mqtt_topic",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="MQTT Topic" />
      ),
      cell: ({ row }) => {
        const topic = row.getValue("mqtt_topic") as string | null;
        return topic ? (
          <span className="font-mono text-xs">{topic}</span>
        ) : (
          <span className="text-muted-foreground">-</span>
        );
      },
    },
    {
      accessorKey: "last_heartbeat",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="Last Seen" />
      ),
      cell: ({ row }) => {
        const heartbeat = row.getValue("last_heartbeat") as string | null;
        return heartbeat ? (
          <span className="text-sm text-muted-foreground">
            {formatRelativeTime(heartbeat)}
          </span>
        ) : (
          <span className="text-muted-foreground">-</span>
        );
      },
    },
    {
      id: "actions",
      cell: ({ row }) => {
        const machine = row.original;
        return (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                className="h-8 w-8 p-0"
                aria-label={`Actions for ${machine.name}`}
              >
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuLabel>Actions</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <Link href={`/machines/${machine.id}`}>
                  <ExternalLink className="mr-2 h-4 w-4" />
                  View Details
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onEdit(machine)}>
                <Edit className="mr-2 h-4 w-4" />
                Edit
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => onDelete(machine)}
                className="text-destructive"
              >
                <Trash className="mr-2 h-4 w-4" />
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        );
      },
    },
  ];
}
