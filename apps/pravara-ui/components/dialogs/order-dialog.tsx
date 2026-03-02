"use client";

import { useEffect } from "react";
import { useSession } from "next-auth/react";
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
import { Button } from "@/components/ui/button";
import { type Order, type OrderStatus } from "@/lib/api";
import { createOrderSchema, updateOrderSchema, type CreateOrderInput, type UpdateOrderInput } from "@/lib/validations/order";
import { useCreateOrder, useUpdateOrder } from "@/lib/mutations/use-order-mutations";

interface OrderDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  order?: Order;
}

const ORDER_STATUSES: OrderStatus[] = [
  "received",
  "confirmed",
  "in_production",
  "quality_check",
  "ready",
  "shipped",
  "delivered",
  "cancelled",
];

export function OrderDialog({ open, onOpenChange, order }: OrderDialogProps) {
  const { data: session } = useSession();
  const token = (session?.user as any)?.accessToken;
  const isEditMode = !!order;

  const createMutation = useCreateOrder();
  const updateMutation = useUpdateOrder();

  const form = useForm({
    resolver: zodResolver(isEditMode ? updateOrderSchema : createOrderSchema) as any,
    defaultValues: {
      external_id: "",
      customer_name: "",
      customer_email: "",
      priority: 5,
      due_date: "",
      total_amount: 0,
      currency: "MXN",
      ...(isEditMode && { status: order.status }),
    },
  });

  useEffect(() => {
    if (order) {
      form.reset({
        external_id: order.external_id || "",
        customer_name: order.customer_name,
        customer_email: order.customer_email || "",
        priority: order.priority,
        due_date: order.due_date ? order.due_date.split("T")[0] : "",
        total_amount: order.total_amount,
        currency: order.currency,
        status: order.status,
      });
    } else {
      form.reset({
        external_id: "",
        customer_name: "",
        customer_email: "",
        priority: 5,
        due_date: "",
        total_amount: 0,
        currency: "MXN",
      });
    }
  }, [order, form]);

  const onSubmit = async (data: any) => {
    if (!token) return;

    const payload = {
      ...data,
      external_id: data.external_id || undefined,
      customer_email: data.customer_email || undefined,
      due_date: data.due_date || undefined,
      total_amount: data.total_amount || undefined,
    };

    if (isEditMode) {
      await updateMutation.mutateAsync({
        token,
        id: order.id,
        data: payload as UpdateOrderInput,
      });
    } else {
      await createMutation.mutateAsync({
        token,
        data: payload as CreateOrderInput,
      });
    }

    onOpenChange(false);
  };

  const isLoading = createMutation.isPending || updateMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>{isEditMode ? "Edit Order" : "Create Order"}</DialogTitle>
          <DialogDescription>
            {isEditMode
              ? "Update the order details below."
              : "Fill in the details to create a new order."}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <FormField
                control={form.control}
                name="customer_name"
                render={({ field }) => (
                  <FormItem className="col-span-2">
                    <FormLabel>Customer Name</FormLabel>
                    <FormControl>
                      <Input placeholder="Acme Corporation" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="external_id"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Order ID (Optional)</FormLabel>
                    <FormControl>
                      <Input placeholder="ORD-2024-001" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="customer_email"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Email (Optional)</FormLabel>
                    <FormControl>
                      <Input
                        type="email"
                        placeholder="customer@example.com"
                        {...field}
                      />
                    </FormControl>
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
                name="due_date"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Due Date (Optional)</FormLabel>
                    <FormControl>
                      <Input type="date" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="total_amount"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Amount (Optional)</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        step="0.01"
                        min={0}
                        placeholder="0.00"
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

              <FormField
                control={form.control}
                name="currency"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Currency</FormLabel>
                    <FormControl>
                      <Input placeholder="MXN" maxLength={3} {...field} />
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
                          {ORDER_STATUSES.map((status) => (
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
                  ? "Update Order"
                  : "Create Order"}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
