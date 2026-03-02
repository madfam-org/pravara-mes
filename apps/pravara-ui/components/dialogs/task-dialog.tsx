"use client";

import { useEffect } from "react";
import { useSession } from "next-auth/react";
import { useQuery } from "@tanstack/react-query";
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
import { type Task, type TaskStatus, ordersAPI, machinesAPI } from "@/lib/api";
import { createTaskSchema, updateTaskSchema, type CreateTaskInput, type UpdateTaskInput } from "@/lib/validations/task";
import { useCreateTask, useUpdateTask } from "@/lib/mutations/use-task-mutations";

interface TaskDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task?: Task;
}

const TASK_STATUSES: TaskStatus[] = [
  "backlog",
  "queued",
  "in_progress",
  "quality_check",
  "completed",
  "blocked",
];

export function TaskDialog({ open, onOpenChange, task }: TaskDialogProps) {
  const { data: session } = useSession();
  const token = (session?.user as any)?.accessToken;
  const isEditMode = !!task;

  const createMutation = useCreateTask();
  const updateMutation = useUpdateTask();

  const { data: ordersData } = useQuery({
    queryKey: ["orders"],
    queryFn: () => ordersAPI.list(token),
    enabled: !!token && open,
  });

  const { data: machinesData } = useQuery({
    queryKey: ["machines"],
    queryFn: () => machinesAPI.list(token),
    enabled: !!token && open,
  });

  const orders = ordersData?.data || [];
  const machines = machinesData?.data || [];

  const form = useForm({
    resolver: zodResolver(isEditMode ? updateTaskSchema : createTaskSchema) as any,
    defaultValues: {
      title: "",
      description: "",
      order_id: "",
      machine_id: "",
      priority: 5,
      estimated_minutes: 0,
      ...(isEditMode && { status: task.status }),
    },
  });

  useEffect(() => {
    if (task) {
      form.reset({
        title: task.title,
        description: task.description || "",
        order_id: task.order_id || "",
        machine_id: task.machine_id || "",
        priority: task.priority,
        estimated_minutes: task.estimated_minutes,
        status: task.status,
      });
    } else {
      form.reset({
        title: "",
        description: "",
        order_id: "",
        machine_id: "",
        priority: 5,
        estimated_minutes: 0,
      });
    }
  }, [task, form]);

  const onSubmit = async (data: any) => {
    if (!token) return;

    const payload = {
      ...data,
      description: data.description || undefined,
      order_id: data.order_id || undefined,
      machine_id: data.machine_id || undefined,
      estimated_minutes: data.estimated_minutes || undefined,
    };

    if (isEditMode) {
      await updateMutation.mutateAsync({
        token,
        id: task.id,
        data: payload as UpdateTaskInput,
      });
    } else {
      await createMutation.mutateAsync({
        token,
        data: payload as CreateTaskInput,
      });
    }

    onOpenChange(false);
  };

  const isLoading = createMutation.isPending || updateMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>{isEditMode ? "Edit Task" : "Create Task"}</DialogTitle>
          <DialogDescription>
            {isEditMode
              ? "Update the task details below."
              : "Fill in the details to create a new task."}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            <FormField
              control={form.control}
              name="title"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Title</FormLabel>
                  <FormControl>
                    <Input placeholder="Task title" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="description"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Description (Optional)</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="Task description"
                      className="resize-none"
                      rows={3}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className="grid grid-cols-2 gap-4">
              <FormField
                control={form.control}
                name="order_id"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Order (Optional)</FormLabel>
                    <Select onValueChange={field.onChange} value={field.value}>
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder="Select order" />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectItem value="">None</SelectItem>
                        {orders.map((order) => (
                          <SelectItem key={order.id} value={order.id}>
                            {order.customer_name}
                            {order.external_id && ` (#${order.external_id})`}
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
                name="machine_id"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Machine (Optional)</FormLabel>
                    <Select onValueChange={field.onChange} value={field.value}>
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder="Select machine" />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectItem value="">None</SelectItem>
                        {machines.map((machine) => (
                          <SelectItem key={machine.id} value={machine.id}>
                            {machine.name} ({machine.code})
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
                name="priority"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Priority</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        min={1}
                        max={10}
                        {...field}
                        onChange={(e) => field.onChange(e.target.valueAsNumber)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="estimated_minutes"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Est. Time (minutes)</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        min={1}
                        placeholder="60"
                        {...field}
                        onChange={(e) =>
                          field.onChange(
                            e.target.value ? e.target.valueAsNumber : undefined
                          )
                        }
                        value={field.value ?? ""}
                      />
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
                          {TASK_STATUSES.map((status) => (
                            <SelectItem key={status} value={status}>
                              {status.replace("_", " ")}
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
                {isLoading
                  ? "Saving..."
                  : isEditMode
                  ? "Update Task"
                  : "Create Task"}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
