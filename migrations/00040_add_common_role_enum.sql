-- +goose Up
-- +goose NO TRANSACTION
ALTER TYPE user_role ADD VALUE IF NOT EXISTS 'common';

-- +goose Down
-- Postgres cannot drop enum values; this migration is not reversible.
SELECT 1;
