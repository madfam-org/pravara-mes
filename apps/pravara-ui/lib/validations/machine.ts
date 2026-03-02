import { z } from "zod";

/**
 * Machine status enum with descriptive values
 */
export const machineStatusEnum = z.enum([
  "offline",
  "online",
  "idle",
  "running",
  "maintenance",
  "error",
]);

export type MachineStatus = z.infer<typeof machineStatusEnum>;

/**
 * Schema for creating a new machine
 *
 * Validates:
 * - name: Required, 1-255 characters
 * - code: Required, alphanumeric with underscore/hyphen (auto-uppercased in UI)
 * - type: Required, machine category
 * - description: Optional, max 2000 characters
 * - mqtt_topic: Optional, MQTT topic pattern (validates format)
 * - location: Optional, physical location description
 */
export const createMachineSchema = z.object({
  name: z
    .string()
    .min(1, "Machine name is required")
    .max(255, "Machine name cannot exceed 255 characters")
    .trim(),
  code: z
    .string()
    .min(1, "Machine code is required")
    .max(50, "Machine code cannot exceed 50 characters")
    .regex(
      /^[A-Z0-9][A-Z0-9_-]*$/i,
      "Machine code must start with a letter or number and contain only A-Z, 0-9, underscores, or hyphens"
    )
    .transform((val) => val.toUpperCase()),
  type: z
    .string()
    .min(1, "Machine type is required")
    .max(100, "Machine type cannot exceed 100 characters"),
  description: z
    .string()
    .max(2000, "Description cannot exceed 2000 characters")
    .optional()
    .or(z.literal("")),
  mqtt_topic: z
    .string()
    .max(255, "MQTT topic cannot exceed 255 characters")
    .regex(
      /^$|^[a-zA-Z0-9/_+-]+$/,
      "MQTT topic can only contain letters, numbers, slashes, underscores, plus, and hyphens"
    )
    .optional()
    .or(z.literal("")),
  location: z
    .string()
    .max(255, "Location cannot exceed 255 characters")
    .optional()
    .or(z.literal("")),
});

export type CreateMachineInput = z.infer<typeof createMachineSchema>;

/**
 * Schema for updating an existing machine
 *
 * Extends create schema with optional status field
 */
export const updateMachineSchema = createMachineSchema.extend({
  status: machineStatusEnum.optional(),
});

export type UpdateMachineInput = z.infer<typeof updateMachineSchema>;
