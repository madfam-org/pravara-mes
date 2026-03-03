"use client";

import { useEffect } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { type Machine, type MachineStatus } from "@/lib/api";
import { createMachineSchema, updateMachineSchema, type CreateMachineInput, type UpdateMachineInput } from "@/lib/validations/machine";
import { useCreateMachine, useUpdateMachine } from "@/lib/mutations/use-machine-mutations";

interface MachineDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  machine?: Machine;
}

const MACHINE_STATUSES: MachineStatus[] = [
  "offline",
  "online",
  "idle",
  "running",
  "maintenance",
  "error",
];

const MACHINE_TYPES = [
  "CNC Mill",
  "CNC Lathe",
  "3D Printer",
  "Laser Cutter",
  "Injection Molding",
  "Assembly Station",
  "Quality Control",
  "Packaging",
  "Other",
];

export function MachineDialog({ open, onOpenChange, machine }: MachineDialogProps) {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const isEditMode = !!machine;

  const createMutation = useCreateMachine();
  const updateMutation = useUpdateMachine();

  const form = useForm({
    resolver: zodResolver(isEditMode ? updateMachineSchema : createMachineSchema) as any,
    defaultValues: {
      name: "",
      code: "",
      type: "",
      description: "",
      mqtt_topic: "",
      location: "",
      ...(isEditMode && { status: machine.status }),
    },
  });

  useEffect(() => {
    if (machine) {
      form.reset({
        name: machine.name,
        code: machine.code,
        type: machine.type,
        description: machine.description || "",
        mqtt_topic: machine.mqtt_topic || "",
        location: machine.location || "",
        status: machine.status,
      });
    } else {
      form.reset({
        name: "",
        code: "",
        type: "",
        description: "",
        mqtt_topic: "",
        location: "",
      });
    }
  }, [machine, form]);

  const onSubmit = async (data: any) => {
    if (!token) return;

    const payload = {
      ...data,
      description: data.description || undefined,
      mqtt_topic: data.mqtt_topic || undefined,
      location: data.location || undefined,
    };

    if (isEditMode) {
      await updateMutation.mutateAsync({
        token,
        id: machine.id,
        data: payload as UpdateMachineInput,
      });
    } else {
      await createMutation.mutateAsync({
        token,
        data: payload as CreateMachineInput,
      });
    }

    onOpenChange(false);
  };

  const isLoading = createMutation.isPending || updateMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>
            {isEditMode ? "Edit Machine" : "Register Machine"}
          </DialogTitle>
          <DialogDescription>
            {isEditMode
              ? "Update the machine details below."
              : "Fill in the details to register a new machine."}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Machine Name</FormLabel>
                    <FormControl>
                      <Input placeholder="CNC Mill #1" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="code"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Machine Code</FormLabel>
                    <FormControl>
                      <Input
                        placeholder="CNC-001"
                        {...field}
                        onChange={(e) =>
                          field.onChange(e.target.value.toUpperCase())
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="type"
                render={({ field }) => (
                  <FormItem className="col-span-2">
                    <FormLabel>Machine Type</FormLabel>
                    <Select onValueChange={field.onChange} value={field.value}>
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder="Select machine type" />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        {MACHINE_TYPES.map((type) => (
                          <SelectItem key={type} value={type}>
                            {type}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="location"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Location (Optional)</FormLabel>
                    <FormControl>
                      <Input placeholder="Building A, Floor 2" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="mqtt_topic"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>MQTT Topic (Optional)</FormLabel>
                    <FormControl>
                      <Input placeholder="machines/cnc-001" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {isEditMode && (
                <FormField
                  control={form.control}
                  name="status"
                  render={({ field }) => (
                    <FormItem className="col-span-2">
                      <FormLabel>Status</FormLabel>
                      <Select
                        onValueChange={field.onChange}
                        defaultValue={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder="Select status" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {MACHINE_STATUSES.map((status) => (
                            <SelectItem key={status} value={status}>
                              {status}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
            </div>

            <FormField
              control={form.control}
              name="description"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Description (Optional)</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="Machine description and specifications"
                      className="resize-none"
                      rows={3}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
                disabled={isLoading}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={isLoading}>
                {isLoading && <Spinner size="sm" className="mr-2" />}
                {isLoading
                  ? "Saving..."
                  : isEditMode
                  ? "Update Machine"
                  : "Register Machine"}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
