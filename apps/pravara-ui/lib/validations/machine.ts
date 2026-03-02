import { z } from "zod";

export const createMachineSchema = z.object({
  name: z.string().min(1, "Name is required").max(255),
  code: z.string().min(1, "Code is required").regex(/^[A-Z0-9_-]+$/i, "Code must be alphanumeric (A-Z, 0-9, _, -)"),
  type: z.string().min(1, "Type is required").max(100),
  description: z.string().max(2000).optional().or(z.literal("")),
  mqtt_topic: z.string().max(255).optional().or(z.literal("")),
  location: z.string().max(255).optional().or(z.literal("")),
});

export type CreateMachineInput = z.infer<typeof createMachineSchema>;

export const updateMachineSchema = createMachineSchema.extend({
  status: z.enum([
    "offline",
    "online",
    "idle",
    "running",
    "maintenance",
    "error",
  ]).optional(),
});

export type UpdateMachineInput = z.infer<typeof updateMachineSchema>;
