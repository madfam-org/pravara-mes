import { z } from "zod";

/**
 * Task status enum for Kanban workflow
 */
export const taskStatusEnum = z.enum([
  "backlog",
  "queued",
  "in_progress",
  "quality_check",
  "completed",
  "blocked",
]);

export type TaskStatus = z.infer<typeof taskStatusEnum>;

/**
 * UUID validation helper (allows empty string for optional fields)
 */
const optionalUuid = z
  .string()
  .refine(
    (val) =>
      val === "" ||
      /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
        val
      ),
    "Invalid ID format"
  )
  .optional()
  .or(z.literal(""));

/**
 * Schema for creating a new task
 *
 * Validates:
 * - title: Required, task name
 * - description: Optional, detailed description
 * - order_id: Optional, linked order UUID
 * - machine_id: Optional, assigned machine UUID
 * - priority: 1-10 scale (1=lowest, 10=highest)
 * - estimated_minutes: Optional, positive integer
 */
export const createTaskSchema = z.object({
  title: z
    .string()
    .min(1, "Task title is required")
    .max(255, "Task title cannot exceed 255 characters")
    .trim(),
  description: z
    .string()
    .max(2000, "Description cannot exceed 2000 characters")
    .optional()
    .or(z.literal("")),
  order_id: optionalUuid,
  machine_id: optionalUuid,
  priority: z.coerce
    .number({ message: "Priority must be a valid number" })
    .int({ message: "Priority must be a whole number" })
    .min(1, { message: "Priority must be at least 1" })
    .max(10, { message: "Priority cannot exceed 10" })
    .default(5),
  estimated_minutes: z.coerce
    .number({ message: "Estimated time must be a valid number" })
    .int({ message: "Estimated time must be a whole number" })
    .min(1, { message: "Estimated time must be at least 1 minute" })
    .max(99999, { message: "Estimated time cannot exceed 99999 minutes" })
    .optional(),
});

export type CreateTaskInput = z.infer<typeof createTaskSchema>;

/**
 * Schema for updating an existing task
 *
 * Extends create schema with optional status field
 */
export const updateTaskSchema = createTaskSchema.extend({
  status: taskStatusEnum.optional(),
});

export type UpdateTaskInput = z.infer<typeof updateTaskSchema>;
