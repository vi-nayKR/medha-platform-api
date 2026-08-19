-- +goose Up
-- +goose NO TRANSACTION
-- New enum values can't be used in the same transaction they're added in on
-- older Postgres, so this is split into its own migration (same pattern as
-- 00040_add_common_role_enum.sql). A dedicated non-login role for platform
-- system accounts, distinct from the human-facing yajman/pandit/common roles.
ALTER TYPE user_role ADD VALUE IF NOT EXISTS 'system';

-- +goose Down
-- Postgres cannot drop enum values; this migration is not reversible.
SELECT 1;
