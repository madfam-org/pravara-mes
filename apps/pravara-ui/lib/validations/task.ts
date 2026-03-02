import { z } from "zod";

export const createTaskSchema = z.object({
  title: z.string().min(1, "Title is required").max(255),
  description: z.string().max(2000).optional().or(z.literal("")),
  order_id: z.string().uuid().optional().or(z.literal("")),
  machine_id: z.string().uuid().optional().or(z.literal("")),
  priority: z.coerce.number().min(1).max(10).default(5),
  estimated_minutes: z.coerce.number().min(1).optional(),
});

export type CreateTaskInput = z.infer<typeof createTaskSchema>;

export const updateTaskSchema = createTaskSchema.extend({
  status: z.enum([
    "backlog",
    "queued",
    "in_progress",
    "quality_check",
    "completed",
    "blocked",
  ]).optional(),
});

export type UpdateTaskInput = z.infer<typeof updateTaskSchema>;
