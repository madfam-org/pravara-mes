import { z } from "zod";

export const createOrderSchema = z.object({
  external_id: z.string().max(255).optional().or(z.literal("")),
  customer_name: z.string().min(1, "Customer name is required").max(255),
  customer_email: z.string().email("Invalid email").optional().or(z.literal("")),
  priority: z.coerce.number().min(1).max(10).default(5),
  due_date: z.string().optional().or(z.literal("")),
  total_amount: z.coerce.number().min(0).optional(),
  currency: z.string().length(3).default("MXN"),
});

export type CreateOrderInput = z.infer<typeof createOrderSchema>;

export const updateOrderSchema = createOrderSchema.extend({
  status: z.enum([
    "received",
    "confirmed",
    "in_production",
    "quality_check",
    "ready",
    "shipped",
    "delivered",
    "cancelled",
  ]).optional(),
});

export type UpdateOrderInput = z.infer<typeof updateOrderSchema>;
