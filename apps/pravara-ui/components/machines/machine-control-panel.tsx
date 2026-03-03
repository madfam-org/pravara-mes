"use client";

import { useState } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Play,
  Pause,
  Square,
  Home,
  AlertTriangle,
  Thermometer,
  Snowflake,
  Target,
  Loader2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Separator } from "@/components/ui/separator";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useToast } from "@/lib/hooks/use-toast";
import { machinesAPI, type Machine, type MachineCommand } from "@/lib/api";

interface MachineControlPanelProps {
  machine: Machine;
}

interface CommandButtonProps {
  command: MachineCommand;
  label: string;
  description: string;
  icon: React.ReactNode;
  variant?: "default" | "secondary" | "destructive" | "outline";
  disabled?: boolean;
  requiresConfirmation?: boolean;
  onExecute: (command: MachineCommand) => void;
  isPending?: boolean;
}

function CommandButton({
  command,
  label,
  description,
  icon,
  variant = "secondary",
  disabled,
  onExecute,
  isPending,
}: CommandButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant={variant}
          size="sm"
          className="flex flex-col h-auto py-3 gap-1"
          disabled={disabled || isPending}
          onClick={() => onExecute(command)}
        >
          {isPending ? <Loader2 className="h-5 w-5 animate-spin" /> : icon}
          <span className="text-xs">{label}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        <p>{description}</p>
      </TooltipContent>
    </Tooltip>
  );
}

export function MachineControlPanel({ machine }: MachineControlPanelProps) {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [confirmCommand, setConfirmCommand] = useState<MachineCommand | null>(null);
  const [pendingCommand, setPendingCommand] = useState<MachineCommand | null>(null);

  const sendCommandMutation = useMutation({
    mutationFn: async (command: MachineCommand) => {
      setPendingCommand(command);
      return machinesAPI.sendCommand(token, machine.id, command);
    },
    onSuccess: (data) => {
      toast({
        title: `Command sent: ${pendingCommand}`,
        description: `Command ID: ${data.command_id.slice(0, 8)}...`,
        variant: "success",
      });
      queryClient.invalidateQueries({ queryKey: ["machines", machine.id] });
    },
    onError: (error: Error) => {
      toast({
        title: "Command failed",
        description: error.message,
        variant: "destructive",
      });
    },
    onSettled: () => {
      setPendingCommand(null);
    },
  });

  const handleExecuteCommand = (command: MachineCommand) => {
    if (command === "emergency_stop" || command === "stop") {
      setConfirmCommand(command);
    } else {
      sendCommandMutation.mutate(command);
    }
  };

  const handleConfirmCommand = () => {
    if (confirmCommand) {
      sendCommandMutation.mutate(confirmCommand);
      setConfirmCommand(null);
    }
  };

  const isOnline = machine.status !== "offline";
  const isRunning = machine.status === "running";
  const isPrinter = machine.type.toLowerCase().includes("printer") ||
                   machine.type.toLowerCase().includes("3d");

  return (
    <TooltipProvider>
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Machine Control</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Offline Alert */}
          {!isOnline && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertDescription>
                Machine is offline. Commands are unavailable.
              </AlertDescription>
            </Alert>
          )}

          {/* Job Control */}
          <div>
            <p className="text-xs text-muted-foreground mb-2">Job Control</p>
            <div className="grid grid-cols-4 gap-2">
              <CommandButton
                command="start_job"
                label="Start"
                description="Start a new print or manufacturing job"
                icon={<Play className="h-5 w-5" />}
                disabled={!isOnline || isRunning}
                onExecute={handleExecuteCommand}
                isPending={pendingCommand === "start_job"}
              />
              <CommandButton
                command="pause"
                label="Pause"
                description="Pause the current job (can be resumed)"
                icon={<Pause className="h-5 w-5" />}
                disabled={!isRunning}
                onExecute={handleExecuteCommand}
                isPending={pendingCommand === "pause"}
              />
              <CommandButton
                command="resume"
                label="Resume"
                description="Resume a paused job"
                icon={<Play className="h-5 w-5" />}
                disabled={!isOnline || isRunning}
                onExecute={handleExecuteCommand}
                isPending={pendingCommand === "resume"}
              />
              <CommandButton
                command="stop"
                label="Stop"
                description="Stop the current job (cannot be resumed)"
                icon={<Square className="h-5 w-5" />}
                variant="outline"
                disabled={!isOnline}
                onExecute={handleExecuteCommand}
                isPending={pendingCommand === "stop"}
              />
            </div>
          </div>

          <Separator />

          {/* Machine Control */}
          <div>
            <p className="text-xs text-muted-foreground mb-2">Machine Control</p>
            <div className="grid grid-cols-4 gap-2">
              <CommandButton
                command="home"
                label="Home"
                description="Move all axes to home position"
                icon={<Home className="h-5 w-5" />}
                disabled={!isOnline || isRunning}
                onExecute={handleExecuteCommand}
                isPending={pendingCommand === "home"}
              />
              <CommandButton
                command="calibrate"
                label="Calibrate"
                description="Run automatic calibration routine"
                icon={<Target className="h-5 w-5" />}
                disabled={!isOnline || isRunning}
                onExecute={handleExecuteCommand}
                isPending={pendingCommand === "calibrate"}
              />
              {isPrinter && (
                <>
                  <CommandButton
                    command="preheat"
                    label="Preheat"
                    description="Preheat hotend and bed to default temperatures"
                    icon={<Thermometer className="h-5 w-5" />}
                    disabled={!isOnline || isRunning}
                    onExecute={handleExecuteCommand}
                    isPending={pendingCommand === "preheat"}
                  />
                  <CommandButton
                    command="cooldown"
                    label="Cooldown"
                    description="Turn off all heaters and cool down"
                    icon={<Snowflake className="h-5 w-5" />}
                    disabled={!isOnline}
                    onExecute={handleExecuteCommand}
                    isPending={pendingCommand === "cooldown"}
                  />
                </>
              )}
            </div>
          </div>

          <Separator />

          {/* Emergency Stop */}
          <div>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="destructive"
                  className="w-full"
                  disabled={!isOnline}
                  onClick={() => handleExecuteCommand("emergency_stop")}
                >
                  <AlertTriangle className="mr-2 h-4 w-4" />
                  Emergency Stop
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Immediately halt all machine operations</p>
              </TooltipContent>
            </Tooltip>
          </div>
        </CardContent>
      </Card>

      <Dialog open={!!confirmCommand} onOpenChange={() => setConfirmCommand(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {confirmCommand === "emergency_stop" ? "Emergency Stop" : "Stop Machine"}
            </DialogTitle>
            <DialogDescription>
              {confirmCommand === "emergency_stop"
                ? "This will immediately halt all machine operations. Use only in emergencies."
                : "This will stop the current job. The machine will need to be restarted."}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmCommand(null)}>
              Cancel
            </Button>
            <Button
              variant={confirmCommand === "emergency_stop" ? "destructive" : "default"}
              onClick={handleConfirmCommand}
            >
              {confirmCommand === "emergency_stop" ? "Emergency Stop" : "Stop"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  );
}
