-- Rollback: PostgreSQL does not support removing a value from an enum type.
-- Removing 'online' would require rebuilding the type and every dependent
-- column; existing rows may already hold the value. Intentional no-op.
SELECT 1;
