import { z } from "zod";

/**
 * Order status enum with all possible states
 */
export const orderStatusEnum = z.enum([
  "received",
  "confirmed",
  "in_production",
  "quality_check",
  "ready",
  "shipped",
  "delivered",
  "cancelled",
]);

export type OrderStatus = z.infer<typeof orderStatusEnum>;

/**
 * Supported currency codes (ISO 4217)
 */
export const currencyEnum = z.enum(["MXN", "USD", "EUR", "GBP", "CAD"]);

export type Currency = z.infer<typeof currencyEnum>;

/**
 * Schema for creating a new order
 *
 * Validates:
 * - external_id: Optional reference from external system
 * - customer_name: Required, customer or company name
 * - customer_email: Optional, must be valid email format
 * - priority: 1-10 scale (1=lowest, 10=highest)
 * - due_date: Optional ISO date string
 * - total_amount: Optional positive number
 * - currency: ISO 4217 currency code
 */
export const createOrderSchema = z.object({
  external_id: z
    .string()
    .max(255, "External ID cannot exceed 255 characters")
    .regex(
      /^$|^[A-Za-z0-9_-]+$/,
      "External ID can only contain letters, numbers, underscores, and hyphens"
    )
    .optional()
    .or(z.literal("")),
  customer_name: z
    .string()
    .min(1, "Customer name is required")
    .max(255, "Customer name cannot exceed 255 characters")
    .trim(),
  customer_email: z
    .string()
    .email("Please enter a valid email address")
    .max(255, "Email cannot exceed 255 characters")
    .optional()
    .or(z.literal("")),
  priority: z.coerce
    .number({ message: "Priority must be a valid number" })
    .int({ message: "Priority must be a whole number" })
    .min(1, { message: "Priority must be at least 1" })
    .max(10, { message: "Priority cannot exceed 10" })
    .default(5),
  due_date: z
    .string()
    .refine(
      (val) => val === "" || !isNaN(Date.parse(val)),
      "Please enter a valid date"
    )
    .optional()
    .or(z.literal("")),
  total_amount: z.coerce
    .number({ message: "Amount must be a valid number" })
    .min(0, { message: "Amount cannot be negative" })
    .max(999999999.99, { message: "Amount exceeds maximum allowed value" })
    .optional(),
  currency: z
    .string()
    .length(3, "Currency code must be exactly 3 characters")
    .toUpperCase()
    .default("MXN"),
});

export type CreateOrderInput = z.infer<typeof createOrderSchema>;

/**
 * Schema for updating an existing order
 *
 * Extends create schema with optional status field
 */
export const updateOrderSchema = createOrderSchema.extend({
  status: orderStatusEnum.optional(),
});

export type UpdateOrderInput = z.infer<typeof updateOrderSchema>;
