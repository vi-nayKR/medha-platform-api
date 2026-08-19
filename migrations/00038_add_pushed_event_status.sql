-- +goose Up
ALTER TYPE event_status ADD VALUE IF NOT EXISTS 'Pushed';

-- +goose Down
-- PostgreSQL does not support dropping values from an enum, so this is a no-op.
