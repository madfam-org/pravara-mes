/**
 * Centralized validation schemas using Zod
 *
 * All form validation is handled through these schemas with react-hook-form's zodResolver.
 * Each schema provides:
 * - Type inference for TypeScript
 * - Custom error messages
 * - Input transformation (coercion)
 * - Field constraints (min/max length, patterns)
 */

export * from "./machine";
export * from "./order";
export * from "./task";
