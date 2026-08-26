-- =============================================================================
-- Order shipping address (delivery fields for the order lifecycle)
-- =============================================================================
-- Adds a first-class delivery model to orders instead of burying shipping
-- details in metadata. Free-form JSONB: line1, line2, city, state,
-- postal_code, country, contact_name, contact_phone (keys are a convention,
-- not enforced).
--
-- DEPLOY ORDER: this migration MUST be applied before deploying application
-- code that references orders.shipping_address (order repository selects the
-- column). Run `make db-migrate` (infra/db/migrate.sh) first.
--
-- Note on numbering: migrations 002-008 never existed; numbering continues
-- from the highest existing file (024).

ALTER TABLE orders ADD COLUMN IF NOT EXISTS shipping_address JSONB;

COMMENT ON COLUMN orders.shipping_address IS
    'Delivery address/contact as free-form JSON (line1, line2, city, state, postal_code, country, contact_name, contact_phone)';
