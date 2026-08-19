-- +goose Up
CREATE TABLE ceremony_catalog (
    slug         TEXT   PRIMARY KEY,
    display_name TEXT   NOT NULL,
    logo_url     TEXT   NOT NULL,
    category     TEXT   NOT NULL DEFAULT 'general',
    description  TEXT,
    created_at   BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

-- +goose Down
DROP TABLE IF EXISTS ceremony_catalog;
