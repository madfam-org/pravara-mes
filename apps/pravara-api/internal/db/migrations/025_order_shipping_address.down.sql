-- Rollback: remove the order shipping address column.
ALTER TABLE orders DROP COLUMN IF EXISTS shipping_address;
